package middleware

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

const RequestIDHeader = "X-Request-ID"

// RequestID injects a unique request identifier into every request and response.
// If the client already sends an X-Request-ID header its value is kept (useful for
// frontend-to-backend tracing); otherwise a new UUID v4 is generated.
// The ID is stored in c.Locals("request_id") so handlers and the logger can read it.
func RequestID() fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.Get(RequestIDHeader)
		if id == "" {
			id = uuid.New().String()
		}
		c.Locals("request_id", id)
		c.Set(RequestIDHeader, id)
		return c.Next()
	}
}
