package routes

import (
	"github.com/gofiber/fiber/v2"
	"github.com/imanjo/fiber-api/internal/middleware"
)

// registerHumanReviewRoutes handles the AI policy-fix approval queue: queue a fix for review,
// list pending reviews, approve or reject one. Same permission gate as the adsense-policy scan
// and content-gen endpoints that feed it.
func registerHumanReviewRoutes(dash fiber.Router, h *Handlers) {
	review := dash.Group("/human-review", middleware.CanAny("manage articles", "manage posts"))
	review.Get("/", h.HumanReview.List)
	review.Post("/fix", h.HumanReview.QueueFix)
	review.Post("/:id/approve", h.HumanReview.Approve)
	review.Post("/:id/reject", h.HumanReview.Reject)
}
