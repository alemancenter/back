package middleware

import (
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

// CORS configures Cross-Origin Resource Sharing
func CORS() fiber.Handler {
	// In production, restrict to allowed origins from env.
	// Comma-separated list, e.g. "https://example.com,https://www.example.com"
	// Falls back to allow-all (for local dev) when not set.
	allowedOrigins := os.Getenv("CORS_ALLOWED_ORIGINS")
	appEnv := strings.ToLower(os.Getenv("APP_ENV"))
	isDev := appEnv == "development" || appEnv == "local"

	var allowOriginsFunc func(string) bool
	if allowedOrigins == "" {
		if isDev {
			// Explicit local/development fallback only — allow all.
			allowOriginsFunc = func(origin string) bool { return true }
		} else {
			// Fail closed: unset/misconfigured origin list must never
			// default to allow-all in production, since AllowCredentials
			// is true (that combination lets any origin read authenticated
			// responses via credentialed fetch).
			allowOriginsFunc = func(origin string) bool { return false }
		}
	} else {
		origins := strings.Split(allowedOrigins, ",")
		originSet := make(map[string]bool, len(origins))
		for _, o := range origins {
			originSet[strings.TrimSpace(o)] = true
		}
		allowOriginsFunc = func(origin string) bool {
			return originSet[origin]
		}
	}

	return cors.New(cors.Config{
		AllowOriginsFunc: allowOriginsFunc,
		AllowOrigins:     "",
		AllowMethods: "GET,POST,PUT,PATCH,DELETE,OPTIONS",
		AllowHeaders: strings.Join([]string{
			"Origin",
			"Content-Type",
			"Accept",
			"Authorization",
			"X-Requested-With",
			"X-Frontend-Key",
			"X-Country-Id",
			"X-Country-Code",
			"X-App-Locale",
			"X-CSRF-Token",
			"Cache-Control",
		}, ","),
		ExposeHeaders: strings.Join([]string{
			"X-Total-Count",
			"X-Page",
			"X-Per-Page",
			"Content-Disposition",
		}, ","),
		AllowCredentials: true,
		MaxAge:           86400,
	})
}
