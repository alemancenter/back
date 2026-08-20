package routes

import (
	"github.com/gofiber/fiber/v2"
	"github.com/imanjo/fiber-api/internal/middleware"
)

// registerAnalyticsRoutes handles dashboard overviews, monitoring,
// user activities, and general analytics reporting.
func registerAnalyticsRoutes(public, dash fiber.Router, h *Handlers) {
	// =====================
	// PUBLIC ROUTES
	// =====================

	// Basic public home
	public.Get("/home", h.Home.GetHome)

	// =====================
	// ADMIN DASHBOARD ROUTES
	// =====================

	// Dashboard Home Summaries
	// Dashboard entry follows the same permission used by the frontend.
	dash.Get("", middleware.Can("access dashboard"), h.Analytics.DashboardSummary)

	// Content analytics can be viewed by dashboard users or accounts carrying
	// one of the dedicated analytics permissions.
	dash.Get(
		"/content-analytics",
		middleware.CanAny("access dashboard", "view analytics", "manage analytics"),
		h.Analytics.ContentAnalytics,
	)

	// Activities Log
	dashActivities := dash.Group("", middleware.CanAny("manage monitoring", "manage performance", "manage security"))
	dashActivities.Get("/activities", h.Dashboard.Activities)
	dashActivities.Delete("/activities/clean", h.Dashboard.CleanActivities)

	// Visitor Analytics (requires monitoring permission)
	dashMonitor := dash.Group("", middleware.CanAny("manage monitoring", "manage performance"))
	dashMonitor.Get("/visitor-analytics", h.Analytics.VisitorAnalytics)
	dashMonitor.Post("/visitor-analytics/prune", h.Analytics.PruneAnalytics)
	dashMonitor.Get("/performance/summary", h.Analytics.PerformanceSummary)
	dashMonitor.Get("/performance/live", h.Analytics.PerformanceLive)
	dashMonitor.Get("/performance/raw", h.Analytics.PerformanceRaw)
	dashMonitor.Get("/performance/response-time", h.Analytics.PerformanceResponseTime)
	dashMonitor.Get("/performance/cache", h.Analytics.PerformanceCache)
	dashMonitor.Get("/performance/metrics", middleware.MetricsSnapshot)
}
