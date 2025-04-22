package store

import (
	"sync"

	"github.com/ebracha/airflow-observer/storage"
)

type Store struct {
	metricStore    MetricStore
	ruleStore      RuleStore
	violationStore ViolationStore
	mu             sync.RWMutex
}

func NewStore(timeSeriesStorage storage.TimeSeriesStorage, storage storage.Storage) (*Store, error) {
	eventStore := NewMetricStore(timeSeriesStorage)
	ruleStore := NewRuleStore(storage)
	violationStore := NewViolationStore(storage)

	return &Store{
		metricStore:    eventStore,
		ruleStore:      ruleStore,
		violationStore: violationStore,
	}, nil
}

func (s *Store) Metrics() MetricStore {
	return s.metricStore
}

func (s *Store) Rules() RuleStore {
	return s.ruleStore
}

func (s *Store) Violations() ViolationStore {
	return s.violationStore
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.metricStore.Close()

	if err := s.ruleStore.Close(); err != nil {
		return err
	}

	if err := s.violationStore.Close(); err != nil {
		return err
	}

	return nil
}
