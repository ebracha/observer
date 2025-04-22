package services

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/ebracha/airflow-observer/models"
	"github.com/ebracha/airflow-observer/store"
)

type ViolationService struct {
	rulesStore     store.RuleStore
	violationStore store.ViolationStore
	metricChan     chan models.Metric
}

const violationChannelBufferSize = 100

func NewViolationService(rules store.RuleStore, violations store.ViolationStore) *ViolationService {
	metricChan := make(chan models.Metric, violationChannelBufferSize)

	return &ViolationService{
		rulesStore:     rules,
		violationStore: violations,
		metricChan:     metricChan,
	}
}

func (s *ViolationService) MetricChan() chan models.Metric {
	return s.metricChan
}

func (s *ViolationService) Start(ctx context.Context) {
	log.Println("Successfully started Violation service and listening for incoming metrics")
	for {
		select {
		case <-ctx.Done():
			log.Println("Violation service stopping...")
			return
		case metric := <-s.metricChan:
			log.Printf("Violation service received metric: DAG=%s, Event=%s", metric.DagID, metric.EventType)
			rules, err := s.rulesStore.ListRules(ctx)
			if err != nil {
				log.Printf("Error fetching rules in Violation service: %v", err)
				continue
			}

			violations := s.checkSingleMetric(metric, rules)
			if len(violations) > 0 {
				log.Printf("Detected %d violation(s) for metric: DAG=%s, Event=%s", len(violations), metric.DagID, metric.EventType)
				for _, v := range violations {
					log.Printf("  Violation: %+v", v)
					if s.violationStore != nil {
						err := s.violationStore.StoreViolation(ctx, &v)
						if err != nil {
							log.Printf("Error storing violation: %v", err)
						}
					}
				}
			}
		}
	}
}

func (s *ViolationService) checkSingleMetric(metric models.Metric, rules []*store.Rule) []models.Violation {
	var violations []models.Violation
	// now := time.Now() // Removed: Not used since count> is disabled

	for _, rule := range rules {
		// Convert store.Rule to models.SLARule for consistent checking logic
		// This assumes getIntFromTags helper exists or is added
		slaRule := models.SLARule{
			ID:          0, // ID might need adjustment depending on how rules are identified
			DagID:       rule.Tags["dag_id"],
			FieldName:   rule.Tags["field_name"],
			Condition:   rule.Tags["condition"],
			Value:       fmt.Sprintf("%.2f", rule.Threshold), // Or handle non-float rules
			Severity:    rule.Tags["severity"],
			WindowMins:  getIntFromTags(rule.Tags, "window_mins"),  // Assumes this helper exists
			CountThresh: getIntFromTags(rule.Tags, "count_thresh"), // Assumes this helper exists
			CreatedAt:   rule.CreatedAt,
		}

		if slaRule.DagID != "" && slaRule.DagID != metric.DagID {
			continue // Skip if rule is tied to a specific DAG and doesn't match
		}

		// --- Start of checking logic adapted from CheckViolations ---
		var metricValueStr string
		taskID := ""
		if metric.TaskID != nil {
			taskID = *metric.TaskID
		}

		// Extract the relevant value from the metric based on the rule's FieldName
		switch strings.ToLower(slaRule.FieldName) {
		case "event_type":
			metricValueStr = metric.EventType
		case "dag_id":
			metricValueStr = metric.DagID
		case "task_id":
			metricValueStr = taskID
		case "execution_time":
			metricValueStr = metric.ExecutionTime // String comparison might be needed or parse time
		case "start_time":
			if metric.StartTime != nil {
				metricValueStr = *metric.StartTime // String comparison or parse time
			}
		case "duration":
			if metric.Duration != nil {
				metricValueStr = fmt.Sprintf("%.2f", *metric.Duration)
			}
			// Add cases for other fields if necessary (e.g., State, JobType)
		}

		if metricValueStr == "" {
			continue // Skip if the metric doesn't have the field the rule applies to
		}

		slaMissed := false
		var metricValueNum float64 // Numeric value for comparisons
		var ruleThresholdNum float64
		isNumericField := slaRule.FieldName == "duration" // Add other numeric fields here

		if isNumericField {
			var err error
			metricValueNum, err = strconv.ParseFloat(metricValueStr, 64)
			if err != nil {
				log.Printf("Warning: Could not parse metric value '%s' as float for rule %v", metricValueStr, slaRule)
				continue
			}
			ruleThresholdNum, err = strconv.ParseFloat(slaRule.Value, 64)
			if err != nil {
				log.Printf("Warning: Could not parse rule threshold '%s' as float for rule %v", slaRule.Value, slaRule)
				continue
			}
		}

		switch slaRule.Condition {
		case ">", "<", "=":
			if isNumericField {
				switch slaRule.Condition {
				case ">":
					slaMissed = metricValueNum > ruleThresholdNum
				case "<":
					slaMissed = metricValueNum < ruleThresholdNum
				case "=":
					slaMissed = metricValueNum == ruleThresholdNum
				}
			} else { // String comparison
				switch slaRule.Condition {
				case ">":
					slaMissed = metricValueStr > slaRule.Value
				case "<":
					slaMissed = metricValueStr < slaRule.Value
				case "=":
					slaMissed = metricValueStr == slaRule.Value
				}
			}
			if slaMissed {
				execTime, _ := time.Parse(time.RFC3339, metric.ExecutionTime)
				violation := models.Violation{
					ID:          fmt.Sprintf("v:%s:%s:%s", metric.DagID, taskID, execTime.Format(time.RFC3339)),
					RuleID:      rule.ID, // Use the actual rule ID from store.Rule
					DagID:       metric.DagID,
					TaskID:      taskID,
					Timestamp:   execTime,
					Value:       metricValueNum, // Store the numeric value if applicable
					Threshold:   ruleThresholdNum,
					SLAMissed:   true,
					Severity:    slaRule.Severity,
					Description: fmt.Sprintf("%s %s %s", slaRule.FieldName, slaRule.Condition, slaRule.Value),
				}
				if !isNumericField {
					violation.Value = 1.0   // Use 1.0 for non-numeric violations
					violation.Threshold = 0 // No numeric threshold for string compares
					violation.Description += fmt.Sprintf(" (Actual: %s)", metricValueStr)
				}
				violations = append(violations, violation)
			}

			// Note: 'count>' condition type cannot be reliably checked with single metrics
			// without maintaining state or querying historical data within the service.
			// The current implementation only handles direct metric comparisons.
			// A separate mechanism or stateful check would be needed for count-based rules.
			// case "count>":
			//    // ... logic to check count over window would require historical data access ...
		}
		// --- End of checking logic ---
	}
	return violations
}

// Helper function to get integer from tags (assuming it exists elsewhere or add it here)
func getIntFromTags(tags map[string]string, key string) int {
	if val, ok := tags[key]; ok {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return 0 // Default value if tag not found or not an integer
}
