package repositories

import (
	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"gorm.io/gorm"
)

type CalendarRepository interface {
	ListEvents(countryID database.CountryID, start, end string) ([]models.Event, error)
	FindEventByID(countryID database.CountryID, id uint64) (*models.Event, error)
	CreateEvent(countryID database.CountryID, event *models.Event) error
	UpdateEvent(countryID database.CountryID, event *models.Event) error
	DeleteEvent(countryID database.CountryID, id uint64) error

	ListPublicEvents(countryID database.CountryID, start, end string, limit int) ([]models.Event, error)
}

type calendarRepository struct{}

func NewCalendarRepository() CalendarRepository {
	return &calendarRepository{}
}

func (r *calendarRepository) getDB(countryID database.CountryID) *gorm.DB {
	return database.DBForCountry(countryID)
}

func (r *calendarRepository) ListEvents(countryID database.CountryID, start, end string) ([]models.Event, error) {
	var events []models.Event
	query := r.getDB(countryID).Model(&models.Event{})

	if start != "" {
		query = query.Where("event_date >= ?", start)
	}
	if end != "" {
		query = query.Where("event_date <= ?", end)
	}

	err := query.Order("event_date ASC").Find(&events).Error
	return events, err
}

func (r *calendarRepository) FindEventByID(countryID database.CountryID, id uint64) (*models.Event, error) {
	var event models.Event
	err := r.getDB(countryID).First(&event, id).Error
	return &event, err
}

func (r *calendarRepository) CreateEvent(countryID database.CountryID, event *models.Event) error {
	return r.getDB(countryID).Create(event).Error
}

func (r *calendarRepository) UpdateEvent(countryID database.CountryID, event *models.Event) error {
	return r.getDB(countryID).Save(event).Error
}

func (r *calendarRepository) DeleteEvent(countryID database.CountryID, id uint64) error {
	return r.getDB(countryID).Delete(&models.Event{}, id).Error
}

func (r *calendarRepository) ListPublicEvents(countryID database.CountryID, start, end string, limit int) ([]models.Event, error) {
	var events []models.Event
	query := r.getDB(countryID).Model(&models.Event{})
	if start != "" {
		query = query.Where("event_date >= ?", start)
	}
	if end != "" {
		query = query.Where("event_date <= ?", end)
	}
	err := query.Order("event_date ASC").Limit(limit).Find(&events).Error
	return events, err
}
