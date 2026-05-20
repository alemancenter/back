package middleware

import (
	"context"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/alemancenter/fiber-api/pkg/logger"
	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

// noTrackPrefixes lists high-traffic public endpoints excluded from visitor tracking.
var noTrackPrefixes = []string{
	"/api/front/settings",
	"/api/home",
	"/api/articles",
	"/api/posts",
	"/api/categories",
	"/api/school-classes",
	"/api/filter",
}

func isNoTrack(path string) bool {
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

		ev := services.VisitorEvent{
			IPAddress:    utils.GetClientIP(c),
			UserAgent:    c.Get("User-Agent"),
			URL:          c.Path(),
			Referer:      c.Get("Referer"),
			CountryCode:  countryCode,
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
