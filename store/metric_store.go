package store

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ebracha/airflow-observer/models"
	"github.com/ebracha/airflow-observer/storage"
)

type Config struct {
	Host   string
	Token  string
	Org    string
	Bucket string
}

type MetricFilter struct {
	EventType      string    `json:"event_type"`
	DagID          string    `json:"dag_id"`
	TaskID         string    `json:"task_id"`
	Producer       string    `json:"producer"`
	RunID          string    `json:"run_id"`
	JobType        string    `json:"job_type"`
	ProcessingType string    `json:"processing_type"`
	Integration    string    `json:"integration"`
	StartTime      time.Time `json:"start_time"`
	EndTime        time.Time `json:"end_time"`
	Limit          int       `json:"limit"`
}

type MetricStore interface {
	Create(ctx context.Context, event models.Metric) error
	List(ctx context.Context, filter MetricFilter) ([]models.Metric, error)
	GetLatestMetricTime(ctx context.Context) (time.Time, error)
	IsInitialized() bool
	Close()
}

type MetricStorage struct {
	storage storage.TimeSeriesStorage
}

func NewMetricStore(storage storage.TimeSeriesStorage) MetricStore {
	return &MetricStorage{
		storage: storage,
	}
}

func (s *MetricStorage) Create(ctx context.Context, event models.Metric) error {
	if !s.IsInitialized() {
		return fmt.Errorf("InfluxDB client is not initialized")
	}

	point := storage.Point{
		Measurement: "metrics",
		Tags: map[string]string{
			"event_type":      event.EventType,
			"dag_id":          event.DagID,
			"job_type":        event.JobType,
			"processing_type": event.ProcessingType,
			"integration":     event.Integration,
			"producer":        event.Producer,
			"run_id":          event.RunID,
			"namespace":       event.Namespace,
		},
		Fields: map[string]interface{}{
			"execution_time": event.ExecutionTime,
			"start_time":     event.StartTime,
			"duration":       event.Duration,
			"schema_url":     event.SchemaURL,
			"state":          event.State,
			"tasks_state":    event.TasksState,
		},
		Time: time.Now(),
	}

	if event.TaskID != nil {
		point.Tags["task_id"] = *event.TaskID
	}

	return s.storage.WritePoint(ctx, point)
}

func (s *MetricStorage) List(ctx context.Context, filter MetricFilter) ([]models.Metric, error) {
	if !s.IsInitialized() {
		return nil, fmt.Errorf("InfluxDB client is not initialized")
	}

	queryFilter := storage.QueryFilter{
		Measurement: "metrics",
		Tags:        map[string]string{},
		StartTime:   filter.StartTime,
		EndTime:     filter.EndTime,
		Limit:       filter.Limit,
	}

	// Add non-empty tag filters
	if filter.EventType != "" {
		queryFilter.Tags["event_type"] = filter.EventType
	}
	if filter.DagID != "" {
		queryFilter.Tags["dag_id"] = filter.DagID
	}
	if filter.TaskID != "" {
		queryFilter.Tags["task_id"] = filter.TaskID
	}
	if filter.RunID != "" {
		queryFilter.Tags["run_id"] = filter.RunID
	}
	if filter.JobType != "" {
		queryFilter.Tags["job_type"] = filter.JobType
	}
	if filter.ProcessingType != "" {
		queryFilter.Tags["processing_type"] = filter.ProcessingType
	}
	if filter.Integration != "" {
		queryFilter.Tags["integration"] = filter.Integration
	}
	if filter.Producer != "" {
		queryFilter.Tags["producer"] = filter.Producer
	}

	points, err := s.storage.QueryPoints(ctx, queryFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to query points: %w", err)
	}

	var metrics []models.Metric
	for _, point := range points {
		metric, err := s.parsePoint(point)
		if err != nil {
			log.Printf("Failed to parse point: %v", err)
			continue
		}
		metrics = append(metrics, metric)
	}

	return metrics, nil
}

func (s *MetricStorage) GetLatestMetricTime(ctx context.Context) (time.Time, error) {
	if !s.IsInitialized() {
		return time.Time{}, fmt.Errorf("InfluxDB client is not initialized")
	}

	return s.storage.GetLatestTime(ctx, "metrics")
}

func (s *MetricStorage) IsInitialized() bool {
	return s.storage != nil
}

func (s *MetricStorage) Close() {
	if s.storage != nil {
		s.storage.Close()
	}
}

func (s *MetricStorage) parsePoint(point storage.Point) (models.Metric, error) {
	executionTime, _ := point.Fields["execution_time"].(string)
	startTime, _ := point.Fields["start_time"].(*string)
	duration, _ := point.Fields["duration"].(*float64)
	schemaURL, _ := point.Fields["schema_url"].(string)
	state, _ := point.Fields["state"].(string)
	tasksState, _ := point.Fields["tasks_state"].(map[string]string)

	metric := models.Metric{
		EventType:      point.Tags["event_type"],
		DagID:          point.Tags["dag_id"],
		ExecutionTime:  executionTime,
		StartTime:      startTime,
		Duration:       duration,
		JobType:        point.Tags["job_type"],
		ProcessingType: point.Tags["processing_type"],
		Integration:    point.Tags["integration"],
		Producer:       point.Tags["producer"],
		RunID:          point.Tags["run_id"],
		Namespace:      point.Tags["namespace"],
		SchemaURL:      schemaURL,
		State:          state,
		TasksState:     tasksState,
	}

	if taskID, ok := point.Tags["task_id"]; ok {
		metric.TaskID = &taskID
	}

	return metric, nil
}
