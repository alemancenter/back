package health

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/gofiber/fiber/v2"
)

type mockHealthService struct{}

func (m *mockHealthService) GetHealthStatus() (services.HealthStatusResponse, int, bool) {
	return services.HealthStatusResponse{Status: "ok"}, fiber.StatusOK, true
}

func TestHealthHandler_Ping(t *testing.T) {
	app := fiber.New()
	handler := New(&mockHealthService{})

	app.Get("/api/ping", handler.Ping)

	req := httptest.NewRequest("GET", "/api/ping", nil)
	resp, err := app.Test(req)

	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var body services.PingResponse
	json.NewDecoder(resp.Body).Decode(&body)

	if body.Status != "ok" {
		t.Errorf("Expected status 'ok', got '%s'", body.Status)
	}
	if body.Message != "pong" {
		t.Errorf("Expected message 'pong', got '%s'", body.Message)
	}
}

func TestHealthHandler_Health(t *testing.T) {
	app := fiber.New()
	handler := New(&mockHealthService{})

	app.Get("/api/health", handler.Health)

	req := httptest.NewRequest("GET", "/api/health", nil)
	resp, err := app.Test(req)

	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)

	if body["status"] != "ok" {
		t.Errorf("Expected status 'ok', got '%v'", body["status"])
	}
}
