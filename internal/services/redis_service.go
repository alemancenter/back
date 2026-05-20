package services

import (
	"context"
	"strings"
	"time"

	"github.com/alemancenter/fiber-api/internal/repositories"
)

type RedisService interface {
	ListKeys(ctx context.Context, pattern string, limit, offset int, ttlFilter string) ([]RedisKeyInfo, bool, error)
	SetKey(ctx context.Context, key string, value interface{}, ttl time.Duration, persist bool) error
	DeleteKey(ctx context.Context, key string) error
	ExpireKey(ctx context.Context, key string, ttl time.Duration) error
	CleanExpired(ctx context.Context) error
	CleanLegacyIPLocationKeys(ctx context.Context) (int64, error)
	ExpireLegacyIPLocationKeys(ctx context.Context, ttl time.Duration) (int64, error)
	TestConnection() (map[string]bool, bool)
	GetInfo(ctx context.Context) (string, error)
}

type RedisKeyInfo struct {
	Key              string `json:"key"`
	Value            string `json:"value,omitempty"`
	TTL              int64  `json:"ttl"`
	TTLLabel         string `json:"ttl_label"`
	IsPersistent     bool   `json:"is_persistent"`
	Type             string `json:"type"`
	MemoryUsageBytes int64  `json:"memory_usage_bytes"`
}

type RedisKeysResponse struct {
	Data        []RedisKeyInfo `json:"data"`
	Keys        []string       `json:"keys,omitempty"`
	Count       int            `json:"count"`
	CurrentPage int            `json:"current_page"`
	PerPage     int            `json:"per_page"`
	Total       int            `json:"total"`
	LastPage    int            `json:"last_page"`
	From        int            `json:"from"`
	To          int            `json:"to"`
	HasMore     bool           `json:"has_more"`
}

type RedisInfoResponse struct {
	Info string `json:"info"`
}

type redisService struct {
	repo repositories.RedisRepository
}

func NewRedisService(repo repositories.RedisRepository) RedisService {
	return &redisService{repo: repo}
}

func (s *redisService) ListKeys(ctx context.Context, pattern string, limit, offset int, ttlFilter string) ([]RedisKeyInfo, bool, error) {
	rows, hasMore, err := s.repo.ListKeys(ctx, pattern, limit, offset, ttlFilter)
	if err != nil {
		return nil, false, err
	}

	out := make([]RedisKeyInfo, 0, len(rows))
	for _, row := range rows {
		out = append(out, RedisKeyInfo{
			Key:              row.Key,
			TTL:              row.TTL,
			TTLLabel:         row.TTLLabel,
			IsPersistent:     row.IsPersistent,
			Type:             row.Type,
			MemoryUsageBytes: row.MemoryUsageBytes,
		})
	}
	return out, hasMore, nil
}

func (s *redisService) SetKey(ctx context.Context, key string, value interface{}, ttl time.Duration, persist bool) error {
	if !persist && ttl <= 0 {
		ttl = time.Hour
	}
	if persist {
		ttl = 0
	}
	return s.repo.SetKey(ctx, key, value, ttl)
}

func (s *redisService) DeleteKey(ctx context.Context, key string) error {
	return s.repo.DeleteKey(ctx, key)
}

func (s *redisService) ExpireKey(ctx context.Context, key string, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	return s.repo.ExpireKey(ctx, key, ttl)
}

func (s *redisService) CleanExpired(ctx context.Context) error {
	// Redis automatically removes expired keys. This endpoint is kept for backward compatibility.
	return nil
}

func (s *redisService) CleanLegacyIPLocationKeys(ctx context.Context) (int64, error) {
	return s.repo.DeleteByPattern(ctx, "*_cache_ip_location_*")
}

func (s *redisService) ExpireLegacyIPLocationKeys(ctx context.Context, ttl time.Duration) (int64, error) {
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	return s.repo.ExpireByPattern(ctx, "*_cache_ip_location_*", ttl)
}

func (s *redisService) TestConnection() (map[string]bool, bool) {
	health := s.repo.HealthCheck()
	allOk := true
	for _, ok := range health {
		if !ok {
			allOk = false
			break
		}
	}
	return health, allOk
}

func (s *redisService) GetInfo(ctx context.Context) (string, error) {
	return s.repo.GetInfo(ctx)
}

func NormalizeRedisTTLFilter(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "persistent", "persist", "no_ttl":
		return "persistent"
	case "volatile", "expiring", "ttl":
		return "volatile"
	default:
		return "all"
	}
}
