package services

import (
	"context"
	"runtime"
	"time"

	"github.com/alemancenter/fiber-api/internal/config"
	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/monitoring"
	"github.com/alemancenter/fiber-api/internal/repositories"
)

type HealthStatusResponse struct {
	Status     string              `json:"status"`
	AppName    string              `json:"app_name"`
	Env        string              `json:"env"`
	Uptime     string              `json:"uptime"`
	Databases  map[string]bool     `json:"databases"`
	Redis      map[string]bool     `json:"redis"`
	RedisStats database.RedisStats `json:"redis_stats"`
	Metrics    monitoring.Snapshot `json:"metrics"`
	Memory     MemoryStats         `json:"memory"`
	Timestamp  time.Time           `json:"timestamp"`
}

type MemoryStats struct {
	AllocMB    uint64 `json:"alloc_mb"`
	SysMB      uint64 `json:"sys_mb"`
	NumGC      uint32 `json:"num_gc"`
	Goroutines int    `json:"goroutines"`
}

type HealthService interface {
	GetHealthStatus() (HealthStatusResponse, int, bool)
}

type PingResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type healthService struct {
	repo      repositories.HealthRepository
	startTime time.Time
}

func NewHealthService(repo repositories.HealthRepository) HealthService {
	return &healthService{repo: repo, startTime: time.Now()}
}

func (s *healthService) GetHealthStatus() (HealthStatusResponse, int, bool) {
	cfg := config.Get()
	dbHealth := s.repo.CheckDatabases()
	redisHealth := s.repo.CheckRedis()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	allHealthy := true
	for _, ok := range dbHealth {
		if !ok {
			allHealthy = false
			break
		}
	}
	for _, ok := range redisHealth {
		if !ok {
			allHealthy = false
			break
		}
	}

	status := "healthy"
	httpStatus := 200 // fiber.StatusOK
	if !allHealthy {
		status = "degraded"
		httpStatus = 503 // fiber.StatusServiceUnavailable
	}

	return HealthStatusResponse{
		Status:     status,
		AppName:    cfg.App.Name,
		Env:        cfg.App.Env,
		Uptime:     time.Since(s.startTime).String(),
		Databases:  dbHealth,
		Redis:      redisHealth,
		RedisStats: database.Redis().Stats(context.Background()),
		Metrics:    monitoring.SnapshotData(),
		Memory: MemoryStats{
			AllocMB:    memStats.Alloc / 1024 / 1024,
			SysMB:      memStats.Sys / 1024 / 1024,
			NumGC:      memStats.NumGC,
			Goroutines: runtime.NumGoroutine(),
		},
		Timestamp: time.Now().UTC(),
	}, httpStatus, allHealthy
}
