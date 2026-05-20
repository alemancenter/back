package repositories

import (
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
)

type DashboardRepository interface {
	ListActivities(logName, causerID string, offset, limit int) ([]models.ActivityLog, int64, error)
	CleanOldActivities(cutoff time.Time) (int64, error)
}

type dashboardRepository struct{}

func NewDashboardRepository() DashboardRepository {
	return &dashboardRepository{}
}

func (r *dashboardRepository) ListActivities(logName, causerID string, offset, limit int) ([]models.ActivityLog, int64, error) {
	var activities []models.ActivityLog
	var total int64

	query := database.DB().Model(&models.ActivityLog{})

	if logName != "" {
		query = query.Where("log_name = ?", logName)
	}
	if causerID != "" {
		query = query.Where("causer_id = ?", causerID)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.Order("created_at DESC").Limit(limit).Offset(offset).Find(&activities).Error
	return activities, total, err
}

func (r *dashboardRepository) CleanOldActivities(cutoff time.Time) (int64, error) {
	result := database.DB().Where("created_at < ?", cutoff).Delete(&models.ActivityLog{})
	return result.RowsAffected, result.Error
}
