package services

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ebracha/airflow-observer/models"
	"github.com/ebracha/airflow-observer/storage"
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
				if rule.FieldName == "duration" {
					val, _ := strconv.ParseFloat(metricValue, 64)
					ruleVal, _ := strconv.ParseFloat(rule.Value, 64)
					switch rule.Condition {
					case ">":
						slaMissed = val > ruleVal
					case "<":
						slaMissed = val < ruleVal
					case "=":
						slaMissed = val == ruleVal
					}
				} else {
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
					violations = append(violations, models.Violation{
						DagID:     metric.DagID,
						TaskID:    taskID,
						FieldName: rule.FieldName,
						Value:     metricValue,
						Condition: rule.Condition,
						RuleValue: rule.Value,
						SLAMissed: true,
						Timestamp: execTime,
					})
					for i, r := range storage.Rules.Data {
						if r.ID == rule.ID {
							storage.Rules.Lock()
							storage.Rules.Data[i].LastViolated = &execTime
							storage.Rules.Unlock()
							break
						}
					}
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
				violations = append(violations, models.Violation{
					DagID:     rule.DagID,
					TaskID:    "",
					FieldName: rule.FieldName,
					Value:     fmt.Sprintf("%d occurrences", count),
					Condition: fmt.Sprintf("count> %d in %d mins", rule.CountThresh, rule.WindowMins),
					RuleValue: rule.Value,
					SLAMissed: true,
					Timestamp: now,
				})
				for i, r := range storage.Rules.Data {
					if r.ID == rule.ID {
						storage.Rules.Lock()
						storage.Rules.Data[i].LastViolated = &now
						storage.Rules.Unlock()
						break
					}
				}
			}
		}
	}
	return violations
}
