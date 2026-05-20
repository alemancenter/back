package services

import (
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
)

type MessageService interface {
	ListInbox(userID uint, offset, limit int) ([]models.Message, int64, error)
	ListSent(userID uint, offset, limit int) ([]models.Message, int64, error)
	ListDrafts(userID uint, offset, limit int) ([]models.Message, int64, error)
	SendMessage(senderID uint, recipientID uint, subject, body string) (*models.Message, error)
	SaveDraft(senderID uint, recipientID uint, subject, body string) (*models.Message, error)
	GetMessage(msgID uint64, userID uint) (*models.Message, error)
	MarkAsRead(msgID uint64, userID uint) error
	ToggleImportant(msgID uint64, userID uint) error
	DeleteMessage(msgID uint64, userID uint) error
}

type messageService struct {
	repo repositories.MessageRepository
}

func NewMessageService(repo repositories.MessageRepository) MessageService {
	return &messageService{repo: repo}
}

func populateRecipient(msg *models.Message, myID uint) {
	if msg.Conversation == nil {
		return
	}
	if msg.Conversation.User1ID == myID {
		msg.Recipient = msg.Conversation.User2
	} else {
		msg.Recipient = msg.Conversation.User1
	}
}

func (s *messageService) ListInbox(userID uint, offset, limit int) ([]models.Message, int64, error) {
	msgs, total, err := s.repo.ListInbox(userID, offset, limit)
	if err == nil {
		for i := range msgs {
			populateRecipient(&msgs[i], userID)
		}
	}
	return msgs, total, MapError(err)
}

func (s *messageService) ListSent(userID uint, offset, limit int) ([]models.Message, int64, error) {
	msgs, total, err := s.repo.ListSent(userID, offset, limit)
	if err == nil {
		for i := range msgs {
			populateRecipient(&msgs[i], userID)
		}
	}
	return msgs, total, MapError(err)
}

func (s *messageService) ListDrafts(userID uint, offset, limit int) ([]models.Message, int64, error) {
	msgs, total, err := s.repo.ListDrafts(userID, offset, limit)
	if err == nil {
		for i := range msgs {
			populateRecipient(&msgs[i], userID)
		}
	}
	return msgs, total, MapError(err)
}

func (s *messageService) SendMessage(senderID uint, recipientID uint, subject, body string) (*models.Message, error) {
	conv, err := s.repo.FindOrCreateConversation(senderID, recipientID)
	if err != nil {
		return nil, MapError(err)
	}

	msg := &models.Message{
		ConversationID: conv.ID,
		SenderID:       senderID,
		Subject:        subject,
		Body:           body,
		IsDraft:        false,
	}

	err = s.repo.CreateMessage(msg)
	return msg, MapError(err)
}

func (s *messageService) SaveDraft(senderID uint, recipientID uint, subject, body string) (*models.Message, error) {
	msg := &models.Message{
		SenderID: senderID,
		Subject:  subject,
		Body:     body,
		IsDraft:  true,
	}

	if recipientID > 0 {
		conv, err := s.repo.FindOrCreateConversation(senderID, recipientID)
		if err != nil {
			return nil, MapError(err)
		}
		msg.ConversationID = conv.ID
	}

	err := s.repo.CreateMessage(msg)
	return msg, MapError(err)
}

func (s *messageService) GetMessage(msgID uint64, userID uint) (*models.Message, error) {
	msg, err := s.repo.GetMessage(msgID, userID)
	if err != nil {
		return nil, MapError(err)
	}

	populateRecipient(msg, userID)

	if msg.SenderID != userID && !msg.Read {
		if err := s.repo.MarkAsRead(uint64(msg.ID), userID); err == nil {
			msg.Read = true
		}
	}

	return msg, nil
}

func (s *messageService) MarkAsRead(msgID uint64, userID uint) error {
	return s.repo.MarkAsRead(msgID, userID)
}

func (s *messageService) ToggleImportant(msgID uint64, userID uint) error {
	return s.repo.ToggleImportant(msgID, userID)
}

func (s *messageService) DeleteMessage(msgID uint64, userID uint) error {
	return s.repo.SoftDeleteMessage(msgID, userID)
}
