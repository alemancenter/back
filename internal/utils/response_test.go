package utils

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestResponse(t *testing.T) {
	app := fiber.New()

	app.Get("/success", func(c *fiber.Ctx) error {
		return Success(c, "ok", map[string]string{"foo": "bar"})
	})

	app.Get("/bad_request", func(c *fiber.Ctx) error {
		return BadRequest(c, "invalid input")
	})

	t.Run("Success Response", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/success", nil)

		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("Failed to execute request: %v", err)
		}

		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&body)

		if body["success"] != true {
			t.Errorf("Expected success to be true, got %v", body["success"])
		}
		if body["message"] != "ok" {
			t.Errorf("Expected message to be ok, got %v", body["message"])
		}
	})

	t.Run("BadRequest Response", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/bad_request", nil)

		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("Failed to execute request: %v", err)
		}

		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&body)

		if body["success"] != false {
			t.Errorf("Expected success to be false, got %v", body["success"])
		}
	})
}
