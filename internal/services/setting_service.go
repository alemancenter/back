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

var publicSettingKeys = map[string]bool{
	"adsense_client":       true,
	"canonical_url":        true,
	"cookieyes_id":         true,
	"date_format":          true,
	"enable_notifications": true,
	"enable_registration":  true,
	"facebook_pixel_id":    true,
	"footer_text":          true,
	"recaptcha_site_key":   true,
	"twitter_handle":       true,
	// Read by DownloadAuthGate via GetPublic — must be public or the admin
	// toggle "طلب تسجيل الدخول قبل التحميل" has no effect.
	"require_login_for_download": true,
}

var privateSettingMarkers = []string{
	"bounce_",
	"client_secret",
	"imap",
	"mail_",
	"password",
	"private",
	"secret",
	"smtp",
	"token",
	"_api_key",
}

func isPublicSettingKey(key string) bool {
	lowerKey := strings.ToLower(key)
	for _, marker := range privateSettingMarkers {
		if strings.Contains(lowerKey, marker) {
			return false
		}
	}
	if publicSettingKeys[key] {
		return true
	}
	for _, prefix := range []string{
		"contact_",
		"google_ads_",
		"site_",
		"social_",
	} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
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
			if row.Value != nil && isPublicSettingKey(row.Key) {
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
