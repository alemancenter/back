package middleware

import (
	"context"
	"math/rand"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/models"
	"github.com/imanjo/fiber-api/internal/services"
	"github.com/imanjo/fiber-api/internal/utils"
	"github.com/imanjo/fiber-api/pkg/logger"
	"go.uber.org/zap"
)

// noTrackPrefixes lists high-traffic/internal endpoints excluded from visitor tracking.
// Dashboard/API polling and AI batch endpoints are intentionally excluded to keep
// visitor analytics focused on real public traffic and to reduce DB write pressure.
//
// /api/home, /api/articles, /api/posts, /api/categories, and /api/school-classes used to be
// listed here too — but those ARE the routes the public Astro frontend calls to render the
// homepage, article/post pages, category pages, and class pages. Excluding them meant almost
// every genuine page view was silently dropped before ever reaching visitors_tracking, leaving
// the dashboard's "current visitors" / audience analytics effectively empty regardless of real
// traffic. Only endpoints that are never themselves a distinct page view stay excluded:
// settings/filter lookups a page fires alongside its real request, and the dashboard/auth/
// notifications polling the admin UI itself generates.
var noTrackPrefixes = []string{
	"/api/front/settings",
	"/api/filter",
	"/api/dashboard",
	"/api/auth",
	"/api/download",
	"/api/notifications",
	"/backend-api/dashboard",
	"/backend-api/auth",
	"/backend-api/notifications",
	"/backend-api/content-audit",
}

func isNoTrack(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return true
	}

	// Do not track static/runtime assets or internal health checks as visitors.
	if strings.HasPrefix(path, "/_next/") ||
		strings.HasPrefix(path, "/assets/") ||
		strings.HasPrefix(path, "/fonts/") ||
		strings.HasPrefix(path, "/favicon") ||
		strings.HasPrefix(path, "/health") ||
		strings.HasPrefix(path, "/ping") {
		return true
	}

	for _, prefix := range noTrackPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

// activityCache is an in-process write-dedup map: userID → last DB write time.
// Stored BEFORE the goroutine fires so concurrent requests on the same user see
// the updated timestamp immediately and skip the redundant UPDATE.
var activityCache sync.Map

const activityDebounce = time.Minute

// UpdateLastActivity updates the authenticated user's last activity timestamp.
// At most one DB write per user per activityDebounce window, regardless of
// how many concurrent requests arrive.
func UpdateLastActivity() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if err := c.Next(); err != nil {
			return err
		}

		user, ok := c.Locals("user").(*models.User)
		if !ok || user == nil {
			return nil
		}

		now := time.Now()

		// LoadOrStore with a *sync.Mutex per user to make the check-and-set atomic
		type entry struct {
			mu   sync.Mutex
			last time.Time
		}
		v, _ := activityCache.LoadOrStore(user.ID, &entry{})
		e := v.(*entry)

		e.mu.Lock()
		skip := now.Sub(e.last) < activityDebounce
		if !skip {
			e.last = now // mark before unlock so other goroutines see it
		}
		e.mu.Unlock()

		if skip {
			return nil
		}

		// Capture values before goroutine — c is reused after handler returns
		countryID, _ := c.Locals("country_id").(database.CountryID)
		userID := user.ID

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			db := database.DBForCountry(countryID).WithContext(ctx)
			if err := db.Exec(
				"UPDATE users SET last_activity = ?, updated_at = ? WHERE id = ?",
				now, now, userID,
			).Error; err != nil {
				logger.Error("activity update failed",
					zap.Uint("user_id", userID),
					zap.Error(err),
				)
			}
		}()

		return nil
	}
}

const maxTrackedPageLength = 2048

// normalizeFrontendPage accepts only a local public path. X-Page is analytics metadata,
// never a redirect target, but validating it prevents arbitrary/header-controlled values
// from being persisted as the visitor's current page.
func normalizeFrontendPage(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxTrackedPageLength {
		return ""
	}
	if !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return ""
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return ""
		}
	}

	// Analytics wants the page, not query strings/fragments. This also avoids storing
	// search terms or other URL parameters in the realtime visitor table.
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	if raw == "" {
		return "/"
	}

	// Astro BFF routes are implementation details, not pages visible to the visitor.
	if raw == "/api" || strings.HasPrefix(raw, "/api/") {
		return ""
	}

	return raw
}

// frontendPageFromReferer is a fallback for browser requests that hit an Astro /api/* BFF.
// In that case X-Page is the BFF route itself, while Referer identifies the actual ImanJo page.
func frontendPageFromReferer(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}

	host := strings.ToLower(u.Hostname())
	if host != "imanjo.com" && host != "www.imanjo.com" {
		return ""
	}

	return normalizeFrontendPage(u.Path)
}

// resolveVisitorPage prefers the frontend route supplied by trusted server-side Astro calls.
// Direct API clients/bots without frontend context retain the real API path, which keeps
// bot/direct-API activity distinguishable in analytics.
func resolveVisitorPage(c *fiber.Ctx) string {
	if page := normalizeFrontendPage(c.Get("X-Page")); page != "" {
		return page
	}
	if page := frontendPageFromReferer(c.Get("Referer")); page != "" {
		return page
	}
	return c.Path()
}

// TrackVisitor captures visitor data and enqueues it for async batch insertion.
// The hot path is a single channel send — no goroutine, no Redis round-trip, no JSON marshal.
func TrackVisitor() fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()

		if err := c.Next(); err != nil {
			return err
		}

		statusCode := c.Response().StatusCode()

		// Only track successful GET requests
		if c.Method() != "GET" || statusCode >= 400 {
			return nil
		}

		// Skip high-traffic public endpoints — home, listings, static data
		if isNoTrack(c.Path()) {
			return nil
		}

		// Sample 1 in 3 requests to further reduce queue volume
		if rand.Intn(3) != 0 {
			return nil
		}

		countryCode, _ := c.Locals("country_code").(string)
		if countryCode == "" {
			countryCode = "jo"
		}

		// Analytics must never persist arbitrary proxy-header content as an IP.
		// GetClientIP returns a validated canonical address; local/invalid requests
		// are not meaningful public visitors and are skipped.
		clientIP := utils.GetClientIP(c)
		if clientIP == "" || utils.IsLocalhost(clientIP) {
			return nil
		}

		// Fiber reuses request-context buffers after the handler returns.
		// VisitorEvent is consumed asynchronously by visitorCh, so every string
		// derived from fiber.Ctx must own its backing memory before enqueue.
		ev := services.VisitorEvent{
			IPAddress:    strings.Clone(clientIP),
			UserAgent:    strings.Clone(c.Get("User-Agent")),
			URL:          strings.Clone(resolveVisitorPage(c)),
			Referer:      strings.Clone(c.Get("Referer")),
			CountryCode:  strings.Clone(countryCode),
			StatusCode:   statusCode,
			ResponseTime: float64(time.Since(start).Microseconds()) / 1000.0,
			Timestamp:    time.Now(),
		}
		if u, ok := c.Locals("user").(*models.User); ok && u != nil {
			uid := u.ID
			ev.UserID = &uid
		}

		services.EnqueueVisitor(ev)
		return nil
	}
}
