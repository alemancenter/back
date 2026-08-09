package repositories

import (
	"strings"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"gorm.io/gorm"
)

type MessageRepository interface {
	ListInbox(userID uint, search string, importantOnly bool, offset, limit int) ([]models.Message, int64, error)
	ListSent(userID uint, search string, importantOnly bool, offset, limit int) ([]models.Message, int64, error)
	ListDrafts(userID uint, search string, importantOnly bool, offset, limit int) ([]models.Message, int64, error)
	Stats(userID uint) (map[string]int64, error)
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

func applyMessageFilters(query *gorm.DB, search string, importantOnly bool) *gorm.DB {
	if importantOnly {
		query = query.Where("messages.is_important = ?", true)
	}
	if search = strings.TrimSpace(search); search != "" {
		term := "%" + strings.ToLower(search) + "%"
		query = query.Where("LOWER(messages.subject) LIKE ? OR LOWER(messages.body) LIKE ? OR LOWER(message_senders.name) LIKE ? OR LOWER(message_senders.email) LIKE ? OR LOWER(conversation_user1.name) LIKE ? OR LOWER(conversation_user1.email) LIKE ? OR LOWER(conversation_user2.name) LIKE ? OR LOWER(conversation_user2.email) LIKE ?", term, term, term, term, term, term, term, term)
	}
	return query
}

func messageListQuery(db *gorm.DB) *gorm.DB {
	return db.Model(&models.Message{}).
		Joins("JOIN conversations ON conversations.id = messages.conversation_id").
		Joins("LEFT JOIN users AS message_senders ON message_senders.id = messages.sender_id").
		Joins("LEFT JOIN users AS conversation_user1 ON conversation_user1.id = conversations.user1_id").
		Joins("LEFT JOIN users AS conversation_user2 ON conversation_user2.id = conversations.user2_id")
}

func preloadMessageUsers(query *gorm.DB) *gorm.DB {
	return query.Preload("Sender").Preload("Conversation.User1").Preload("Conversation.User2")
}

func (r *messageRepository) ListInbox(userID uint, search string, importantOnly bool, offset, limit int) ([]models.Message, int64, error) {
	var msgs []models.Message
	var total int64
	db := database.DB()

	query := messageListQuery(db).
		Where("(conversations.user1_id = ? OR conversations.user2_id = ?)", userID, userID).
		Where("messages.sender_id != ?", userID).
		Where("messages.is_draft = ? AND messages.is_deleted = ?", false, false)
	query = preloadMessageUsers(applyMessageFilters(query, search, importantOnly))

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("messages.created_at DESC").Limit(limit).Offset(offset).Find(&msgs).Error
	return msgs, total, err
}

func (r *messageRepository) ListSent(userID uint, search string, importantOnly bool, offset, limit int) ([]models.Message, int64, error) {
	var msgs []models.Message
	var total int64
	db := database.DB()

	query := messageListQuery(db).
		Where("messages.sender_id = ? AND messages.is_draft = ? AND messages.is_deleted = ?", userID, false, false)
	query = preloadMessageUsers(applyMessageFilters(query, search, importantOnly))

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("messages.created_at DESC").Limit(limit).Offset(offset).Find(&msgs).Error
	return msgs, total, err
}

func (r *messageRepository) ListDrafts(userID uint, search string, importantOnly bool, offset, limit int) ([]models.Message, int64, error) {
	var msgs []models.Message
	var total int64
	db := database.DB()

	query := messageListQuery(db).
		Where("messages.sender_id = ? AND messages.is_draft = ? AND messages.is_deleted = ?", userID, true, false)
	query = preloadMessageUsers(applyMessageFilters(query, search, importantOnly))

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("messages.created_at DESC").Limit(limit).Offset(offset).Find(&msgs).Error
	return msgs, total, err
}

func (r *messageRepository) Stats(userID uint) (map[string]int64, error) {
	db := database.DB()
	stats := map[string]int64{"inbox": 0, "unread": 0, "sent": 0, "drafts": 0, "important": 0, "today": 0}
	var inboxCount, unreadCount, sentCount, draftsCount, importantCount, todayCount int64
	participant := "messages.conversation_id IN (SELECT id FROM conversations WHERE user1_id = ? OR user2_id = ?)"
	inbox := func() *gorm.DB {
		return db.Model(&models.Message{}).Where(participant, userID, userID).Where("sender_id != ? AND is_draft = ? AND is_deleted = ?", userID, false, false)
	}
	if err := inbox().Count(&inboxCount).Error; err != nil {
		return nil, err
	}
	if err := inbox().Where("`read` = ?", false).Count(&unreadCount).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&models.Message{}).Where("sender_id = ? AND is_draft = ? AND is_deleted = ?", userID, false, false).Count(&sentCount).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&models.Message{}).Where("sender_id = ? AND is_draft = ? AND is_deleted = ?", userID, true, false).Count(&draftsCount).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&models.Message{}).Where(participant, userID, userID).Where("is_deleted = ? AND is_important = ?", false, true).Count(&importantCount).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if err := db.Model(&models.Message{}).Where(participant, userID, userID).Where("is_deleted = ? AND created_at >= ?", false, startOfDay).Count(&todayCount).Error; err != nil {
		return nil, err
	}
	stats["inbox"], stats["unread"], stats["sent"] = inboxCount, unreadCount, sentCount
	stats["drafts"], stats["important"], stats["today"] = draftsCount, importantCount, todayCount
	return stats, nil
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
		"UPDATE messages SET `read` = 1 WHERE id = ? AND sender_id != ? AND conversation_id IN (SELECT id FROM conversations WHERE user1_id = ? OR user2_id = ?)",
		msgID, userID, userID, userID,
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
