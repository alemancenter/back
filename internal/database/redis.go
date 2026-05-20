package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alemancenter/fiber-api/internal/config"
	"github.com/alemancenter/fiber-api/pkg/logger"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type RedisKeyInfo struct {
	Key              string `json:"key"`
	TTL              int64  `json:"ttl"`
	TTLLabel         string `json:"ttl_label"`
	IsPersistent     bool   `json:"is_persistent"`
	Type             string `json:"type"`
	MemoryUsageBytes int64  `json:"memory_usage_bytes"`
}

type RedisStats struct {
	Healthy          map[string]bool `json:"healthy"`
	KeyspaceHits     int64           `json:"keyspace_hits"`
	KeyspaceMisses   int64           `json:"keyspace_misses"`
	HitRatio         float64         `json:"hit_ratio"`
	MemoryUsed       string          `json:"memory_used"`
	ConnectedClients int64           `json:"connected_clients"`
}

type RedisManager struct {
	default_ *redis.Client
	cache    *redis.Client
	queue    *redis.Client
	prefix   string
}

var (
	redisManager     *RedisManager
	redisManagerOnce sync.Once
)

// GetRedis returns the singleton Redis manager
func GetRedis() *RedisManager {
	redisManagerOnce.Do(func() {
		cfg := config.Get().Redis
		redisManager = &RedisManager{
			prefix:   cfg.Prefix,
			default_: newRedisClient(cfg, cfg.DB),
			cache:    newRedisClient(cfg, cfg.CacheDB),
			queue:    newRedisClient(cfg, cfg.QueueDB),
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := redisManager.default_.Ping(ctx).Err(); err != nil {
			logger.Fatal("failed to connect to Redis", zap.Error(err))
		}
		logger.Info("Redis connected", zap.String("addr", cfg.Addr()))
	})
	return redisManager
}

func newRedisClient(cfg config.RedisConfig, db int) *redis.Client {
	opts := &redis.Options{
		Addr:         cfg.Addr(),
		Password:     cfg.Password,
		DB:           db,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     20,
		MinIdleConns: 5,
		MaxRetries:   3,
	}
	return redis.NewClient(opts)
}

// Default returns the default Redis client
func (r *RedisManager) Default() *redis.Client { return r.default_ }

// Cache returns the cache Redis client
func (r *RedisManager) Cache() *redis.Client { return r.cache }

// Queue returns the queue Redis client
func (r *RedisManager) Queue() *redis.Client { return r.queue }

// Key builds a namespaced cache key
func (r *RedisManager) Key(parts ...string) string {
	key := r.prefix
	for _, part := range parts {
		key += ":" + part
	}
	return key
}

// CountryKey builds a country-specific cache key
func (r *RedisManager) CountryKey(country string, parts ...string) string {
	base := r.Key(country)
	for _, part := range parts {
		base += ":" + part
	}
	return base
}

// Set stores a value in cache
func (r *RedisManager) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return r.cache.Set(ctx, key, value, ttl).Err()
}

// Get retrieves a value from cache
func (r *RedisManager) Get(ctx context.Context, key string) (string, error) {
	return r.cache.Get(ctx, key).Result()
}

// Del deletes a key from cache
func (r *RedisManager) Del(ctx context.Context, keys ...string) error {
	return r.cache.Del(ctx, keys...).Err()
}

// Exists checks if a key exists
func (r *RedisManager) Exists(ctx context.Context, key string) (bool, error) {
	n, err := r.cache.Exists(ctx, key).Result()
	return n > 0, err
}

// IncrBy increments a key's value
func (r *RedisManager) IncrBy(ctx context.Context, key string, val int64) (int64, error) {
	return r.cache.IncrBy(ctx, key, val).Result()
}

// Expire sets expiry on a key
func (r *RedisManager) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return r.cache.Expire(ctx, key, ttl).Err()
}

// GetTTL returns remaining TTL
func (r *RedisManager) GetTTL(ctx context.Context, key string) (time.Duration, error) {
	return r.cache.TTL(ctx, key).Result()
}

// FlushCountryCache flushes all cache keys for a given country
func (r *RedisManager) FlushCountryCache(ctx context.Context, country string) error {
	pattern := fmt.Sprintf("%s:%s:*", r.prefix, country)
	return r.flushPattern(ctx, pattern)
}

