package storage

import (
	"context"
	"sync"
	"time"

	"github.com/ebracha/airflow-observer/models"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
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

func InitInfluxDB() {
	client = influxdb2.NewClient("http://localhost:8086", "your-token")
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
	writeAPI := client.WriteAPIBlocking("your-org", "your-bucket")
	p := influxdb2.NewPointWithMeasurement("lineage_events").
		AddTag("event_type", event.EventType).
		AddTag("job_name", event.Job.Name).
		AddTag("job_namespace", event.Job.Namespace).
		AddTag("producer", event.Producer).
		AddTag("run_id", event.Run.RunId).
		AddField("event_time", event.EventTime).
		AddField("schema_url", event.SchemaURL).
		SetTime(time.Now())
	return writeAPI.WritePoint(context.Background(), p)
}

func (s *InfluxLineageStore) ReadAll() ([]models.LineageEvent, error) {
	queryAPI := client.QueryAPI("your-org")
	query := `from(bucket:"your-bucket") |> range(start: -1h)`
	result, err := queryAPI.Query(context.Background(), query)
	if err != nil {
		return nil, err
	}
	var events []models.LineageEvent
	for result.Next() {
		record := result.Record()
		event := models.LineageEvent{
			EventTime: record.Time().String(),
			EventType: record.ValueByKey("event_type").(string),
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
				Name:      record.ValueByKey("job_name").(string),
				Namespace: record.ValueByKey("job_namespace").(string),
			},
			Producer:  record.ValueByKey("producer").(string),
			SchemaURL: record.ValueByKey("schema_url").(string),
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
				RunId: record.ValueByKey("run_id").(string),
			},
		}
		events = append(events, event)
	}
	return events, nil
}

func (s *InfluxLineageStore) GetLatestMetricTime() (time.Time, error) {
	queryAPI := client.QueryAPI("your-org")
	query := `from(bucket:"your-bucket")
		|> range(start: -30d)
		|> filter(fn: (r) => r["_measurement"] == "lineage_events")
		|> last()
		|> keep(columns: ["_time"])`

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

func GetLineageStore() LineageStoreInterface {
	once.Do(func() {
		store = &InfluxLineageStore{}
	})
	return store
}
