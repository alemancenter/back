package services

import (
	"time"

	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
)

type DashboardService interface {
	ListActivities(logName, causerID string, offset, limit int) ([]models.ActivityLog, int64, error)
	CleanOldActivities() (int64, error)
}

type CleanActivitiesResponse struct {
	Deleted int64 `json:"deleted"`
}

type dashboardService struct {
	repo repositories.DashboardRepository
}

func NewDashboardService(repo repositories.DashboardRepository) DashboardService {
	return &dashboardService{repo: repo}
}

func (s *dashboardService) ListActivities(logName, causerID string, offset, limit int) ([]models.ActivityLog, int64, error) {
	return s.repo.ListActivities(logName, causerID, offset, limit)
}

func (s *dashboardService) CleanOldActivities() (int64, error) {
	cutoff := time.Now().AddDate(0, -3, 0) // 3 months ago
	return s.repo.CleanOldActivities(cutoff)
}