// FlushPattern deletes all keys matching a pattern
func (r *RedisManager) flushPattern(ctx context.Context, pattern string) error {
	var cursor uint64
	for {
		keys, nextCursor, err := r.cache.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := r.cache.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return nil
}

// HealthCheck checks Redis connectivity
func (r *RedisManager) HealthCheck() map[string]bool {
	ctx := context.Background()
	return map[string]bool{
		"default": r.default_.Ping(ctx).Err() == nil,
		"cache":   r.cache.Ping(ctx).Err() == nil,
		"queue":   r.queue.Ping(ctx).Err() == nil,
	}
}

// GetInfo returns Redis server information
func (r *RedisManager) GetInfo(ctx context.Context) (string, error) {
	return r.default_.Info(ctx).Result()
}

// SetNX sets key only if it doesn't exist (for distributed locks)
func (r *RedisManager) SetNX(ctx context.Context, key string, value interface{}, ttl time.Duration) (bool, error) {
	return r.default_.SetNX(ctx, key, value, ttl).Result()
}

// ListKeys returns one bounded page of keys matching a pattern from the cache DB.
func (r *RedisManager) ListKeys(ctx context.Context, pattern string, limit, offset int) ([]string, bool, error) {
	if pattern == "" {
		pattern = "*"
	}
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	result := make([]string, 0, limit)
	var cursor uint64
	skipped := 0
	scanCount := int64(limit)
	if scanCount < 100 {
		scanCount = 100
	}

	for {
		keys, nextCursor, err := r.cache.Scan(ctx, cursor, pattern, scanCount).Result()
		if err != nil {
			return nil, false, err
		}
		for _, key := range keys {
			if skipped < offset {
				skipped++
				continue
			}
			if len(result) < limit {
				result = append(result, key)
				continue
			}
			return result, true, nil
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return result, false, nil
}

// ListKeyDetails returns one bounded page of keys with real TTL/type/memory metadata from the cache DB.
func (r *RedisManager) ListKeyDetails(ctx context.Context, pattern string, limit, offset int, ttlFilter string) ([]RedisKeyInfo, bool, error) {
	if pattern == "" {
		pattern = "*"
	}
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	ttlFilter = strings.ToLower(strings.TrimSpace(ttlFilter))

	result := make([]RedisKeyInfo, 0, limit)
	var cursor uint64
	skipped := 0
	scanCount := int64(limit)
	if scanCount < 100 {
		scanCount = 100
	}

	for {
		keys, nextCursor, err := r.cache.Scan(ctx, cursor, pattern, scanCount).Result()
		if err != nil {
			return nil, false, err
		}

		for _, key := range keys {
			info, err := r.keyInfo(ctx, key)
			if err != nil {
				continue
			}
			if ttlFilter == "persistent" && !info.IsPersistent {
				continue
			}
			if ttlFilter == "volatile" && info.IsPersistent {
				continue
			}

			if skipped < offset {
				skipped++
				continue
			}

			if len(result) < limit {
				result = append(result, info)
				continue
			}
			return result, true, nil
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return result, false, nil
}

func (r *RedisManager) keyInfo(ctx context.Context, key string) (RedisKeyInfo, error) {
	ttl, err := r.cache.TTL(ctx, key).Result()
	if err != nil {
		return RedisKeyInfo{}, err
	}
	typeName, _ := r.cache.Type(ctx, key).Result()
	memoryUsage, _ := r.cache.MemoryUsage(ctx, key).Result()

	seconds := int64(ttl.Seconds())
	info := RedisKeyInfo{
		Key:              key,
		TTL:              seconds,
		TTLLabel:         formatRedisTTL(ttl),
		IsPersistent:     ttl == -1*time.Second,
		Type:             typeName,
		MemoryUsageBytes: memoryUsage,
	}
	return info, nil
}

func formatRedisTTL(ttl time.Duration) string {
	switch ttl {
	case -2 * time.Second:
		return "missing"
	case -1 * time.Second:
		return "Persist"
	}
	if ttl < 0 {
		return "unknown"
	}
	if ttl < time.Minute {
		return fmt.Sprintf("%ds", int64(ttl.Seconds()))
	}
	if ttl < time.Hour {
		return fmt.Sprintf("%dm", int64(ttl.Minutes()))
	}
	if ttl < 24*time.Hour {
		return fmt.Sprintf("%dh", int64(ttl.Hours()))
	}
	return fmt.Sprintf("%dd", int64(ttl.Hours()/24))
}

// DeleteByPattern deletes all cache keys matching pattern and returns the number of deleted keys.
func (r *RedisManager) DeleteByPattern(ctx context.Context, pattern string) (int64, error) {
	var cursor uint64
	var deleted int64
	for {
		keys, nextCursor, err := r.cache.Scan(ctx, cursor, pattern, 500).Result()
		if err != nil {
			return deleted, err
		}
		if len(keys) > 0 {
			n, err := r.cache.Del(ctx, keys...).Result()
			if err != nil {
				return deleted, err
			}
			deleted += n
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return deleted, nil
}

// ExpireByPattern assigns a TTL to all cache keys matching pattern and returns the affected count.
func (r *RedisManager) ExpireByPattern(ctx context.Context, pattern string, ttl time.Duration) (int64, error) {
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	var cursor uint64
	var affected int64
	for {
		keys, nextCursor, err := r.cache.Scan(ctx, cursor, pattern, 500).Result()
		if err != nil {
			return affected, err
		}
		for _, key := range keys {
			ok, err := r.cache.Expire(ctx, key, ttl).Result()
			if err != nil {
				return affected, err
			}
			if ok {
				affected++
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return affected, nil
}

// SetJSON marshals value and stores it in the cache database.
func (r *RedisManager) SetJSON(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return r.Set(ctx, key, data, ttl)
}

// GetJSON reads a JSON value from the cache database into dest.
func (r *RedisManager) GetJSON(ctx context.Context, key string, dest interface{}) bool {
	raw, err := r.Get(ctx, key)
	if err != nil || raw == "" {
		return false
	}
	return json.Unmarshal([]byte(raw), dest) == nil
}

// RememberJSON is a production-safe cache helper used by repositories/services.
func (r *RedisManager) RememberJSON(ctx context.Context, key string, ttl time.Duration, dest interface{}, fetch func() (interface{}, error)) error {
	if r.GetJSON(ctx, key, dest) {
		return nil
	}
	value, err := fetch()
	if err != nil {
		return err
	}
	if err := r.SetJSON(ctx, key, value, ttl); err != nil {
		return err
	}
	data, _ := json.Marshal(value)
	return json.Unmarshal(data, dest)
}

// WithLock executes fn while holding a Redis-backed lock. It returns locked=false when another request is already running.
func (r *RedisManager) WithLock(ctx context.Context, key string, ttl time.Duration, fn func() error) (locked bool, err error) {
	token := strconv.FormatInt(time.Now().UnixNano(), 10)
	ok, err := r.default_.SetNX(ctx, key, token, ttl).Result()
	if err != nil || !ok {
		return ok, err
	}
	defer func() {
		current, getErr := r.default_.Get(ctx, key).Result()
		if getErr == nil && current == token {
			_ = r.default_.Del(ctx, key).Err()
		}
	}()
	return true, fn()
}

// Stats returns a compact Redis monitoring summary.
func (r *RedisManager) Stats(ctx context.Context) RedisStats {
	stats := RedisStats{Healthy: r.HealthCheck()}
	info, err := r.cache.Info(ctx, "stats", "memory", "clients").Result()
	if err != nil {
		return stats
	}
	for _, line := range strings.Split(info, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		key, value := parts[0], strings.TrimSpace(parts[1])
		switch key {
		case "keyspace_hits":
			stats.KeyspaceHits, _ = strconv.ParseInt(value, 10, 64)
		case "keyspace_misses":
			stats.KeyspaceMisses, _ = strconv.ParseInt(value, 10, 64)
		case "used_memory_human":
			stats.MemoryUsed = value
		case "connected_clients":
			stats.ConnectedClients, _ = strconv.ParseInt(value, 10, 64)
		}
	}
	total := stats.KeyspaceHits + stats.KeyspaceMisses
	if total > 0 {
		stats.HitRatio = float64(stats.KeyspaceHits) / float64(total)
	}
	return stats
}

// Redis is a convenience function to get the Redis manager
func Redis() *RedisManager {
	return GetRedis()
}
