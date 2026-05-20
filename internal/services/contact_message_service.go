package services

import (
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
)

type ContactMessageService interface {
	Create(msg *models.ContactMessage) error
	List(offset, limit int) ([]models.ContactMessage, int64, error)
	Get(id uint) (*models.ContactMessage, error)
	MarkAsRead(id uint) error
	Delete(id uint) error
}

type contactMessageService struct {
	repo repositories.ContactMessageRepository
}

func NewContactMessageService(repo repositories.ContactMessageRepository) ContactMessageService {
	return &contactMessageService{repo: repo}
}

func (s *contactMessageService) Create(msg *models.ContactMessage) error {
	return s.repo.Create(msg)
}

func (s *contactMessageService) List(offset, limit int) ([]models.ContactMessage, int64, error) {
	return s.repo.List(offset, limit)
}

func (s *contactMessageService) Get(id uint) (*models.ContactMessage, error) {
	return s.repo.Get(id)
}

func (s *contactMessageService) MarkAsRead(id uint) error {
	return s.repo.MarkAsRead(id)
}

func (s *contactMessageService) Delete(id uint) error {
	return s.repo.Delete(id)
}
