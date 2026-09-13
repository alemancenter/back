package routes

import (
	"github.com/gofiber/fiber/v2"
	"github.com/imanjo/fiber-api/internal/middleware"
)

// registerAdSensePolicyRoutes handles the deterministic (no AI) AdSense content-policy scan —
// currently just duplicate/near-duplicate detection across articles and posts.
func registerAdSensePolicyRoutes(dash fiber.Router, h *Handlers) {
	policy := dash.Group("/adsense-policy", middleware.CanAny("manage articles", "manage posts"))
	policy.Post("/scan", h.AdSensePolicy.ScanDuplicates)
}
