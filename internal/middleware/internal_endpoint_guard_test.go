package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestInternalEndpointGuardAllowsDirectLocalRequest(t *testing.T) {
	app := fiber.New(fiber.Config{ProxyHeader: "X-Test-IP"})
	app.Get("/api/health", internalEndpointGuard("", ""), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Host = "127.0.0.1:8082"
	req.RemoteAddr = "127.0.0.1:49152"
	req.Header.Set("X-Test-IP", "127.0.0.1")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("expected local request status %d, got %d", fiber.StatusNoContent, resp.StatusCode)
	}
}

func TestInternalEndpointGuardBlocksForwardedPublicRequest(t *testing.T) {
	app := fiber.New()
	app.Get("/api/health", internalEndpointGuard("monitor-secret", ""), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Host = "api.alemancenter.com"
	req.RemoteAddr = "127.0.0.1:49152"
	req.Header.Set("X-Forwarded-For", "198.51.100.10")
	req.Header.Set("X-Real-IP", "198.51.100.10")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("expected forwarded request status %d, got %d", fiber.StatusNotFound, resp.StatusCode)
	}
}

func TestInternalEndpointGuardAllowsInternalMonitorKey(t *testing.T) {
	app := fiber.New()
	app.Get("/api/health", internalEndpointGuard("monitor-secret", ""), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Host = "api.alemancenter.com"
	req.RemoteAddr = "127.0.0.1:49152"
	req.Header.Set("X-Forwarded-For", "198.51.100.10")
	req.Header.Set("X-Internal-Monitor-Key", "monitor-secret")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("expected monitor request status %d, got %d", fiber.StatusNoContent, resp.StatusCode)
	}
}
