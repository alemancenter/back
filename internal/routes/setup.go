package routes

import (
	"time"

	"github.com/alemancenter/fiber-api/internal/middleware"
	"github.com/gofiber/fiber/v2"
	fiberCompress "github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/etag"
)

// Setup registers all API routes on the given Fiber app and returns the
// initialized handler dependencies so callers can access shared instances
// (e.g. BounceReader for the IMAP scheduler).
func Setup(app *fiber.App) *Handlers {
	// Initialize Dependencies
	deps := NewDependencies()

	// Global middleware
	app.Use(middleware.RequestID())
	app.Use(middleware.SecurityHeaders())
	app.Use(middleware.CORS())
	app.Use(middleware.MethodOverride()) // must run before routing to rewrite _method in FormData
	app.Use(middleware.Metrics())
	app.Use(middleware.RequestLogger())
	app.Use(middleware.AuthRateLimit())
	app.Use(middleware.PrefixRateLimit(
		// Batch job progress is intentionally polled by the dashboard while long AI jobs run.
		// Keep GET polling permissive, while POST job creation remains limited.
		middleware.RateLimitRule{Prefix: "/api/dashboard/content-audit/ai/batch-jobs", Methods: []string{fiber.MethodGet}, Max: 900, Window: 5 * time.Minute},
		middleware.RateLimitRule{Prefix: "/backend-api/dashboard/content-audit/ai/batch-jobs", Methods: []string{fiber.MethodGet}, Max: 900, Window: 5 * time.Minute},
		middleware.RateLimitRule{Prefix: "/api/dashboard/content-audit/ai/batch-jobs/", Methods: []string{fiber.MethodPost}, Max: 120, Window: 5 * time.Minute},
		middleware.RateLimitRule{Prefix: "/backend-api/dashboard/content-audit/ai/batch-jobs/", Methods: []string{fiber.MethodPost}, Max: 120, Window: 5 * time.Minute},
		middleware.RateLimitRule{Prefix: "/api/dashboard/content-audit/ai/batch-jobs", Methods: []string{fiber.MethodPost}, Max: 20, Window: 5 * time.Minute},
		middleware.RateLimitRule{Prefix: "/backend-api/dashboard/content-audit/ai/batch-jobs", Methods: []string{fiber.MethodPost}, Max: 20, Window: 5 * time.Minute},
		middleware.RateLimitRule{Prefix: "/api/dashboard/content-audit/ai/", Max: 60, Window: 5 * time.Minute},
		middleware.RateLimitRule{Prefix: "/backend-api/dashboard/content-audit/ai/", Max: 60, Window: 5 * time.Minute},
		middleware.RateLimitRule{Prefix: "/api/ai/status/", Max: 300, Window: 5 * time.Minute},
		middleware.RateLimitRule{Prefix: "/backend-api/ai/status/", Max: 300, Window: 5 * time.Minute},
		middleware.RateLimitRule{Prefix: "/api/ai/", Max: 60, Window: 5 * time.Minute},
		middleware.RateLimitRule{Prefix: "/backend-api/ai/", Max: 60, Window: 5 * time.Minute},
		middleware.RateLimitRule{Prefix: "/api/chatbot/message", Methods: []string{fiber.MethodPost}, Max: 30, Window: 10 * time.Minute},
		middleware.RateLimitRule{Prefix: "/backend-api/chatbot/message", Methods: []string{fiber.MethodPost}, Max: 30, Window: 10 * time.Minute},
		middleware.RateLimitRule{Prefix: "/api/chatbot/feedback", Methods: []string{fiber.MethodPost}, Max: 60, Window: 10 * time.Minute},
		middleware.RateLimitRule{Prefix: "/backend-api/chatbot/feedback", Methods: []string{fiber.MethodPost}, Max: 60, Window: 10 * time.Minute},
		middleware.RateLimitRule{Prefix: "/api/dashboard/files", Max: 60, Window: time.Minute},
		middleware.RateLimitRule{Prefix: "/backend-api/dashboard/files", Max: 60, Window: time.Minute},
	))
	app.Use(fiberCompress.New(fiberCompress.Config{
		Level: fiberCompress.LevelBestSpeed,
	}))
	app.Use(etag.New())
	app.Use(middleware.ResponseCache(2 * time.Minute))

	// Health check (no auth required)
	app.Get("/api/ping", deps.Health.Ping)
	app.Get("/api/health", deps.Health.Health)
	app.Get("/metrics", middleware.PrometheusMetrics)

	// Base API group with frontend guard and IP guard
	api := app.Group("/api",
		middleware.IPGuard(),
		middleware.FrontendGuard(),
	)

	// Public Group
	public := api.Group("", middleware.OptionalAuth(), middleware.TrackVisitor())

	// Dashboard Group
	dash := api.Group("/dashboard",
		middleware.Auth(),
		middleware.RequireVerifiedEmail(),
		middleware.UpdateLastActivity(),
		middleware.DashboardSecurityHeaders(),
	)

	// Register Domain Modules
	registerAuthRoutes(api, dash, deps)
	registerContentRoutes(api, public, dash, deps)
	registerAcademicRoutes(public, dash, deps)
	registerCommunicationRoutes(public, dash, deps)
	registerSystemRoutes(api, public, dash, deps)
	registerAnalyticsRoutes(public, dash, deps)
	registerTeacherSubscriptionRoutes(api, dash, deps)

	return deps
}
