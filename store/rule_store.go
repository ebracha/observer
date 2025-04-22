package store

import (
	"context"
	"fmt"
	"time"

	"github.com/ebracha/airflow-observer/storage"
)

type Rule struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Type        string            `json:"type"`
	Threshold   float64           `json:"threshold"`
	Tags        map[string]string `json:"tags"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type RuleStore interface {
	StoreRule(ctx context.Context, rule *Rule) error
	GetRule(ctx context.Context, id string) (*Rule, error)
	ListRules(ctx context.Context) ([]*Rule, error)
	DeleteRule(ctx context.Context, id string) error
	GetRulesByType(ctx context.Context, ruleType string) ([]*Rule, error)
	GetRulesByTag(ctx context.Context, tagKey, tagValue string) ([]*Rule, error)
	Close() error
}

type RuleStorage struct {
	redis storage.Storage
}

func NewRuleStore(redis storage.Storage) RuleStore {
	return &RuleStorage{
		redis: redis,
	}
}

func (s *RuleStorage) StoreRule(ctx context.Context, rule *Rule) error {
	if rule.ID == "" {
		return fmt.Errorf("rule ID cannot be empty")
	}

	rule.UpdatedAt = time.Now()
	if rule.CreatedAt.IsZero() {
		rule.CreatedAt = rule.UpdatedAt
	}

	return s.redis.Set(ctx, rule.ID, rule, 0) // No expiration for rules
}

func (s *RuleStorage) GetRule(ctx context.Context, id string) (*Rule, error) {
	var rule Rule
	if err := s.redis.Get(ctx, id, &rule); err != nil {
		return nil, err
	}
	return &rule, nil
}

func (s *RuleStorage) ListRules(ctx context.Context) ([]*Rule, error) {
	keys, err := s.redis.ListKeys(ctx, "rule:*")
	if err != nil {
		return nil, err
	}

	var rules []*Rule
	for _, key := range keys {
		var rule Rule
		if err := s.redis.Get(ctx, key, &rule); err != nil {
			continue
		}
		rules = append(rules, &rule)
	}

	return rules, nil
}

func (s *RuleStorage) DeleteRule(ctx context.Context, id string) error {
	return s.redis.Delete(ctx, id)
}

func (s *RuleStorage) GetRulesByType(ctx context.Context, ruleType string) ([]*Rule, error) {
	rules, err := s.ListRules(ctx)
	if err != nil {
		return nil, err
	}

	var filtered []*Rule
	for _, rule := range rules {
		if rule.Type == ruleType {
			filtered = append(filtered, rule)
		}
	}

	return filtered, nil
}

func (s *RuleStorage) GetRulesByTag(ctx context.Context, tagKey, tagValue string) ([]*Rule, error) {
	rules, err := s.ListRules(ctx)
	if err != nil {
		return nil, err
	}

	var filtered []*Rule
	for _, rule := range rules {
		if value, ok := rule.Tags[tagKey]; ok && value == tagValue {
			filtered = append(filtered, rule)
		}
	}

	return filtered, nil
}

func (s *RuleStorage) Close() error {
	return s.redis.Close()
}
