package services

import (
	"context"
	"strings"
	"time"

	"github.com/imanjo/fiber-api/internal/config"
	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/repositories"
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
	"adsense_client":               true,
	"canonical_url":                true,
	"date_format":                  true,
	"time_format":                  true,
	"enable_notifications":         true,
	"enable_registration":          true,
	"enable_teacher_subscriptions": true,
	"facebook_pixel_id":            true,
	// Read by the frontend's own middleware to actually gate public routes — same class
	// of bug as require_login_for_download above: without this, the admin toggle "وضع
	// الصيانة" silently does nothing, because the frontend can never see its value.
	"maintenance_mode": true,
	// Public OAuth identifiers — safe to expose (used in the browser to start the
	// login flow). The matching *_secret keys are blocked by privateSettingMarkers.
	"google_client_id":   true,
	"facebook_app_id":    true,
	"footer_text":        true,
	"recaptcha_site_key": true,
	"twitter_handle":     true,
	// Read by DownloadAuthGate via GetPublic — must be public or the admin
	// toggle "طلب تسجيل الدخول قبل التحميل" has no effect.
	"require_login_for_download": true,
	// Public page-rendering data: SEO defaults, brand colors, analytics IDs — all
	// meant to be embedded directly in page HTML, none of them secrets.
	"meta_title":               true,
	"meta_description":         true,
	"meta_keywords":            true,
	"primary_color":            true,
	"secondary_color":          true,
	"google_analytics_id":      true,
	"robots_txt":               true,
	"sitemap_url":              true,
	"seo_title_template":       true,
	"indexnow_key":             true,
	"llms_txt_enabled":         true,
	"rss_enabled":              true,
	"rss_items":                true,
	"rss_before_content":       true,
	"rss_after_content":        true,
	"llms_full_txt_enabled":    true,
	"google_site_verification": true,
	"bing_site_verification":   true,
	"yandex_verification":      true,
	"pinterest_verification":   true,
	"baidu_site_verification":  true,
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

	// Some public HTTP responses, especially /api/home, embed settings.
	// Settings updates are rare, so invalidate cached public responses here
	// to make dashboard changes visible on the next request.
	_, _ = database.Redis().DeleteByPattern(
		ctx,
		database.Redis().Key("http_cache", "*"),
	)

	if userID != 0 {
		LogActivity("حدّث الإعدادات", "Setting", 0, userID)
	}

	return nil
}
