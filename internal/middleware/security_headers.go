package middleware

import (
	"github.com/alemancenter/fiber-api/internal/config"
	"github.com/gofiber/fiber/v2"
)

// SecurityHeaders adds security-related HTTP response headers
func SecurityHeaders() fiber.Handler {
	cfg := config.Get()
	return func(c *fiber.Ctx) error {
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("X-Frame-Options", "DENY")
		c.Set("X-XSS-Protection", "1; mode=block")
		c.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		c.Set("X-DNS-Prefetch-Control", "off")

		if cfg.Security.AddHSTS {
			c.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
		}

		if cfg.App.IsProduction() {
			// unsafe-eval removed: eval() and similar dynamic code execution blocked.
			// unsafe-inline kept for script/style because the Next.js frontend injects
			// inline styles; replace with nonce-based CSP when the frontend supports it.
			c.Set("Content-Security-Policy",
				"default-src 'self'; "+
					"script-src 'self' 'unsafe-inline' https:; "+
					"style-src 'self' 'unsafe-inline' https:; "+
					"img-src 'self' data: https:; "+
					"font-src 'self' https: data:; "+
					"connect-src 'self' https:; "+
					"frame-ancestors 'none'")
		}

		return c.Next()
	}
}

// DashboardSecurityHeaders adds stricter headers for dashboard routes
func DashboardSecurityHeaders() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set("X-Robots-Tag", "noindex, nofollow")
		c.Set("Cache-Control", "no-store, no-cache, must-revalidate")
		c.Set("Pragma", "no-cache")
		return c.Next()
	}
}
