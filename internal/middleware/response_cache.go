package middleware

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/gofiber/fiber/v2"
)

type cachedHTTPResponse struct {
	Status      int       `json:"status"`
	ContentType string    `json:"content_type"`
	Body        string    `json:"body"`
	CreatedAt   time.Time `json:"created_at"`
}

// pathTTLRules maps path prefixes to their Redis cache TTL.
// Paths NOT listed here are not cached.
// neverCachedPaths is checked first and always wins.
var pathTTLRules = []struct {
	prefix string
	ttl    time.Duration
}{
	// Static-ish reference data — changes only via admin dashboard
	{"/api/school-classes", 10 * time.Minute},
	{"/api/subjects", 10 * time.Minute},
	{"/api/semesters", 10 * time.Minute},
	{"/api/classes", 10 * time.Minute},

	// Settings loaded on every SSR render — short TTL avoids DB hit on each page
	{"/api/front/settings", 5 * time.Minute},

	// Public content feeds
	{"/api/articles", 2 * time.Minute},
	{"/api/posts", 2 * time.Minute},
	{"/api/home", 2 * time.Minute},
	{"/api/categories", 2 * time.Minute},
	{"/api/keywords", 2 * time.Minute},

	// Comments are the highest-traffic public endpoint.
	// 60 s cache cuts repeated identical requests to near zero while staying fresh.
	{"/api/comments", 60 * time.Second},
}

// neverCachedPaths are always skipped regardless of pathTTLRules.
var neverCachedPaths = []string{
	"/api/dashboard",
	"/api/auth",
	"/api/user",
	"/api/notifications",
	"/api/messages",
}

func cacheTTLForPath(path string) time.Duration {
	for _, prefix := range neverCachedPaths {
		if strings.HasPrefix(path, prefix) {
			return 0
		}
	}
	for _, rule := range pathTTLRules {
		if strings.HasPrefix(path, rule.prefix) {
			return rule.ttl
		}
	}
	return 0
}

// ResponseCache caches successful GET responses in Redis with per-path TTLs.
// The ttl parameter is kept for call-site compatibility but is now unused —
// TTLs are determined per path by pathTTLRules.
func ResponseCache(_ time.Duration) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if c.Method() != fiber.MethodGet {
			return c.Next()
		}
		if strings.Contains(strings.ToLower(c.Get(fiber.HeaderCacheControl)), "no-cache") {
			return c.Next()
		}

		ttl := cacheTTLForPath(c.Path())
		if ttl <= 0 {
			return c.Next()
		}

		rdb := database.Redis()
		ctx := context.Background()
		key := publicCacheKey(c)
		var cached cachedHTTPResponse
		if rdb.GetJSON(ctx, key, &cached) && cached.Body != "" {
			if cached.ContentType != "" {
				c.Set(fiber.HeaderContentType, cached.ContentType)
			}
			c.Set("X-Cache", "HIT")
			return c.Status(cached.Status).SendString(cached.Body)
		}

		if err := c.Next(); err != nil {
			return err
		}

		status := c.Response().StatusCode()
		if status != fiber.StatusOK {
			return nil
		}
		contentType := string(c.Response().Header.ContentType())
		if !strings.Contains(contentType, "application/json") {
			return nil
		}
		body := string(c.Response().Body())
		if len(body) == 0 || len(body) > 1024*512 {
			return nil
		}
		_ = rdb.SetJSON(ctx, key, cachedHTTPResponse{Status: status, ContentType: contentType, Body: body, CreatedAt: time.Now().UTC()}, ttl)
		c.Set("X-Cache", "MISS")
		return nil
	}
}

func publicCacheKey(c *fiber.Ctx) string {
	raw := c.Method() + ":" + c.OriginalURL() + ":" + c.Get("Accept-Language") + ":" + c.Get("X-Country-Code")
	sum := sha1.Sum([]byte(raw))
	return database.Redis().Key("http_cache", hex.EncodeToString(sum[:]))
}
