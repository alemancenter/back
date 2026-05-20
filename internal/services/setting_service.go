package services

import (
	"context"
	"strings"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/repositories"
)

const (
	settingsCacheTTL = 2 * time.Hour
)

type SettingService interface {
	GetAll(ctx context.Context, countryID database.CountryID) (map[string]string, error)
	GetPublic(ctx context.Context, countryID database.CountryID) (map[string]string, error)
	Update(ctx context.Context, countryID database.CountryID, updates map[string]string, userID uint) error
}

type settingService struct {
	repo repositories.SettingRepository
}

func NewSettingService(repo repositories.SettingRepository) SettingService {
	return &settingService{repo: repo}
}

func (s *settingService) GetAll(ctx context.Context, countryID database.CountryID) (map[string]string, error) {
	rows, err := s.repo.GetAll(ctx, countryID)
	if err != nil {
		return nil, MapError(err)
	}

	m := make(map[string]string, len(rows))
	for _, row := range rows {
		val := ""
		if row.Value != nil {
			val = strings.ReplaceAll(*row.Value, "\\", "/")
		}
		m[row.Key] = val
	}
	return m, nil
}

func (s *settingService) GetPublic(ctx context.Context, countryID database.CountryID) (map[string]string, error) {
	countryCode := database.CountryCode(countryID)
	key := database.Redis().Key("settings", countryCode)

	result, err := GetOrSet(ctx, key, settingsCacheTTL, func() (map[string]string, error) {
		rows, err := s.repo.GetAll(ctx, countryID)
		if err != nil {
			return nil, MapError(err)
		}

		m := make(map[string]string, len(rows))
		for _, row := range rows {
			if row.Value != nil {
				m[row.Key] = strings.ReplaceAll(*row.Value, "\\", "/")
			}
		}
		return m, nil
	})

	return result, MapError(err)
}

func (s *settingService) Update(ctx context.Context, countryID database.CountryID, updates map[string]string, userID uint) error {
	for key, value := range updates {
		if err := s.repo.Upsert(ctx, countryID, key, value); err != nil {
			return MapError(err)
		}
	}

	countryCode := database.CountryCode(countryID)
	InvalidateCache(database.Redis().Key("settings", countryCode))

	if userID != 0 {
		LogActivity("حدّث الإعدادات", "Setting", 0, userID)
	}

	return nil
}
