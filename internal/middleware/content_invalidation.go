package middleware

import (
	"context"
	"github.com/gofiber/fiber/v2"
	"github.com/imanjo/fiber-api/internal/database"
	"github.com/redis/go-redis/v9"
	"strings"
	"time"
)

func InvalidateContentAfterWrite() fiber.Handler {
	return func(c *fiber.Ctx) error {
		err := c.Next()
		if err != nil || c.Response().StatusCode() >= 400 || c.Method() == "GET" || c.Method() == "HEAD" {
			return err
		}
		p := c.Path()
		content := false
		for _, prefix := range []string{"/api/dashboard/articles", "/api/dashboard/posts", "/api/dashboard/files", "/api/dashboard/categories", "/api/dashboard/school-classes", "/api/dashboard/subjects", "/api/dashboard/semesters", "/api/dashboard/seo", "/api/dashboard/content-audit"} {
			if strings.HasPrefix(p, prefix) {
				content = true
				break
			}
		}
		if !content {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		rdb := database.Redis()
		for _, client := range []*redis.Client{rdb.Default(), rdb.Cache()} {
			for _, pattern := range []string{"articles:list:*", "posts:list:*", "school-classes:*", "school-class:*", "filter:*", rdb.Key("home", "*"), rdb.Key("http_cache", "*")} {
				iter := client.Scan(ctx, 0, pattern, 100).Iterator()
				for iter.Next(ctx) {
					_ = client.Del(ctx, iter.Val()).Err()
				}
			}
		}
		return nil
	}
}
