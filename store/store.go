package store

import (
	"sync"

	"github.com/ebracha/airflow-observer/storage"
)

// Store combines all three store implementations
type Store struct {
	metricStore    MetricStore
	ruleStore      RuleStore
	violationStore ViolationStore
	mu             sync.RWMutex
}

// NewStore creates a new Store instance
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

// Events returns the event store
func (s *Store) Metrics() MetricStore {
	return s.metricStore
}

// Rules returns the rule store
func (s *Store) Rules() RuleStore {
	return s.ruleStore
}

// Violations returns the violation store
func (s *Store) Violations() ViolationStore {
	return s.violationStore
}

// Close closes all store connections
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
