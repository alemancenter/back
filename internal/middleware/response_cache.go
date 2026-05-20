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

func ResponseCache(ttl time.Duration) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if ttl <= 0 || c.Method() != fiber.MethodGet || !isPublicCacheablePath(c.Path()) {
			return c.Next()
		}
		if strings.Contains(strings.ToLower(c.Get(fiber.HeaderCacheControl)), "no-cache") {
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

func isPublicCacheablePath(path string) bool {
	blocked := []string{"/api/dashboard", "/api/auth", "/api/user", "/api/notifications", "/api/messages", "/api/comments"}
	for _, prefix := range blocked {
		if strings.HasPrefix(path, prefix) {
			return false
		}
	}
	allowed := []string{"/api/front/settings", "/api/home", "/api/classes", "/api/school-classes", "/api/subjects", "/api/semesters", "/api/articles", "/api/posts", "/api/categories", "/api/keywords"}
	for _, prefix := range allowed {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func publicCacheKey(c *fiber.Ctx) string {
	raw := c.Method() + ":" + c.OriginalURL() + ":" + c.Get("Accept-Language") + ":" + c.Get("X-Country-Code")
	sum := sha1.Sum([]byte(raw))
	return database.Redis().Key("http_cache", hex.EncodeToString(sum[:]))
}
