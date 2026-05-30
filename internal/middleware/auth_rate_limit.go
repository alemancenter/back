package middleware

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/alemancenter/fiber-api/pkg/logger"
	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

// authLimitRule defines per-endpoint brute-force limits.
type authLimitRule struct {
	max    int
	window time.Duration
}

// authLimits maps endpoint suffix → rule. Keys are matched as path suffixes.
var authLimits = map[string]authLimitRule{
	"/auth/login":             {max: 5, window: 15 * time.Minute},
	"/auth/register":          {max: 10, window: 15 * time.Minute},
	"/auth/check-email":       {max: 20, window: 10 * time.Minute},
	"/auth/email/preflight":   {max: 20, window: 10 * time.Minute},
	"/auth/password/forgot":   {max: 3, window: 15 * time.Minute},
	"/auth/password/reset":    {max: 5, window: 15 * time.Minute},
	"/auth/refresh":           {max: 20, window: time.Minute},
	"/auth/google/redirect":   {max: 20, window: 15 * time.Minute},
	"/auth/google/callback":   {max: 30, window: 15 * time.Minute},
	"/auth/google/token":      {max: 10, window: 15 * time.Minute},
	"/auth/facebook/redirect": {max: 20, window: 15 * time.Minute},
	"/auth/facebook/callback": {max: 30, window: 15 * time.Minute},
	"/auth/facebook/token":    {max: 10, window: 15 * time.Minute},
}

// AuthRateLimit applies a strict per-IP rate limit for sensitive auth endpoints.
// It is intentionally separate from the general FrontendGuard rate limiter so
// that auth routes can be tuned independently without affecting other API paths.
func AuthRateLimit() fiber.Handler {
	return func(c *fiber.Ctx) error {
		path := c.Path()

		var rule authLimitRule
		matched := false
		for suffix, currentRule := range authLimits {
			if strings.HasSuffix(path, suffix) {
				rule = currentRule
				matched = true
				break
			}
		}
		if !matched {
			return c.Next()
		}

		clientIP := utils.GetClientIP(c)
		rdb := database.GetRedis()
		ctx := context.Background()

		key := rdb.Key("auth_rl", clientIP, path)

		count, err := rdb.IncrBy(ctx, key, 1)
		if err != nil {
			logger.Error("auth rate limit Redis error — failing closed",
				zap.String("ip", clientIP),
				zap.String("path", path),
				zap.Error(err),
			)
			return utils.TooManyRequests(c)
		}

		if count == 1 {
			_ = rdb.Expire(ctx, key, rule.window)
		}

		c.Set("X-RateLimit-Limit", fmt.Sprintf("%d", rule.max))
		c.Set("X-RateLimit-Remaining", fmt.Sprintf("%d", max(0, rule.max-int(count))))

		if int(count) > rule.max {
			retryAfter := int(rule.window.Seconds())
			c.Set("Retry-After", fmt.Sprintf("%d", retryAfter))
			return utils.TooManyRequests(c)
		}

		return c.Next()
	}
}

// RateLimitRule defines a Redis-backed per-IP rule for selected route prefixes.
type RateLimitRule struct {
	Prefix  string
	Methods []string
	Max     int
	Window  time.Duration
}

// PrefixRateLimit applies low-overhead Redis rate limiting to expensive or abuse-prone routes.
// It is designed for AI, upload, and dashboard mutation endpoints where duplicate bursts are costly.
func PrefixRateLimit(rules ...RateLimitRule) fiber.Handler {
	return func(c *fiber.Ctx) error {
		path := c.Path()
		var matched *RateLimitRule
		method := c.Method()
		for i := range rules {
			if rules[i].Prefix == "" || len(path) < len(rules[i].Prefix) || path[:len(rules[i].Prefix)] != rules[i].Prefix {
				continue
			}
			if len(rules[i].Methods) > 0 {
				allowedMethod := false
				for _, ruleMethod := range rules[i].Methods {
					if ruleMethod == method {
						allowedMethod = true
						break
					}
				}
				if !allowedMethod {
					continue
				}
			}
			matched = &rules[i]
			break
		}
		if matched == nil {
			return c.Next()
		}

		clientIP := utils.GetClientIP(c)
		rdb := database.GetRedis()
		ctx := context.Background()
		key := rdb.Key("prefix_rl", clientIP, method, matched.Prefix)

		count, err := rdb.IncrBy(ctx, key, 1)
		if err != nil {
			logger.Error("prefix rate limit Redis error — failing closed", zap.String("ip", clientIP), zap.String("path", path), zap.Error(err))
			return utils.TooManyRequests(c)
		}
		if count == 1 {
			_ = rdb.Expire(ctx, key, matched.Window)
		}
		remaining := matched.Max - int(count)
		if remaining < 0 {
			remaining = 0
		}
		c.Set("X-RateLimit-Limit", fmt.Sprintf("%d", matched.Max))
		c.Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
		if int(count) > matched.Max {
			c.Set("Retry-After", fmt.Sprintf("%d", int(matched.Window.Seconds())))
			return utils.TooManyRequests(c)
		}
		return c.Next()
	}
}
