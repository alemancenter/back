package middleware

import (
	"github.com/gofiber/fiber/v2"
	"strings"
	"time"
)

// ResponseCache deliberately does not store HTTP envelopes. Service-layer caches
// own invalidation; authentication, publication and download decisions always run.
// This also ignores cache entries written by older releases during rolling deploys.
func ResponseCache(_ time.Duration) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if strings.HasPrefix(c.Path(), "/api/") {
			c.Set("Cache-Control", "private, no-store")
		}
		return c.Next()
	}
}
