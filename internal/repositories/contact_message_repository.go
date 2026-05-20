package repositories

import (
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

func NewContactMessageRepository() ContactMessageRepository {
	return &contactMessageRepository{}
}

func (r *contactMessageRepository) Create(msg *models.ContactMessage) error {
	return database.DB().Create(msg).Error
}

func (r *contactMessageRepository) List(offset, limit int) ([]models.ContactMessage, int64, error) {
	var msgs []models.ContactMessage
	var total int64
	db := database.DB()

	if err := db.Model(&models.ContactMessage{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := db.Order("created_at DESC").Limit(limit).Offset(offset).Find(&msgs).Error
	return msgs, total, err
}

func (r *contactMessageRepository) Get(id uint) (*models.ContactMessage, error) {
	var msg models.ContactMessage
	err := database.DB().First(&msg, id).Error
	return &msg, err
}

func (r *contactMessageRepository) MarkAsRead(id uint) error {
	return database.DB().Exec("UPDATE contact_messages SET `read` = 1 WHERE id = ?", id).Error
}

func (r *contactMessageRepository) Delete(id uint) error {
	return database.DB().Delete(&models.ContactMessage{}, id).Error
}
