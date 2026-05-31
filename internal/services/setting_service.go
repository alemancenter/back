package services

import (
	"context"
	"strings"
	"time"

	"github.com/alemancenter/fiber-api/internal/config"
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
	if err != nil {
		return nil, MapError(err)
	}

	// ── AdSense unification (AdSense policy: only ONE ca-pub-* per domain) ──
	// If ADSENSE_CLIENT env var is set, it overrides whatever the per-country DB
	// returns. This guarantees every page — regardless of the country route —
	// carries the same publisher ID, preventing a dual-account policy violation.
	if envAdsense := strings.TrimSpace(config.Get().Frontend.AdsenseClient); envAdsense != "" {
		result["adsense_client"] = envAdsense
	}

	// ── canonical_url / site_url fallback ──
	
	frontendURL := strings.TrimSpace(config.Get().Frontend.URL)
	if frontendURL != "" {
		if strings.TrimSpace(result["canonical_url"]) == "" {
			result["canonical_url"] = frontendURL
		}
		if strings.TrimSpace(result["site_url"]) == "" {
			result["site_url"] = frontendURL
		}
	}

	return result, nil
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
