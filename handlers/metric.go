package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/ebracha/airflow-observer/models"
	"github.com/ebracha/airflow-observer/storage"
)

type MetricHandler struct {
	timeSeriesStorage storage.TimeSeriesStorage
}

func NewMetricHandler(timeSeriesStorage storage.TimeSeriesStorage) *MetricHandler {
	return &MetricHandler{
		timeSeriesStorage: timeSeriesStorage,
	}
}

func (h *MetricHandler) MetricListen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var event models.LineageEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		log.Printf("Error decoding request body: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if event.EventType == "" || event.Job.Name == "" || event.Job.Namespace == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// Convert lineage event to metric
	metric := models.Metric{
		EventType:      event.EventType,
		DagID:          strings.Split(event.Job.Name, ".")[0],
		ExecutionTime:  event.EventTime,
		StartTime:      &event.EventTime, // For START events, this will be the same as execution time
		Duration:       new(float64),     // Will be calculated for COMPLETE/FAIL events
		JobType:        event.Job.Facets.JobType.JobType,
		ProcessingType: event.Job.Facets.JobType.ProcessingType,
		Integration:    event.Job.Facets.JobType.Integration,
		Producer:       event.Producer,
		RunID:          event.Run.RunId,
		Namespace:      event.Job.Namespace,
		SchemaURL:      event.SchemaURL,
		State:          event.Run.Facets.AirflowState.DagRunState,
		TasksState:     event.Run.Facets.AirflowState.TasksState,
	}

	// If this is a task event, extract task ID
	if len(strings.Split(event.Job.Name, ".")) > 1 {
		taskID := strings.Split(event.Job.Name, ".")[1]
		metric.TaskID = &taskID
	}

	// Store the event
	ctx := context.Background()
	point := storage.Point{
		Measurement: "metrics",
		Tags: map[string]string{
			"event_type":      metric.EventType,
			"dag_id":          metric.DagID,
			"job_type":        metric.JobType,
			"processing_type": metric.ProcessingType,
			"integration":     metric.Integration,
			"producer":        metric.Producer,
			"run_id":          metric.RunID,
			"namespace":       metric.Namespace,
		},
		Fields: map[string]interface{}{
			"execution_time": metric.ExecutionTime,
			"start_time":     metric.StartTime,
			"duration":       metric.Duration,
			"schema_url":     metric.SchemaURL,
			"state":          metric.State,
			"tasks_state":    metric.TasksState,
		},
		Time: time.Now(),
	}

	if err := h.timeSeriesStorage.WritePoint(ctx, point); err != nil {
		log.Printf("Failed to create metric: %v", err)
		http.Error(w, "Failed to create metric", http.StatusInternalServerError)
		return
	}

	log.Printf("Received lineage event: %s job: %s/%s", event.EventType, event.Job.Namespace, event.Job.Name)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}
