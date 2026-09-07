package middleware

import (
	"github.com/gofiber/fiber/v2"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPublicStorageRejectsDocumentsAndPrivateMedia(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"files/draft.pdf", "files/draft.png", "private/photo.png", "images/logo.png"} {
		full := filepath.Join(root, p)
		os.MkdirAll(filepath.Dir(full), 0700)
		os.WriteFile(full, []byte("test"), 0600)
	}
	app := fiber.New()
	app.Use("/storage", PublicStorage(root))
	for _, p := range []string{"files/draft.pdf", "files/draft.png", "private/photo.png", "images/../private/photo.png"} {
		res, err := app.Test(httptest.NewRequest("GET", "/storage/"+p, nil))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 404 {
			t.Errorf("exposed %s: %d", p, res.StatusCode)
		}
	}
	res, err := app.Test(httptest.NewRequest("GET", "/storage/images/logo.png", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
}
