package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestCORS(t *testing.T) {
	app := fiber.New()
	app.Use(CORS())
	app.Get("/test", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	t.Run("CORS headers are set", func(t *testing.T) {
		req := httptest.NewRequest("OPTIONS", "/test", nil)
		req.Header.Set("Origin", "http://localhost:3000")
		req.Header.Set("Access-Control-Request-Method", "GET")

		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("Failed to execute request: %v", err)
		}

		// Since AllowOrigins is "*", Allow-Origin header will be set appropriately
		origin := resp.Header.Get("Access-Control-Allow-Origin")
		if origin == "" {
			t.Errorf("Expected CORS headers to be set")
		}
	})
}
