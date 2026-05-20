package services

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

type CacheService interface {
	Get(key string, dest any) bool
	Set(key string, value any, ttl time.Duration) error
	Delete(key string) error
	DeletePattern(pattern string) error
}

type cacheService struct {
	client *redis.Client
	ctx    context.Context
}

func NewCacheService(client *redis.Client) CacheService {
	return &cacheService{
		client: client,
		ctx:    context.Background(),
	}
}

func (s *cacheService) Get(key string, dest any) bool {
	if s == nil || s.client == nil {
		return false
	}

	data, err := s.client.Get(s.ctx, key).Bytes()
	if err != nil {
		return false
	}

	return json.Unmarshal(data, dest) == nil
}

func (s *cacheService) Set(key string, value any, ttl time.Duration) error {
	if s == nil || s.client == nil {
		return nil
	}

	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	return s.client.Set(s.ctx, key, data, ttl).Err()
}

func (s *cacheService) Delete(key string) error {
	if s == nil || s.client == nil {
		return nil
	}

	return s.client.Del(s.ctx, key).Err()
}

func (s *cacheService) DeletePattern(pattern string) error {
	if s == nil || s.client == nil {
		return nil
	}

	iter := s.client.Scan(s.ctx, 0, pattern, 100).Iterator()
	for iter.Next(s.ctx) {
		_ = s.client.Del(s.ctx, iter.Val()).Err()
	}

	return iter.Err()
}
