package repositories

import (
	"strings"
	"time"

	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/models"
)

type NotificationRepository interface {
	List(userID uint, search, status string, offset, limit int) ([]models.Notification, int64, error)
	Stats(userID uint) (map[string]int64, error)
	GetLatest(userID uint, limit int) ([]models.Notification, error)
	GetUnreadCount(userID uint) (int64, error)
	MarkAsRead(id string, userID uint) error
	MarkAllRead(userID uint) error
	Create(notification *models.Notification) error
	CreateBulk(notifications []*models.Notification) error
	Delete(id string, userID uint) error
	Prune(cutoff time.Time) (int64, error)
	BulkMarkAsRead(ids []string, userID uint) error
	BulkDelete(ids []string, userID uint) error
}

type notificationRepository struct{}

func NewNotificationRepository() NotificationRepository {
	return &notificationRepository{}
}

func (r *notificationRepository) List(userID uint, search, status string, offset, limit int) ([]models.Notification, int64, error) {
	var notifications []models.Notification
	var total int64
	db := database.DB()

	query := db.Model(&models.Notification{}).
		Where("notifiable_type = ? AND notifiable_id = ?", "App\\Models\\User", userID)

	switch status {
	case "unread":
		query = query.Where("read_at IS NULL")
	case "read":
		query = query.Where("read_at IS NOT NULL")
	}
	if term := strings.TrimSpace(search); term != "" {
		like := "%" + term + "%"
		query = query.Where("type LIKE ? OR CAST(data AS CHAR) LIKE ?", like, like)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.Order("created_at DESC").Limit(limit).Offset(offset).Find(&notifications).Error
	return notifications, total, err
}

func (r *notificationRepository) Stats(userID uint) (map[string]int64, error) {
	db := database.DB()
	baseWhere := "notifiable_type = ? AND notifiable_id = ?"
	baseArgs := []interface{}{"App\\Models\\User", userID}
	stats := map[string]int64{"total": 0, "unread": 0, "read": 0, "today": 0}
	var total, unread, today int64
	if err := db.Model(&models.Notification{}).Where(baseWhere, baseArgs...).Count(&total).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&models.Notification{}).Where(baseWhere, baseArgs...).Where("read_at IS NULL").Count(&unread).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if err := db.Model(&models.Notification{}).Where(baseWhere, baseArgs...).Where("created_at >= ?", startOfDay).Count(&today).Error; err != nil {
		return nil, err
	}
	stats["total"] = total
	stats["unread"] = unread
	stats["read"] = total - unread
	stats["today"] = today
	return stats, nil
}

func (r *notificationRepository) GetLatest(userID uint, limit int) ([]models.Notification, error) {
	var notifications []models.Notification
	db := database.DB()

	err := db.Where("notifiable_type = ? AND notifiable_id = ?", "App\\Models\\User", userID).
		Order("created_at DESC").Limit(limit).Find(&notifications).Error

	return notifications, err
}

func (r *notificationRepository) CreateBulk(notifications []*models.Notification) error {
	if len(notifications) == 0 {
		return nil
	}
	return database.DB().Create(&notifications).Error
}

func (r *notificationRepository) GetUnreadCount(userID uint) (int64, error) {
	var unreadCount int64
	db := database.DB()

	err := db.Model(&models.Notification{}).
		Where("notifiable_type = ? AND notifiable_id = ? AND read_at IS NULL", "App\\Models\\User", userID).
		Count(&unreadCount).Error

	return unreadCount, err
}

func (r *notificationRepository) MarkAsRead(id string, userID uint) error {
	now := time.Now()
	return database.DB().Model(&models.Notification{}).
		Where("id = ? AND notifiable_id = ?", id, userID).
		Update("read_at", now).Error
}

func (r *notificationRepository) MarkAllRead(userID uint) error {
	now := time.Now()
	return database.DB().Model(&models.Notification{}).
		Where("notifiable_type = ? AND notifiable_id = ? AND read_at IS NULL", "App\\Models\\User", userID).
		Update("read_at", now).Error
}

func (r *notificationRepository) Create(notification *models.Notification) error {
	return database.DB().Create(notification).Error
}

func (r *notificationRepository) Delete(id string, userID uint) error {
	return database.DB().Where("id = ? AND notifiable_id = ?", id, userID).Delete(&models.Notification{}).Error
}

func (r *notificationRepository) Prune(cutoff time.Time) (int64, error) {
	result := database.DB().Where("read_at IS NOT NULL AND read_at < ?", cutoff).Delete(&models.Notification{})
	return result.RowsAffected, result.Error
}

func (r *notificationRepository) BulkMarkAsRead(ids []string, userID uint) error {
	now := time.Now()
	return database.DB().Model(&models.Notification{}).
		Where("id IN ? AND notifiable_id = ?", ids, userID).
		Update("read_at", now).Error
}

func (r *notificationRepository) BulkDelete(ids []string, userID uint) error {
	return database.DB().Where("id IN ? AND notifiable_id = ?", ids, userID).Delete(&models.Notification{}).Error
}
