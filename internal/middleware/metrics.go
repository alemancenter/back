package middleware

import (
	"time"

	"github.com/alemancenter/fiber-api/internal/monitoring"
	"github.com/gofiber/fiber/v2"
)

func Metrics() fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		path := c.Path()
		if route := c.Route(); route != nil && route.Path != "" {
			path = route.Path
		}
		monitoring.RecordRequest(c.Method(), path, c.Response().StatusCode(), time.Since(start))
		return err
	}
}

func PrometheusMetrics(c *fiber.Ctx) error {
	c.Set(fiber.HeaderContentType, "text/plain; version=0.0.4; charset=utf-8")
	return c.SendString(monitoring.PrometheusText())
}

func MetricsSnapshot(c *fiber.Ctx) error {
	return c.JSON(monitoring.SnapshotData())
}
