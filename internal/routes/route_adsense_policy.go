package routes

import (
	"github.com/gofiber/fiber/v2"
	"github.com/imanjo/fiber-api/internal/middleware"
)

// registerAdSensePolicyRoutes handles the deterministic (no AI) AdSense content-policy scan:
// duplicate/near-duplicate, weak, and corrupted content across articles and posts.
func registerAdSensePolicyRoutes(dash fiber.Router, h *Handlers) {
	policy := dash.Group("/adsense-policy", middleware.CanAny("manage articles", "manage posts"))
	policy.Get("/scan", h.AdSensePolicy.GetLastScan)
	policy.Post("/scan", h.AdSensePolicy.ScanDuplicates)
}
