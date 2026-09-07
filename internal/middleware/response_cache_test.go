package middleware

import (
	"github.com/gofiber/fiber/v2"
	"net/http/httptest"
	"testing"
)

func TestResponseCacheNeverReplaysDownloadAuthorization(t *testing.T) {
	app := fiber.New()
	app.Use(ResponseCache(0))
	calls := 0
	app.Get("/api/articles/file/1/download-url", func(c *fiber.Ctx) error {
		calls++
		if c.Get("X-Test-Identity") == "" {
			return c.SendStatus(401)
		}
		return c.JSON(fiber.Map{"token": "signed"})
	})
	first := httptest.NewRequest("GET", "/api/articles/file/1/download-url", nil)
	first.Header.Set("X-Test-Identity", "allowed")
	res, err := app.Test(first)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	second := httptest.NewRequest("GET", "/api/articles/file/1/download-url", nil)
	res, err = app.Test(second)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 401 || calls != 2 {
		t.Fatal("authorization was replayed")
	}
	if res.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatal("missing private response policy")
	}
}
