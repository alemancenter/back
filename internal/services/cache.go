package services

import (
	"context"
	"encoding/json"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/pkg/logger"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

var requestGroup singleflight.Group

// GetOrSet tries the Redis cache first. On a miss it calls fetch(), caches the
// result as JSON, and returns it. T must be JSON-serializable.
// Uses singleflight to prevent Cache Stampede (multiple concurrent requests hitting the DB).
func GetOrSet[T any](ctx context.Context, key string, ttl time.Duration, fetch func() (T, error)) (T, error) {
	rdb := database.Redis()

	// Try cache first
	if cached, err := rdb.Get(ctx, key); err == nil {
		var result T
		if json.Unmarshal([]byte(cached), &result) == nil {
			logger.Debug("cache hit:", zap.String("key", key))
			return result, nil
		}
	}

	logger.Debug("cache miss:", zap.String("key", key))

	// Cache miss - use singleflight to ensure only one fetch executes
	v, err, _ := requestGroup.Do(key, func() (interface{}, error) {
		result, fetchErr := fetch()
		if fetchErr != nil {
			return result, MapError(fetchErr)
		}

		if data, marshalErr := json.Marshal(result); marshalErr == nil {
			_ = rdb.Set(ctx, key, data, ttl)
		}
		return result, nil
	})

	if err != nil {
		var zero T
		return zero, err
	}

	return v.(T), nil
}

// InvalidateCache deletes one or more cache keys immediately.
func InvalidateCache(keys ...string) {
	if len(keys) == 0 {
		return
	}
	database.Redis().Del(context.Background(), keys...)
}
