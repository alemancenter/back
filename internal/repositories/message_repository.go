package repositories

import (
	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"gorm.io/gorm"
)

type MessageRepository interface {
	ListInbox(userID uint, offset, limit int) ([]models.Message, int64, error)
	ListSent(userID uint, offset, limit int) ([]models.Message, int64, error)
	ListDrafts(userID uint, offset, limit int) ([]models.Message, int64, error)
	FindOrCreateConversation(user1ID, user2ID uint) (*models.Conversation, error)
	CreateMessage(msg *models.Message) error
	GetMessage(msgID uint64, userID uint) (*models.Message, error)
	MarkAsRead(msgID uint64, userID uint) error
	ToggleImportant(msgID uint64, userID uint) error
	SoftDeleteMessage(msgID uint64, userID uint) error
}

type messageRepository struct{}

func NewMessageRepository() MessageRepository {
	return &messageRepository{}
}

func (r *messageRepository) ListInbox(userID uint, offset, limit int) ([]models.Message, int64, error) {
	var msgs []models.Message
	var total int64
	db := database.DB()

	query := db.Model(&models.Message{}).
		Joins("JOIN conversations ON conversations.id = messages.conversation_id").
		Where("(conversations.user1_id = ? OR conversations.user2_id = ?)", userID, userID).
		Where("messages.sender_id != ?", userID).
		Where("messages.is_draft = ? AND messages.is_deleted = ?", false, false).
		Preload("Sender").
		Preload("Conversation.User1").
		Preload("Conversation.User2")

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("messages.created_at DESC").Limit(limit).Offset(offset).Find(&msgs).Error
	return msgs, total, err
}

func (r *messageRepository) ListSent(userID uint, offset, limit int) ([]models.Message, int64, error) {
	var msgs []models.Message
	var total int64
	db := database.DB()

	query := db.Model(&models.Message{}).
		Where("sender_id = ? AND is_draft = ? AND is_deleted = ?", userID, false, false).
		Preload("Sender").
		Preload("Conversation.User1").
		Preload("Conversation.User2")

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("created_at DESC").Limit(limit).Offset(offset).Find(&msgs).Error
	return msgs, total, err
}

func (r *messageRepository) ListDrafts(userID uint, offset, limit int) ([]models.Message, int64, error) {
	var msgs []models.Message
	var total int64
	db := database.DB()

	query := db.Model(&models.Message{}).
		Where("sender_id = ? AND is_draft = ? AND is_deleted = ?", userID, true, false).
		Preload("Sender").
		Preload("Conversation.User1").
		Preload("Conversation.User2")

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("created_at DESC").Limit(limit).Offset(offset).Find(&msgs).Error
	return msgs, total, err
}

func (r *messageRepository) FindOrCreateConversation(user1ID, user2ID uint) (*models.Conversation, error) {
	var conv models.Conversation
	db := database.DB()

	err := db.Where(
		"(user1_id = ? AND user2_id = ?) OR (user1_id = ? AND user2_id = ?)",
		user1ID, user2ID, user2ID, user1ID,
	).First(&conv).Error

	if err == gorm.ErrRecordNotFound {
		conv = models.Conversation{
			User1ID: user1ID,
			User2ID: user2ID,
			Type:    "private",
		}
		if err = db.Create(&conv).Error; err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return &conv, nil
}

func (r *messageRepository) CreateMessage(msg *models.Message) error {
	return database.DB().Create(msg).Error
}

func (r *messageRepository) GetMessage(msgID uint64, userID uint) (*models.Message, error) {
	var msg models.Message
	err := database.DB().
		Joins("JOIN conversations ON conversations.id = messages.conversation_id").
		Where("messages.id = ?", msgID).
		Where("(conversations.user1_id = ? OR conversations.user2_id = ?)", userID, userID).
		Preload("Sender").
		Preload("Conversation.User1").
		Preload("Conversation.User2").
		First(&msg).Error
	return &msg, err
}

func (r *messageRepository) MarkAsRead(msgID uint64, userID uint) error {
	return database.DB().Exec(
		"UPDATE messages SET `read` = 1 WHERE id = ? AND sender_id != ?",
		msgID, userID,
	).Error
}

func (r *messageRepository) ToggleImportant(msgID uint64, userID uint) error {
	msg, err := r.GetMessage(msgID, userID)
	if err != nil {
		return err
	}
	return database.DB().Exec("UPDATE messages SET is_important = ? WHERE id = ?", !msg.IsImportant, msg.ID).Error
}

func (r *messageRepository) SoftDeleteMessage(msgID uint64, userID uint) error {
	return database.DB().Exec(
		"UPDATE messages SET is_deleted = 1 WHERE id = ? AND (sender_id = ? OR conversation_id IN (SELECT id FROM conversations WHERE user1_id = ? OR user2_id = ?))",
		msgID, userID, userID, userID,
	).Error
}
