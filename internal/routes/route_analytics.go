package routes

import (
	"github.com/alemancenter/fiber-api/internal/middleware"
	"github.com/gofiber/fiber/v2"
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
	dash.Get("/content-analytics", h.Analytics.ContentAnalytics)
	dash.Get("", h.Analytics.DashboardSummary)

	// Activities Log
	dash.Get("/activities", h.Dashboard.Activities)
	dash.Delete("/activities/clean", h.Dashboard.CleanActivities)

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
