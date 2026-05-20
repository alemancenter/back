package repositories

import (
	"context"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
)

type RedisRepository interface {
	ListKeys(ctx context.Context, pattern string, limit, offset int, ttlFilter string) ([]database.RedisKeyInfo, bool, error)
	SetKey(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	DeleteKey(ctx context.Context, key string) error
	ExpireKey(ctx context.Context, key string, ttl time.Duration) error
	DeleteByPattern(ctx context.Context, pattern string) (int64, error)
	ExpireByPattern(ctx context.Context, pattern string, ttl time.Duration) (int64, error)
	HealthCheck() map[string]bool
	GetInfo(ctx context.Context) (string, error)
}

type redisRepository struct{}

func NewRedisRepository() RedisRepository {
	return &redisRepository{}
}

func (r *redisRepository) ListKeys(ctx context.Context, pattern string, limit, offset int, ttlFilter string) ([]database.RedisKeyInfo, bool, error) {
	return database.Redis().ListKeyDetails(ctx, pattern, limit, offset, ttlFilter)
}

func (r *redisRepository) SetKey(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return database.Redis().Cache().Set(ctx, key, value, ttl).Err()
}

func (r *redisRepository) DeleteKey(ctx context.Context, key string) error {
	return database.Redis().Cache().Del(ctx, key).Err()
}

func (r *redisRepository) ExpireKey(ctx context.Context, key string, ttl time.Duration) error {
	return database.Redis().Cache().Expire(ctx, key, ttl).Err()
}

func (r *redisRepository) DeleteByPattern(ctx context.Context, pattern string) (int64, error) {
	return database.Redis().DeleteByPattern(ctx, pattern)
}

func (r *redisRepository) ExpireByPattern(ctx context.Context, pattern string, ttl time.Duration) (int64, error) {
	return database.Redis().ExpireByPattern(ctx, pattern, ttl)
}

func (r *redisRepository) HealthCheck() map[string]bool {
	return database.Redis().HealthCheck()
}

func (r *redisRepository) GetInfo(ctx context.Context) (string, error) {
	return database.Redis().GetInfo(ctx)
}
