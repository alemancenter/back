package health

import (
	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/gofiber/fiber/v2"
)

// Handler contains health check route handlers
type Handler struct {
	svc services.HealthService
}

// New creates a new health Handler
func New(svc services.HealthService) *Handler {
	return &Handler{
		svc: svc,
	}
}

// Ping returns a simple health check response
// @Summary Simple health check
// @Description Returns a simple ping/pong response to verify the API is running
// @Tags Health
// @Produce json
// @Success 200 {object} services.PingResponse
// @Router /ping [get]
func (h *Handler) Ping(c *fiber.Ctx) error {
	return c.JSON(services.PingResponse{
		Status:  "ok",
		Message: "pong",
	})
}

// Health returns detailed health check information
// @Summary Detailed health check
// @Description Returns health status of the API, Databases, and Redis
// @Tags Health
// @Produce json
// @Success 200 {object} services.HealthStatusResponse
// @Failure 503 {object} services.HealthStatusResponse
// @Router /health [get]
func (h *Handler) Health(c *fiber.Ctx) error {
	data, httpStatus, _ := h.svc.GetHealthStatus()

	return c.Status(httpStatus).JSON(data)
}
