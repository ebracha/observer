package storage

import (
	"context"
	"fmt"
	"log"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/influxdata/influxdb-client-go/v2/api/query"
)

type Config struct {
	Host   string
	Token  string
	Org    string
	Bucket string
}

type Point struct {
	Measurement string
	Tags        map[string]string
	Fields      map[string]interface{}
	Time        time.Time
}

type QueryFilter struct {
	Measurement string
	Tags        map[string]string
	StartTime   time.Time
	EndTime     time.Time
	Limit       int
}

type InfluxDB struct {
	client   influxdb2.Client
	writeAPI api.WriteAPIBlocking
	queryAPI api.QueryAPI
	org      string
	bucket   string
}

type TimeSeriesStorage interface {
	WritePoint(ctx context.Context, point Point) error
	QueryPoints(ctx context.Context, filter QueryFilter) ([]Point, error)
	GetLatestTime(ctx context.Context, measurement string) (time.Time, error)
	Close()
}

func NewInfluxDB(config *Config) (TimeSeriesStorage, error) {
	var client influxdb2.Client
	var err error

	maxRetries := 5
	retryDelay := time.Second * 2

	for i := 0; i < maxRetries; i++ {
		client = influxdb2.NewClient(config.Host, config.Token)

		// Verify connection and permissions
		health, err := client.Health(context.Background())
		if err == nil && health.Status == "pass" {
			// Verify write permissions
			writeAPI := client.WriteAPIBlocking(config.Org, config.Bucket)
			p := influxdb2.NewPointWithMeasurement("test").
				AddField("test", 1).
				SetTime(time.Now())

			if err := writeAPI.WritePoint(context.Background(), p); err == nil {
				log.Printf("Successfully connected to InfluxDB and verified permissions")
				return &InfluxDB{
					client:   client,
					writeAPI: writeAPI,
					queryAPI: client.QueryAPI(config.Org),
					org:      config.Org,
					bucket:   config.Bucket,
				}, nil
			}
		}

		if i < maxRetries-1 {
			log.Printf("Failed to connect to InfluxDB (attempt %d/%d): %v", i+1, maxRetries, err)
			time.Sleep(retryDelay)
			continue
		}
	}

	return nil, fmt.Errorf("failed to connect to InfluxDB after %d attempts: %w", maxRetries, err)
}

func (s *InfluxDB) WritePoint(ctx context.Context, point Point) error {
	p := influxdb2.NewPointWithMeasurement(point.Measurement)

	for k, v := range point.Tags {
		p.AddTag(k, v)
	}

	for k, v := range point.Fields {
		p.AddField(k, v)
	}

	if !point.Time.IsZero() {
		p.SetTime(point.Time)
	} else {
		p.SetTime(time.Now())
	}

	if err := s.writeAPI.WritePoint(ctx, p); err != nil {
		return fmt.Errorf("failed to write point: %w", err)
	}
	return nil
}

func (s *InfluxDB) QueryPoints(ctx context.Context, filter QueryFilter) ([]Point, error) {
	query := s.buildQuery(filter)
	result, err := s.queryAPI.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query points: %w", err)
	}
	defer result.Close()

	var points []Point
	for result.Next() {
		point, err := s.parseRecord(result.Record())
		if err != nil {
			log.Printf("Error parsing record: %v", err)
			continue
		}
		if point != nil {
			points = append(points, *point)
		}
	}

	if result.Err() != nil {
		return nil, fmt.Errorf("error iterating results: %w", result.Err())
	}

	return points, nil
}

func (s *InfluxDB) GetLatestTime(ctx context.Context, measurement string) (time.Time, error) {
	query := fmt.Sprintf(`from(bucket:"%s")
		|> range(start: -30d)
		|> filter(fn: (r) => r["_measurement"] == "%s")
		|> sort(columns: ["_time"], desc: true)
		|> limit(n: 1)`, s.bucket, measurement)

	result, err := s.queryAPI.Query(ctx, query)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to query latest time: %w", err)
	}

	if !result.Next() {
		return time.Time{}, nil
	}

	return result.Record().Time(), nil
}

func (s *InfluxDB) buildQuery(filter QueryFilter) string {
	query := fmt.Sprintf(`from(bucket:"%s")`, s.bucket)

	if !filter.StartTime.IsZero() {
		if filter.EndTime.IsZero() {
			filter.EndTime = time.Now()
		}
		query += fmt.Sprintf(` |> range(start: %s, stop: %s)`,
			filter.StartTime.Format(time.RFC3339),
			filter.EndTime.Format(time.RFC3339))
	} else {
		query += ` |> range(start: -30d)`
	}

	if filter.Measurement != "" {
		query += fmt.Sprintf(` |> filter(fn: (r) => r["_measurement"] == "%s")`, filter.Measurement)
	}

	for k, v := range filter.Tags {
		query += fmt.Sprintf(` |> filter(fn: (r) => r["%s"] == "%s")`, k, v)
	}

	if filter.Limit > 0 {
		query += fmt.Sprintf(` |> limit(n: %d)`, filter.Limit)
	}

	return query
}

func (s *InfluxDB) parseRecord(record *query.FluxRecord) (*Point, error) {
	measurement := record.Measurement()
	tags := make(map[string]string)
	fields := make(map[string]interface{})

	for k, v := range record.Values() {
		if k == "_time" || k == "_measurement" {
			continue
		}
		if _, ok := record.ValueByKey(k).(string); ok {
			tags[k] = fmt.Sprintf("%v", v)
		} else {
			fields[k] = v
		}
	}

	return &Point{
		Measurement: measurement,
		Tags:        tags,
		Fields:      fields,
		Time:        record.Time(),
	}, nil
}

func (s *InfluxDB) Close() {
	s.client.Close()
}
