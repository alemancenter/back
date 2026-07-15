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
		status := c.Response().StatusCode()
		// On a 5xx, capture the handler error text (and the concrete URL) so the
		// performance dashboard can surface what actually failed.
		errMsg := ""
		if status >= 500 && err != nil {
			errMsg = err.Error()
		}
		monitoring.RecordRequestWithError(c.Method(), path, status, time.Since(start), errMsg, c.OriginalURL())
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
