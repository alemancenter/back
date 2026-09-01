package analytics

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/imanjo/fiber-api/internal/database"
	_ "github.com/imanjo/fiber-api/internal/models"
	"github.com/imanjo/fiber-api/internal/services"
	"github.com/imanjo/fiber-api/internal/utils"
)

// Handler contains analytics route handlers
type Handler struct {
	svc services.AnalyticsService
}

// New creates a new analytics Handler
func New(svc services.AnalyticsService) *Handler {
	return &Handler{svc: svc}
}

// servedCached returns a Redis-cached JSON payload for a read-only analytics endpoint,
// falling back to build() on a miss and caching the result for ttl. Safe here because
// these handlers run *after* auth (the HTTP-layer response cache deliberately skips
// /api/dashboard/*), the payload is per-country admin-shared data, and a few seconds of
// staleness on an analytics view is acceptable — it removes repeated heavy aggregation
// while the dashboard is open or being navigated tab-to-tab.
func servedCached[T any](c *fiber.Ctx, key string, ttl time.Duration, build func() T) error {
	rdb := database.Redis()
	ctx := c.UserContext()
	var cached json.RawMessage
	if rdb.GetJSON(ctx, key, &cached) && len(cached) > 0 {
		return utils.Success(c, "success", cached)
	}
	data := build()
	_ = rdb.SetJSON(ctx, key, data, ttl)
	return utils.Success(c, "success", data)
}

// VisitorAnalytics returns the full analytics payload expected by the frontend.
// @Summary Get Visitor Analytics
// @Description Returns comprehensive visitor analytics (e.g., page views, unique visitors, browser stats)
// @Tags Analytics
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param days query int false "Number of days for analysis (default: 30)"
// @Success 200 {object} utils.APIResponse{data=services.VisitorAnalyticsResponse}
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/visitor-analytics [get]
func (h *Handler) VisitorAnalytics(c *fiber.Ctx) error {
	countryID, _ := c.Locals("country_id").(database.CountryID)

	days := 30
	if d, err := fmt.Sscan(c.Query("days", "30"), &days); d == 0 || err != nil || days <= 0 || days > 365 {
		days = 30
	}

	// Short TTL: the heavy multi-day aggregates are already cached deeper in the service
	// (visitorTrends, 10 min); this only collapses bursts of the live "active now" queries.
	key := database.Redis().Key("analytics", "visitor", database.CountryCode(countryID), strconv.Itoa(days))
	return servedCached(c, key, 15*time.Second, func() *services.VisitorAnalyticsResponse {
		return h.svc.GetVisitorAnalytics(countryID, days)
	})
}

type PruneRequest struct {
	Days int `json:"days"`
}

// PruneAnalytics deletes old visitor data
// @Summary Prune Analytics
// @Description Delete visitor analytics data older than a specified number of days
// @Tags Analytics
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param request body PruneRequest false "Days to retain (default: 90)"
// @Success 200 {object} utils.APIResponse{data=services.PruneAnalyticsResponse}
// @Failure 400 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/visitor-analytics/prune [post]
func (h *Handler) PruneAnalytics(c *fiber.Ctx) error {
	var req PruneRequest
	if err := c.BodyParser(&req); err != nil || req.Days == 0 {
		req.Days = 90
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)
	deleted := h.svc.PruneAnalytics(countryID, req.Days)

	// Drop the cached visitor payloads for this country so the next view reflects the prune.
	rdb := database.Redis()
	_, _ = rdb.DeleteByPattern(c.UserContext(), rdb.Key("analytics", "visitor", database.CountryCode(countryID))+":*")

	return utils.Success(c, "تم حذف البيانات القديمة", services.PruneAnalyticsResponse{
		Deleted: deleted,
	})
}

// DashboardSummary returns the main dashboard data expected by the frontend.
// @Summary Dashboard Summary
// @Description Returns the main dashboard summary including articles, posts, and file counts
// @Tags Analytics
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Success 200 {object} utils.APIResponse{data=services.DashboardSummaryResponse}
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard [get]
func (h *Handler) DashboardSummary(c *fiber.Ctx) error {
	countryID, _ := c.Locals("country_id").(database.CountryID)

	key := database.Redis().Key("analytics", "summary", database.CountryCode(countryID))
	return servedCached(c, key, 2*time.Minute, func() *services.DashboardSummaryResponse {
		return h.svc.GetDashboardSummary(countryID)
	})
}

// ContentAnalytics returns content performance
// @Summary Content Analytics
// @Description Get performance metrics for articles and posts (e.g., views)
// @Tags Analytics
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Success 200 {object} utils.APIResponse{data=services.ContentAnalyticsResponse}
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/content-analytics [get]
func (h *Handler) ContentAnalytics(c *fiber.Ctx) error {
	countryID, _ := c.Locals("country_id").(database.CountryID)

	key := database.Redis().Key("analytics", "content", database.CountryCode(countryID))
	return servedCached(c, key, 2*time.Minute, func() *services.ContentAnalyticsResponse {
		return h.svc.GetContentAnalytics(countryID)
	})
}

// PerformanceSummary returns app performance metrics
// @Summary Performance Summary
// @Description Get comprehensive backend performance metrics (uptime, memory, GC)
// @Tags Analytics
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Success 200 {object} utils.APIResponse{data=services.PerformanceSummaryResponse}
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/performance/summary [get]
func (h *Handler) PerformanceSummary(c *fiber.Ctx) error {
	data := h.svc.GetPerformanceSummary()
	return utils.Success(c, "success", data)
}

// PerformanceLive returns lightweight live metrics expected by the dashboard.
// @Summary Live Performance Metrics
// @Description Fast lightweight endpoint for polling live server load (CPU, Mem)
// @Tags Analytics
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Success 200 {object} utils.APIResponse{data=map[string]interface{}}
// @Router /dashboard/performance/live [get]
func (h *Handler) PerformanceLive(c *fiber.Ctx) error {
	data := h.svc.GetPerformanceLive()
	return utils.Success(c, "success", data)
}

// PerformanceResponseTime returns sampled backend request latency.
// @Summary Backend Response Time
// @Description Returns real response-time statistics from tracked successful public requests.
// @Tags Analytics
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Success 200 {object} utils.APIResponse{data=map[string]interface{}}
// @Router /dashboard/performance/response-time [get]
func (h *Handler) PerformanceResponseTime(c *fiber.Ctx) error {
	countryID, _ := c.Locals("country_id").(database.CountryID)

	data := h.svc.GetPerformanceResponseTime(countryID)
	return utils.Success(c, "success", data)
}

// PerformanceCache returns Redis cache hit ratio and size.
// @Summary Cache Performance
// @Description Returns the Redis cache hit ratio and total keys
// @Tags Analytics
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Success 200 {object} utils.APIResponse{data=map[string]interface{}}
// @Router /dashboard/performance/cache [get]
func (h *Handler) PerformanceCache(c *fiber.Ctx) error {
	data := h.svc.GetPerformanceCache()
	return utils.Success(c, "success", data)
}

// PerformanceRaw returns raw Redis and Go runtime metrics for debugging.
// GET /api/dashboard/performance/raw
func (h *Handler) PerformanceRaw(c *fiber.Ctx) error {
	data := h.svc.GetPerformanceRaw()
	return utils.Success(c, "success", data)
}
