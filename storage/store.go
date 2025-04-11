package storage

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/ebracha/airflow-observer/models"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
)

const (
	INFLUXDB_HOST  = "http://localhost:8086"
	INFLUXDB_TOKEN = "test-token"
)

var (
	client influxdb2.Client
	once   sync.Once
	store  *InfluxLineageStore
)

var (
	Metrics = MetricsStore{Data: make([]models.Metric, 0)}
	Rules   = RulesStore{Data: make([]models.SLARule, 0)}
	Lineage = GetLineageStore()
)

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func InitInfluxDB() {
	host := getEnv("INFLUXDB_HOST", "http://localhost:8086")
	token := getEnv("INFLUXDB_TOKEN", "observer-token")
	org := getEnv("INFLUXDB_ORG", "observer")
	bucket := getEnv("INFLUXDB_BUCKET", "observer")

	log.Printf("Initializing InfluxDB connection with host: %s, org: %s, bucket: %s", host, org, bucket)

	client = influxdb2.NewClient(host, token)

	// Verify connection and permissions
	health, err := client.Health(context.Background())
	if err != nil {
		log.Printf("Failed to connect to InfluxDB: %v", err)
		return
	}

	if health.Status != "pass" {
		log.Printf("InfluxDB health check failed: %s", health.Status)
		return
	}

	// Verify write permissions
	writeAPI := client.WriteAPIBlocking(org, bucket)
	p := influxdb2.NewPointWithMeasurement("test").
		AddField("test", 1).
		SetTime(time.Now())

	if err := writeAPI.WritePoint(context.Background(), p); err != nil {
		log.Printf("Failed to verify write permissions: %v", err)
		return
	}

	log.Printf("Successfully connected to InfluxDB and verified permissions")
}

type MetricsStore struct {
	sync.Mutex
	Data []models.Metric
}

type RulesStore struct {
	sync.Mutex
	Data []models.SLARule
}

type LineageStoreInterface interface {
	Create(event models.LineageEvent) error
	ReadAll() ([]models.LineageEvent, error)
	GetLatestMetricTime() (time.Time, error)
}

type InfluxLineageStore struct{}

func (s *InfluxLineageStore) Create(event models.LineageEvent) error {
	if !s.IsInitialized() {
		return fmt.Errorf("InfluxDB client is not initialized")
	}

	org := getEnv("INFLUXDB_ORG", "observer")
	bucket := getEnv("INFLUXDB_BUCKET", "observer")

	writeAPI := client.WriteAPIBlocking(org, bucket)
	p := influxdb2.NewPointWithMeasurement("lineage_events").
		AddTag("event_type", event.EventType).
		AddTag("job_name", event.Job.Name).
		AddTag("job_namespace", event.Job.Namespace).
		AddTag("producer", event.Producer).
		AddTag("run_id", event.Run.RunId).
		AddField("event_time", event.EventTime).
		AddField("schema_url", event.SchemaURL).
		SetTime(time.Now())

	err := writeAPI.WritePoint(context.Background(), p)
	if err != nil {
		log.Printf("Failed to write point: %v", err)
		return fmt.Errorf("failed to write point: %w", err)
	}
	return nil
}

func (s *InfluxLineageStore) ReadAll() ([]models.LineageEvent, error) {
	if !s.IsInitialized() {
		return nil, fmt.Errorf("InfluxDB client is not initialized")
	}

	org := getEnv("INFLUXDB_ORG", "observer")
	bucket := getEnv("INFLUXDB_BUCKET", "observer")

	queryAPI := client.QueryAPI(org)
	query := fmt.Sprintf(`from(bucket:"%s") |> range(start: -1h)`, bucket)
	result, err := queryAPI.Query(context.Background(), query)
	if err != nil {
		return nil, err
	}
	var events []models.LineageEvent
	for result.Next() {
		record := result.Record()

		// Safely get values with nil checks
		eventType, _ := record.ValueByKey("event_type").(string)
		jobName, _ := record.ValueByKey("job_name").(string)
		jobNamespace, _ := record.ValueByKey("job_namespace").(string)
		producer, _ := record.ValueByKey("producer").(string)
		runId, _ := record.ValueByKey("run_id").(string)
		eventTime, _ := record.ValueByKey("event_time").(string)
		schemaURL, _ := record.ValueByKey("schema_url").(string)

		event := models.LineageEvent{
			EventTime: eventTime,
			EventType: eventType,
			Job: struct {
				Facets struct {
					JobType struct {
						Producer       string `json:"_producer"`
						SchemaURL      string `json:"_schemaURL"`
						Integration    string `json:"integration"`
						JobType        string `json:"jobType"`
						ProcessingType string `json:"processingType"`
					} `json:"jobType"`
				} `json:"facets"`
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			}{
				Name:      jobName,
				Namespace: jobNamespace,
			},
			Producer:  producer,
			SchemaURL: schemaURL,
			Run: struct {
				Facets struct {
					AirflowState struct {
						Producer    string            `json:"_producer"`
						SchemaURL   string            `json:"_schemaURL"`
						DagRunState string            `json:"dagRunState"`
						TasksState  map[string]string `json:"tasksState"`
					} `json:"airflowState"`
					Debug struct {
						Producer  string            `json:"_producer"`
						SchemaURL string            `json:"_schemaURL"`
						Packages  map[string]string `json:"packages"`
					} `json:"debug"`
				} `json:"facets"`
				RunId string `json:"runId"`
			}{
				RunId: runId,
			},
		}
		events = append(events, event)
	}
	return events, nil
}

func (s *InfluxLineageStore) GetLatestMetricTime() (time.Time, error) {
	if !s.IsInitialized() {
		return time.Time{}, fmt.Errorf("InfluxDB client is not initialized")
	}

	org := getEnv("INFLUXDB_ORG", "your-org")
	bucket := getEnv("INFLUXDB_BUCKET", "your-bucket")

	queryAPI := client.QueryAPI(org)
	query := fmt.Sprintf(`from(bucket:"%s")
		|> range(start: -30d)
		|> filter(fn: (r) => r["_measurement"] == "lineage_events")
		|> last()
		|> keep(columns: ["_time"])`, bucket)

	result, err := queryAPI.Query(context.Background(), query)
	if err != nil {
		return time.Time{}, err
	}

	if !result.Next() {
		return time.Time{}, nil
	}

	record := result.Record()
	return record.Time(), nil
}

func (s *InfluxLineageStore) IsInitialized() bool {
	if client == nil {
		return false
	}

	// Try to ping the server to verify connection
	health, err := client.Health(context.Background())
	if err != nil {
		return false
	}

	return health.Status == "pass"
}

func GetLineageStore() LineageStoreInterface {
	once.Do(func() {
		store = &InfluxLineageStore{}
		InitInfluxDB()
	})
	return store
}
