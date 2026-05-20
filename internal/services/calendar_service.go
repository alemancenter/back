package services

import (
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
	"github.com/alemancenter/fiber-api/internal/utils"
)

type DatabaseInfo struct {
	ID   int    `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

type EventInput struct {
	Title       string `json:"title" validate:"required,min=2,max=500"`
	Description string `json:"description"`
	EventDate   string `json:"event_date" validate:"required"`
}

type CalendarService interface {
	ListEvents(countryID database.CountryID, start, end string) ([]models.Event, error)
	GetEvent(countryID database.CountryID, id uint64) (*models.Event, error)
	CreateEvent(countryID database.CountryID, req *EventInput) (*models.Event, error)
	UpdateEvent(countryID database.CountryID, id uint64, req *EventInput) (*models.Event, error)
	DeleteEvent(countryID database.CountryID, id uint64) error
	ListPublicEvents(countryID database.CountryID, start, end string, limit int) ([]models.Event, error)
}

type calendarService struct {
	repo repositories.CalendarRepository
}

func NewCalendarService(repo repositories.CalendarRepository) CalendarService {
	return &calendarService{repo: repo}
}

func (s *calendarService) ListEvents(countryID database.CountryID, start, end string) ([]models.Event, error) {
	return s.repo.ListEvents(countryID, start, end)
}

func (s *calendarService) GetEvent(countryID database.CountryID, id uint64) (*models.Event, error) {
	return s.repo.FindEventByID(countryID, id)
}

func (s *calendarService) CreateEvent(countryID database.CountryID, req *EventInput) (*models.Event, error) {
	eventDate, err := time.Parse("2006-01-02", req.EventDate)
	if err != nil {
		return nil, MapError(err)
	}

	event := &models.Event{
		Title:     utils.SanitizeInput(req.Title),
		EventDate: eventDate,
	}

	if req.Description != "" {
		event.Description = &req.Description
	}

	err = s.repo.CreateEvent(countryID, event)
	return event, MapError(err)
}

func (s *calendarService) UpdateEvent(countryID database.CountryID, id uint64, req *EventInput) (*models.Event, error) {
	event, err := s.repo.FindEventByID(countryID, id)
	if err != nil {
		return nil, MapError(err)
	}

	if req.Title != "" {
		event.Title = utils.SanitizeInput(req.Title)
	}

	if req.Description != "" {
		event.Description = &req.Description
	}

	if req.EventDate != "" {
		eventDate, err := time.Parse("2006-01-02", req.EventDate)
		if err == nil {
			event.EventDate = eventDate
		}
	}

	if err := s.repo.UpdateEvent(countryID, event); err != nil {
		return nil, MapError(err)
	}

	return event, nil
}

func (s *calendarService) DeleteEvent(countryID database.CountryID, id uint64) error {
	if _, err := s.repo.FindEventByID(countryID, id); err != nil {
		return MapError(err)
	}

	return s.repo.DeleteEvent(countryID, id)
}

func (s *calendarService) ListPublicEvents(countryID database.CountryID, start, end string, limit int) ([]models.Event, error) {
	return s.repo.ListPublicEvents(countryID, start, end, limit)
}
