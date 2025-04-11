package services

import (
	"fmt"
	"strings"
	"time"

	"github.com/ebracha/airflow-observer/models"
	"github.com/ebracha/airflow-observer/storage"
)

func ProcessLineageEvents() error {
	events, err := storage.GetLineageStore().ReadAll()
	if err != nil {
		return fmt.Errorf("failed to read lineage events: %v", err)
	}

	starts := make(map[string]models.LineageEvent)

	for _, event := range events {
		runId := event.Run.RunId

		if event.EventType == "START" {
			starts[runId] = event
		}
	}

	var metrics []models.Metric
	for _, event := range events {
		jobType := event.Job.Facets.JobType.JobType
		runId := event.Run.RunId

		if event.EventType == "COMPLETE" || event.EventType == "FAIL" {
			var startEvent models.LineageEvent
			var isDag bool
			if jobType == "DAG" {
				startEvent, isDag = starts[runId], true
			} else if jobType == "TASK" {
				startEvent, isDag = starts[runId], false
			} else {
				continue // Skip unknown job types
			}

			if startEvent.EventTime == "" {
				continue
			}

			startTime, err := time.Parse(time.RFC3339, startEvent.EventTime)
			if err != nil {
				return fmt.Errorf("failed to parse start time %s: %v", startEvent.EventTime, err)
			}
			endTime, err := time.Parse(time.RFC3339, event.EventTime)
			if err != nil {
				return fmt.Errorf("failed to parse end time %s: %v", event.EventTime, err)
			}

			duration := endTime.Sub(startTime).Seconds()
			startTimeStr := startEvent.EventTime

			eventType := ""
			if jobType == "DAG" {
				if event.EventType == "COMPLETE" {
					eventType = "dag_success"
				} else {
					eventType = "dag_failed"
				}
			} else {
				if event.EventType == "COMPLETE" {
					eventType = "task_success"
				} else {
					eventType = "task_failed"
				}
			}

			// Create the DerivedMetric
			metric := models.Metric{
				EventType:     eventType,
				ExecutionTime: event.EventTime,
				StartTime:     &startTimeStr,
				Duration:      &duration,
			}

			parts := strings.Split(event.Job.Name, ".")
			if isDag {
				metric.DagID = parts[0]
				metric.TaskID = nil
				metrics = append(metrics, metric)
			} else {
				metric.DagID = parts[0]
				taskID := parts[1]
				metric.TaskID = &taskID
				metrics = append(metrics, metric)
			}
		}
	}

	storage.Metrics.Lock()
	storage.Metrics.Data = metrics
	storage.Metrics.Unlock()

	return nil
}
