package store

import (
	"context"
	"fmt"
	"time"

	"github.com/ebracha/airflow-observer/models"
	"github.com/ebracha/airflow-observer/storage"
)

type ViolationStore interface {
	StoreViolation(ctx context.Context, violation *models.Violation) error
	GetViolations(ctx context.Context, filter ViolationFilter) ([]*models.Violation, error)
	GetViolationsByRule(ctx context.Context, ruleID string) ([]*models.Violation, error)
	GetViolationsByDag(ctx context.Context, dagID string) ([]*models.Violation, error)
	Close() error
}

type ViolationFilter struct {
	RuleID    string
	DagID     string
	TaskID    string
	StartTime time.Time
	EndTime   time.Time
	SLAMissed *bool
	Severity  string
}

type ViolationStorage struct {
	redis storage.Storage
}

func NewViolationStore(redis storage.Storage) ViolationStore {
	return &ViolationStorage{
		redis: redis,
	}
}

func (s *ViolationStorage) StoreViolation(ctx context.Context, violation *models.Violation) error {
	key := fmt.Sprintf("violation:%s:%s:%s", violation.DagID, violation.TaskID, violation.Timestamp.Format(time.RFC3339))
	return s.redis.Set(ctx, key, violation, 24*time.Hour) // Store violations for 24 hours
}

func (s *ViolationStorage) GetViolations(ctx context.Context, filter ViolationFilter) ([]*models.Violation, error) {
	pattern := "violation:*"
	if filter.DagID != "" {
		pattern = fmt.Sprintf("violation:%s:*", filter.DagID)
	}

	keys, err := s.redis.ListKeys(ctx, pattern)
	if err != nil {
		return nil, err
	}

	var violations []*models.Violation
	for _, key := range keys {
		var violation models.Violation
		if err := s.redis.Get(ctx, key, &violation); err != nil {
			continue
		}

		if filter.RuleID != "" && violation.RuleID != filter.RuleID {
			continue
		}
		if filter.TaskID != "" && violation.TaskID != filter.TaskID {
			continue
		}
		if !filter.StartTime.IsZero() && violation.Timestamp.Before(filter.StartTime) {
			continue
		}
		if !filter.EndTime.IsZero() && violation.Timestamp.After(filter.EndTime) {
			continue
		}
		if filter.SLAMissed != nil && violation.SLAMissed != *filter.SLAMissed {
			continue
		}
		if filter.Severity != "" && violation.Severity != filter.Severity {
			continue
		}

		violations = append(violations, &violation)
	}

	return violations, nil
}

func (s *ViolationStorage) GetViolationsByRule(ctx context.Context, ruleID string) ([]*models.Violation, error) {
	return s.GetViolations(ctx, ViolationFilter{RuleID: ruleID})
}

func (s *ViolationStorage) GetViolationsByDag(ctx context.Context, dagID string) ([]*models.Violation, error) {
	return s.GetViolations(ctx, ViolationFilter{DagID: dagID})
}

func (s *ViolationStorage) Close() error {
	return s.redis.Close()
}
