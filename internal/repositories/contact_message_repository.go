package repositories

import (
	"strings"
	"sync"
	"time"

	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/models"
)

type ContactMessageRepository interface {
	Create(msg *models.ContactMessage) error
	List(search, readStatus string, offset, limit int) ([]models.ContactMessage, int64, error)
	Stats() (map[string]int64, error)
	Get(id uint) (*models.ContactMessage, error)
	MarkAsRead(id uint) error
	Delete(id uint) error
}

type contactMessageRepository struct{}

var contactMessageSchema = struct {
	sync.Mutex
	checked bool
}{checked: false}

func NewContactMessageRepository() ContactMessageRepository {
	return &contactMessageRepository{}
}

// ensureContactMessageSchema keeps the dashboard contact inbox compatible with
// older deployments where the public contact form existed before the dashboard
// inbox was introduced. Without this guard, /dashboard/messages?tab=contact can
// fail with a 500 if contact_messages or one of its newer columns is missing.
func ensureContactMessageSchema() error {
	contactMessageSchema.Lock()
	defer contactMessageSchema.Unlock()

	if contactMessageSchema.checked {
		return nil
	}

	if err := database.DB().AutoMigrate(&models.ContactMessage{}); err != nil {
		return err
	}
	contactMessageSchema.checked = true
	return nil
}

func (r *contactMessageRepository) Create(msg *models.ContactMessage) error {
	if err := ensureContactMessageSchema(); err != nil {
		return err
	}
	return database.DB().Create(msg).Error
}

func (r *contactMessageRepository) List(search, readStatus string, offset, limit int) ([]models.ContactMessage, int64, error) {
	if err := ensureContactMessageSchema(); err != nil {
		return nil, 0, err
	}

	var msgs []models.ContactMessage
	var total int64
	db := database.DB().Model(&models.ContactMessage{})
	if term := strings.TrimSpace(search); term != "" {
		like := "%" + term + "%"
		db = db.Where("name LIKE ? OR email LIKE ? OR phone LIKE ? OR subject LIKE ? OR message LIKE ?", like, like, like, like, like)
	}
	switch readStatus {
	case "read":
		db = db.Where("`read` = ?", true)
	case "unread":
		db = db.Where("`read` = ?", false)
	}

	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := db.Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&msgs).Error
	return msgs, total, err
}

func (r *contactMessageRepository) Stats() (map[string]int64, error) {
	if err := ensureContactMessageSchema(); err != nil {
		return nil, err
	}
	db := database.DB().Model(&models.ContactMessage{})
	stats := map[string]int64{"total": 0, "unread": 0, "read": 0, "today": 0}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, err
	}
	var unread int64
	if err := db.Where("`read` = ?", false).Count(&unread).Error; err != nil {
		return nil, err
	}
	stats["total"] = total
	stats["unread"] = unread
	stats["read"] = total - unread
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var today int64
	if err := db.Where("created_at >= ?", startOfDay).Count(&today).Error; err != nil {
		return nil, err
	}
	stats["today"] = today
	return stats, nil
}

func (r *contactMessageRepository) Get(id uint) (*models.ContactMessage, error) {
	if err := ensureContactMessageSchema(); err != nil {
		return nil, err
	}
	var msg models.ContactMessage
	err := database.DB().First(&msg, id).Error
	return &msg, err
}

func (r *contactMessageRepository) MarkAsRead(id uint) error {
	if err := ensureContactMessageSchema(); err != nil {
		return err
	}
	return database.DB().Model(&models.ContactMessage{}).Where("id = ?", id).Update("read", true).Error
}

func (r *contactMessageRepository) Delete(id uint) error {
	if err := ensureContactMessageSchema(); err != nil {
		return err
	}
	return database.DB().Delete(&models.ContactMessage{}, id).Error
}
