package handlers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/ebracha/airflow-observer/services"
	"github.com/ebracha/airflow-observer/storage"
	"github.com/ebracha/airflow-observer/store"
)

// getEnvOrDefault returns the value of the environment variable or the default value if not set
func getEnv(key string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return ""
}

// Router holds all the necessary dependencies for the handlers
type Router struct {
	metricHandler *MetricHandler
	webHandler    *WebHandler
	store         *store.Store
}

// NewRouter creates a new router with all dependencies initialized
func NewRouter() (*Router, error) {
	config := &storage.Config{
		Host:   getEnv("INFLUXDB_URL"),
		Token:  getEnv("INFLUXDB_TOKEN"),
		Org:    getEnv("INFLUXDB_ORG"),
		Bucket: getEnv("INFLUXDB_BUCKET"),
	}

	// Initialize InfluxDB client for time series storage
	timeSeriesStorage, err := storage.NewInfluxDB(config)
	if err != nil {
		return nil, err
	}

	// Initialize Redis client for key-value storage
	db, _ := strconv.Atoi(getEnv("REDIS_DB"))
	redisAddr := fmt.Sprintf("%s:%s", getEnv("REDIS_HOST"), getEnv("REDIS_PORT"))
	storage, err := storage.NewRedisStore(
		redisAddr,
		getEnv("REDIS_PASSWORD"),
		db,
		"", // Empty prefix since we're using the full address
	)
	if err != nil {
		return nil, err
	}

	store, err := store.NewStore(timeSeriesStorage, storage)
	if err != nil {
		return nil, err
	}

	violationService := services.NewViolationService(store.Rules(), store.Violations())
	go violationService.Start(context.Background())

	mh := NewMetricHandler(timeSeriesStorage, violationService.MetricChan())
	wh := NewWebHandler(store)

	return &Router{
		metricHandler: mh,
		webHandler:    wh,
		store:         store,
	}, nil
}

// SetupRoutes configures all the routes and their handlers
func (r *Router) SetupRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// Lineage events endpoint
	mux.HandleFunc("/lineage/events", r.metricHandler.MetricListen)

	// UI endpoints
	mux.HandleFunc("/", r.webHandler.IndexHandler)
	mux.HandleFunc("/dashboard", r.webHandler.DashboardHandler)
	mux.HandleFunc("/monitoring", r.webHandler.MonitoringHandler)
	mux.HandleFunc("/metrics", r.webHandler.MetricsTableHandler)
	mux.HandleFunc("/violations", r.webHandler.ViolationsHandler)
	mux.HandleFunc("/rules/", r.webHandler.RulesHandler)
	mux.HandleFunc("/rules", r.webHandler.RulesHandler)
	mux.HandleFunc("/clients", r.webHandler.ClientsHandler)

	return mux
}

// Close cleans up resources used by the router
func (r *Router) Close() error {
	return r.store.Close()
}
