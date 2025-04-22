package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Storage interface {
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Get(ctx context.Context, key string, value interface{}) error
	Delete(ctx context.Context, key string) error
	ListKeys(ctx context.Context, pattern string) ([]string, error)
	Close() error
}

type RedisStore struct {
	client *redis.Client
	prefix string
}

func NewRedisStore(addr string, password string, db int, prefix string) (Storage, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisStore{
		client: client,
		prefix: prefix,
	}, nil
}

func (s *RedisStore) buildKey(key string) string {
	return fmt.Sprintf("%s:%s", s.prefix, key)
}

func (s *RedisStore) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	return s.client.Set(ctx, s.buildKey(key), data, expiration).Err()
}

func (s *RedisStore) Get(ctx context.Context, key string, value interface{}) error {
	data, err := s.client.Get(ctx, s.buildKey(key)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return fmt.Errorf("key not found: %w", err)
		}
		return fmt.Errorf("failed to get value: %w", err)
	}

	return json.Unmarshal(data, value)
}

func (s *RedisStore) Delete(ctx context.Context, key string) error {
	return s.client.Del(ctx, s.buildKey(key)).Err()
}

func (s *RedisStore) ListKeys(ctx context.Context, pattern string) ([]string, error) {
	keys, err := s.client.Keys(ctx, s.buildKey(pattern)).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list keys: %w", err)
	}

	for i, key := range keys {
		keys[i] = key[len(s.prefix)+1:]
	}

	return keys, nil
}

func (s *RedisStore) Exists(ctx context.Context, key string) (bool, error) {
	exists, err := s.client.Exists(ctx, s.buildKey(key)).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check key existence: %w", err)
	}
	return exists > 0, nil
}

func (s *RedisStore) Close() error {
	return s.client.Close()
}
