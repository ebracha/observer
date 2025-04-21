package services

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ebracha/airflow-observer/models"
)

func CheckViolations(metrics []models.Metric, rules []models.SLARule) []models.Violation {
	var violations []models.Violation
	now := time.Now()

	for _, rule := range rules {
		switch rule.Condition {
		case ">", "<", "=":
			for _, metric := range metrics {
				if rule.DagID != "" && rule.DagID != metric.DagID {
					continue // Skip if rule is tied to a specific DAG and doesn't match
				}
				var metricValue, taskID string
				if metric.TaskID != nil {
					taskID = *metric.TaskID
				}
				switch strings.ToLower(rule.FieldName) {
				case "event_type":
					metricValue = metric.EventType
				case "dag_id":
					metricValue = metric.DagID
				case "task_id":
					if metric.TaskID != nil {
						metricValue = *metric.TaskID
					}
				case "execution_time":
					metricValue = metric.ExecutionTime
				case "start_time":
					if metric.StartTime != nil {
						metricValue = *metric.StartTime
					}
				case "duration":
					if metric.Duration != nil {
						metricValue = fmt.Sprintf("%.2f", *metric.Duration)
					}
				}
				if metricValue == "" {
					continue
				}
				slaMissed := false
				var value float64
				if rule.FieldName == "duration" {
					val, _ := strconv.ParseFloat(metricValue, 64)
					ruleVal, _ := strconv.ParseFloat(rule.Value, 64)
					value = val
					switch rule.Condition {
					case ">":
						slaMissed = val > ruleVal
					case "<":
						slaMissed = val < ruleVal
					case "=":
						slaMissed = val == ruleVal
					}
				} else {
					// For non-numeric fields, we'll use 1.0 as the value
					value = 1.0
					switch rule.Condition {
					case ">":
						slaMissed = metricValue > rule.Value
					case "<":
						slaMissed = metricValue < rule.Value
					case "=":
						slaMissed = metricValue == rule.Value
					}
				}
				if slaMissed {
					execTime, _ := time.Parse(time.RFC3339, metric.ExecutionTime)
					threshold, _ := strconv.ParseFloat(rule.Value, 64)
					violations = append(violations, models.Violation{
						ID:          fmt.Sprintf("%s:%s:%s", metric.DagID, taskID, execTime.Format(time.RFC3339)),
						RuleID:      fmt.Sprintf("%d", rule.ID),
						DagID:       metric.DagID,
						TaskID:      taskID,
						Timestamp:   execTime,
						Value:       value,
						Threshold:   threshold,
						SLAMissed:   true,
						Severity:    rule.Severity,
						Description: fmt.Sprintf("%s %s %s", rule.FieldName, rule.Condition, rule.Value),
					})
				}
			}
		case "count>":
			count := 0
			windowStart := now.Add(-time.Duration(rule.WindowMins) * time.Minute)
			for _, metric := range metrics {
				if rule.DagID != "" && rule.DagID != metric.DagID {
					continue
				}
				execTime, err := time.Parse(time.RFC3339, metric.ExecutionTime)
				if err != nil || execTime.Before(windowStart) {
					continue
				}
				var metricValue string
				switch strings.ToLower(rule.FieldName) {
				case "event_type":
					metricValue = metric.EventType
				case "dag_id":
					metricValue = metric.DagID
				case "task_id":
					if metric.TaskID != nil {
						metricValue = *metric.TaskID
					}
				case "execution_time":
					metricValue = metric.ExecutionTime
				case "start_time":
					if metric.StartTime != nil {
						metricValue = *metric.StartTime
					}
				case "duration":
					if metric.Duration != nil {
						metricValue = fmt.Sprintf("%.2f", *metric.Duration)
					}
				}
				if metricValue == rule.Value {
					count++
				}
			}
			// Only trigger violation if count exceeds threshold
			if count > rule.CountThresh {
				threshold, _ := strconv.ParseFloat(rule.Value, 64)
				violations = append(violations, models.Violation{
					ID:          fmt.Sprintf("%s:%s:%s", rule.DagID, "", now.Format(time.RFC3339)),
					RuleID:      fmt.Sprintf("%d", rule.ID),
					DagID:       rule.DagID,
					TaskID:      "",
					Timestamp:   now,
					Value:       float64(count),
					Threshold:   threshold,
					SLAMissed:   true,
					Severity:    rule.Severity,
					Description: fmt.Sprintf("count> %d in %d mins", rule.CountThresh, rule.WindowMins),
				})
			}
		}
	}
	return violations
}
