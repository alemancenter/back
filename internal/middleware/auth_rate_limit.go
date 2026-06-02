package middleware

import (
	"context"
	"crypto/sha256"
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
// max/window protect by IP. subjectMax/subjectWindow protect a specific email/user.
type authLimitRule struct {
	max           int
	window        time.Duration
	subjectMax    int
	subjectWindow time.Duration
}

// authLimits maps endpoint suffix → rule. Keys are matched as path suffixes.
// Nginx already performs a first-pass IP throttle; these Redis rules protect the backend
// by IP and by logical actor (email/user) so one endpoint cannot be abused repeatedly.
var authLimits = map[string]authLimitRule{
	"/auth/login":             {max: 10, window: 10 * time.Minute, subjectMax: 5, subjectWindow: 10 * time.Minute},
	"/auth/register":          {max: 10, window: 15 * time.Minute, subjectMax: 3, subjectWindow: 30 * time.Minute},
	"/auth/check-email":       {max: 30, window: 10 * time.Minute, subjectMax: 8, subjectWindow: 10 * time.Minute},
	"/auth/email/preflight":   {max: 30, window: 10 * time.Minute, subjectMax: 8, subjectWindow: 10 * time.Minute},
	"/auth/email/verify/":     {max: 10, window: 15 * time.Minute},
	"/auth/email/resend":      {max: 5, window: time.Hour, subjectMax: 5, subjectWindow: time.Hour},
	"/auth/email/change":      {max: 5, window: 30 * time.Minute, subjectMax: 3, subjectWindow: 30 * time.Minute},
	"/auth/password/forgot":   {max: 5, window: time.Hour, subjectMax: 3, subjectWindow: time.Hour},
	"/auth/password/reset":    {max: 5, window: 15 * time.Minute, subjectMax: 5, subjectWindow: 15 * time.Minute},
	"/auth/refresh":           {max: 30, window: time.Minute},
	"/auth/google/redirect":   {max: 10, window: 15 * time.Minute},
	"/auth/google/callback":   {max: 20, window: 15 * time.Minute},
	"/auth/google/token":      {max: 10, window: 15 * time.Minute},
	"/auth/facebook/redirect": {max: 10, window: 15 * time.Minute},
	"/auth/facebook/callback": {max: 20, window: 15 * time.Minute},
	"/auth/facebook/token":    {max: 10, window: 15 * time.Minute},
}

func matchAuthLimitRule(path string) (authLimitRule, bool) {
	for pattern, currentRule := range authLimits {
		// Keys ending with "/" are prefix rules (e.g. "/auth/email/verify/" matches
		// "/api/auth/email/verify/123/abc"). All others are exact suffix matches.
		if strings.HasSuffix(pattern, "/") {
			if strings.Contains(path, pattern) {
				return currentRule, true
			}
		} else if strings.HasSuffix(path, pattern) {
			return currentRule, true
		}
	}
	return authLimitRule{}, false
}

func normalizeRateLimitEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func hashRateLimitSubject(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum)
}

func subjectFromRequest(c *fiber.Ctx) string {
	if userID := c.Locals("user_id"); userID != nil {
		return fmt.Sprintf("user:%v", userID)
	}

	var payload struct {
		Email string `json:"email" form:"email"`
	}
	if err := c.BodyParser(&payload); err == nil {
		if email := normalizeRateLimitEmail(payload.Email); email != "" {
			return "email:" + hashRateLimitSubject(email)
		}
	}

	return ""
}

func applyRedisRateLimit(c *fiber.Ctx, key string, maxAllowed int, window time.Duration) (int64, error) {
	rdb := database.GetRedis()
	ctx := context.Background()
	count, err := rdb.IncrBy(ctx, key, 1)
	if err != nil {
		return 0, err
	}
	if count == 1 {
		_ = rdb.Expire(ctx, key, window)
	}
	return count, nil
}

// AuthRateLimit applies Redis-backed rate limiting for sensitive auth endpoints.
// It limits both by client IP and, when available, by email/user. Redis errors fail closed
// because auth endpoints are abuse-prone and should not bypass throttling when Redis fails.
func AuthRateLimit() fiber.Handler {
	return func(c *fiber.Ctx) error {
		path := c.Path()
		rule, matched := matchAuthLimitRule(path)
		if !matched {
			return c.Next()
		}

		clientIP := utils.GetClientIP(c)
		rdb := database.GetRedis()
		ipKey := rdb.Key("auth_rl", clientIP, c.Method(), path)

		ipCount, err := applyRedisRateLimit(c, ipKey, rule.max, rule.window)
		if err != nil {
			logger.Error("auth rate limit Redis error — failing closed",
				zap.String("ip", clientIP),
				zap.String("path", path),
				zap.Error(err),
			)
			return utils.TooManyRequests(c)
		}

		remaining := rule.max - int(ipCount)
		if remaining < 0 {
			remaining = 0
		}
		c.Set("X-RateLimit-Limit", fmt.Sprintf("%d", rule.max))
		c.Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))

		if int(ipCount) > rule.max {
			c.Set("Retry-After", fmt.Sprintf("%d", int(rule.window.Seconds())))
			return utils.TooManyRequests(c)
		}

		if rule.subjectMax > 0 {
			if subject := subjectFromRequest(c); subject != "" {
				subjectKey := rdb.Key("auth_subject_rl", subject, c.Method(), path)
				subjectCount, err := applyRedisRateLimit(c, subjectKey, rule.subjectMax, rule.subjectWindow)
				if err != nil {
					logger.Error("auth subject rate limit Redis error — failing closed",
						zap.String("ip", clientIP),
						zap.String("path", path),
						zap.Error(err),
					)
					return utils.TooManyRequests(c)
				}
				if int(subjectCount) > rule.subjectMax {
					c.Set("Retry-After", fmt.Sprintf("%d", int(rule.subjectWindow.Seconds())))
					return utils.TooManyRequests(c)
				}
			}
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
