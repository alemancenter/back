package middleware

import (
	"fmt"
	"time"

	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/alemancenter/fiber-api/pkg/logger"
	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

// RequestLogger logs incoming requests and their responses
func RequestLogger() fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		clientIP := utils.GetClientIP(c)

		err := c.Next()

		latency := time.Since(start)
		status := c.Response().StatusCode()

		fields := []zap.Field{
			zap.String("method", c.Method()),
			zap.String("path", c.Path()),
			zap.Int("status", status),
			zap.String("ip", clientIP),
			zap.String("latency", fmt.Sprintf("%v", latency)),
			zap.String("user_agent", c.Get("User-Agent")),
		}

		if reqID, ok := c.Locals("request_id").(string); ok && reqID != "" {
			fields = append(fields, zap.String("request_id", reqID))
		}
		if userID, ok := c.Locals("user_id").(uint); ok {
			fields = append(fields, zap.Uint("user_id", userID))
		}

		if status >= 500 {
			logger.Error("request completed with server error", fields...)
		} else if status >= 400 {
			logger.Warn("request completed with client error", fields...)
		} else {
			logger.Info("request completed", fields...)
		}

		return err
	}
}
