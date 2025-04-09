package storage

import (
	"sync"

	"github.com/ebracha/airflow-observer/models"
)

type MetricsStore struct {
	sync.Mutex
	Data []models.Metric
}

type RulesStore struct {
	sync.Mutex
	Data []models.SLARule
}

type LineageStore struct {
	sync.Mutex
	Events []models.LineageEvent
}

var (
	Metrics = MetricsStore{Data: make([]models.Metric, 0)}
	Rules   = RulesStore{Data: make([]models.SLARule, 0)}
	Lineage = LineageStore{Events: make([]models.LineageEvent, 0)}
)
