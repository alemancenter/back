package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alemancenter/fiber-api/internal/config"
	"github.com/gofiber/fiber/v2"
)

func testFrontendGuardConfig() *config.Config {
	return &config.Config{
		App: config.AppConfig{
			Env: "production",
			URL: "https://api.alemancenter.com",
		},
		Frontend: config.FrontendConfig{
			APIKey:          "frontend-secret",
			CORSOrigins:     []string{"https://alemancenter.com", "https://www.alemancenter.com"},
			RateLimit:       false,
			SSRTrustedIPs:   []string{"127.0.0.1"},
			SSRRateLimitMax: 2000,
		},
	}
}

func newFrontendGuardTestApp() *fiber.App {
	app := fiber.New(fiber.Config{ProxyHeader: "X-Test-IP"})
	app.Get("/api/articles", frontendGuard(testFrontendGuardConfig()), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/api/auth/google/redirect", frontendGuard(testFrontendGuardConfig()), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})
	return app
}

func TestFrontendGuardBlocksDirectPublicAPIHostEvenWithAllowedOrigin(t *testing.T) {
	app := newFrontendGuardTestApp()

	req := httptest.NewRequest(http.MethodGet, "/api/articles", nil)
	req.Host = "api.alemancenter.com"
	req.Header.Set("Origin", "https://alemancenter.com")
	req.Header.Set("X-Forwarded-For", "198.51.100.10")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("expected direct API host status %d, got %d", fiber.StatusNotFound, resp.StatusCode)
	}
}

func TestFrontendGuardBlocksDirectPublicAPIHostEvenWithBearerHeader(t *testing.T) {
	app := newFrontendGuardTestApp()

	req := httptest.NewRequest(http.MethodGet, "/api/articles", nil)
	req.Host = "api.alemancenter.com"
	req.Header.Set("Authorization", "Bearer not-a-validated-token")
	req.Header.Set("X-Forwarded-For", "198.51.100.10")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("expected direct API host status %d, got %d", fiber.StatusNotFound, resp.StatusCode)
	}
}

func TestFrontendGuardAllowsFrontendProxyKey(t *testing.T) {
	app := newFrontendGuardTestApp()

	req := httptest.NewRequest(http.MethodGet, "/api/articles", nil)
	req.Host = "api.alemancenter.com"
	req.Header.Set("X-Frontend-Key", "frontend-secret")
	req.Header.Set("X-Forwarded-For", "198.51.100.10")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("expected frontend proxy status %d, got %d", fiber.StatusNoContent, resp.StatusCode)
	}
}

func TestFrontendGuardKeepsOAuthRedirectPublic(t *testing.T) {
	app := newFrontendGuardTestApp()

	req := httptest.NewRequest(http.MethodGet, "/api/auth/google/redirect", nil)
	req.Host = "api.alemancenter.com"
	req.Header.Set("X-Forwarded-For", "198.51.100.10")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("expected OAuth redirect status %d, got %d", fiber.StatusNoContent, resp.StatusCode)
	}
}

func TestFrontendGuardAllowsDirectLocalRequest(t *testing.T) {
	app := newFrontendGuardTestApp()

	req := httptest.NewRequest(http.MethodGet, "/api/articles", nil)
	req.Host = "127.0.0.1:8082"
	req.Header.Set("X-Test-IP", "127.0.0.1")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("expected direct local status %d, got %d", fiber.StatusNoContent, resp.StatusCode)
	}
}
