package seo

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/imanjo/fiber-api/internal/models"
)

func TestOptimizeAndSaveCannotGenerateOrPersist(t *testing.T) {
	for _, authorized := range []bool{false, true} {
		app := fiber.New()
		if authorized {
			app.Use(func(c *fiber.Ctx) error {
				c.Locals("user", &models.User{ID: 7, Permissions: []models.Permission{{Name: "manage articles"}}})
				return c.Next()
			})
		}
		// Deliberately nil services: the retired route must never call AI or storage.
		handler := &Handler{}
		app.Post("/optimize-save/:content_type/:id", handler.OptimizeAndSave)
		response, err := app.Test(httptest.NewRequest("POST", "/optimize-save/article/1", nil))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		want := fiber.StatusForbidden
		if authorized {
			want = fiber.StatusConflict
		}
		if response.StatusCode != want {
			t.Fatalf("authorized=%v status=%d want=%d", authorized, response.StatusCode, want)
		}
	}
}
