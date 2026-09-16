package routes

import (
	"github.com/gofiber/fiber/v2"
	"github.com/imanjo/fiber-api/internal/middleware"
)

// registerContentGenRoutes handles the AI writing-assist endpoint for article/post drafts.
func registerContentGenRoutes(dash fiber.Router, h *Handlers) {
	gen := dash.Group("/content-gen", middleware.CanAny("manage articles", "manage posts"))
	gen.Post("/draft", h.ContentGen.GenerateDraft)
}
