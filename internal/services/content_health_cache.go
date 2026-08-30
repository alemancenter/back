package services

import (
	"context"

	"github.com/imanjo/fiber-api/internal/database"
)

// InvalidateContentHealthCache drops the cached /dashboard/content-quality scan
// for one country so an article/post create/update/delete is reflected
// immediately instead of after the 2-minute TTL. Key mirrors the one built in
// handlers/contentaudit/content_health.go.
func InvalidateContentHealthCache(countryID database.CountryID) {
	rdb := database.Redis()
	if rdb == nil {
		return
	}
	_ = rdb.Del(context.Background(), rdb.Key("content_health", "v1", database.CountryCode(countryID)))
}
