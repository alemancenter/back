package middleware

import (
	"github.com/gofiber/fiber/v2"
	"github.com/imanjo/fiber-api/internal/services"
	"path/filepath"
	"strings"
)

func PublicStorage(root string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if c.Method() != "GET" && c.Method() != "HEAD" {
			return c.SendStatus(405)
		}
		p := strings.TrimPrefix(c.Path(), "/storage/")
		if p != filepath.ToSlash(filepath.Clean(p)) || strings.ContainsAny(p, "\\\x00") {
			return c.SendStatus(404)
		}
		ext := strings.ToLower(filepath.Ext(p))
		allowed := ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp" || ext == ".gif" || ext == ".avif" || ext == ".ico" || ext == ".svg"
		sitemap := strings.HasPrefix(p, "sitemaps/") && ext == ".xml"
		if strings.HasPrefix(p, "private/") || strings.HasPrefix(p, "files/") || (!allowed && !sitemap) {
			return c.SendStatus(404)
		}
		resolved, err := services.ResolveStoragePath(root, p)
		if err != nil {
			return c.SendStatus(404)
		}
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
		c.Set("Cache-Control", "public, max-age=3600")
		return c.SendFile(resolved)
	}
}
