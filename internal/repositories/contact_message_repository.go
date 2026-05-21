package repositories

import (
	"sync"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
)

type ContactMessageRepository interface {
	Create(msg *models.ContactMessage) error
	List(offset, limit int) ([]models.ContactMessage, int64, error)
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

func (r *contactMessageRepository) List(offset, limit int) ([]models.ContactMessage, int64, error) {
	if err := ensureContactMessageSchema(); err != nil {
		return nil, 0, err
	}

	var msgs []models.ContactMessage
	var total int64
	db := database.DB()

	if err := db.Model(&models.ContactMessage{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := db.Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&msgs).Error
	return msgs, total, err
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
