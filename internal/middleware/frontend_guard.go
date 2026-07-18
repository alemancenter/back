package middleware

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/alemancenter/fiber-api/internal/config"
	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/alemancenter/fiber-api/pkg/logger"
	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

// FrontendGuard validates API requests are from authorized frontends.
// Mirrors Laravel's FrontendApiGuard middleware.
func FrontendGuard() fiber.Handler {
	cfg := config.Get()
	return frontendGuard(cfg)
}

func frontendGuard(cfg *config.Config) fiber.Handler {
	// Paths excluded from frontend guard validation.
	// OAuth redirect/callback URLs come from external providers (Google, Facebook)
	// without X-Frontend-Key or controlled Origin headers — they must bypass the guard.
	// Email verification links are opened directly from email clients.
	excludedPaths := []string{
		"/api/auth/google/redirect",
		"/api/auth/google/callback",
		"/api/auth/facebook/redirect",
		"/api/auth/facebook/callback",
		"/api/auth/email/verify/",
	}

	return func(c *fiber.Ctx) error {
		path := c.Path()

		// Skip excluded paths
		for _, excluded := range excludedPaths {
			if strings.HasPrefix(path, excluded) {
				return continueWithCountry(c, cfg)
			}
		}

		clientIP := utils.GetClientIP(c)
		origin := c.Get("Origin")
		frontendKey := c.Get("X-Frontend-Key")
		userAgent := c.Get("User-Agent")
		requestedWith := c.Get("X-Requested-With")
		referer := c.Get("Referer")
		authToken := authTokenFromRequest(c)
		isPublicAPIHost := isConfiguredAPIHost(c, cfg)

		// 0. Frontend API key — highest priority, always checked first.
		// Next.js must send: headers: { "X-Frontend-Key": NEXT_PUBLIC_FRONTEND_API_KEY }
		// This bypasses Origin/Referer checks for SSR and curl requests.
		if cfg.Frontend.APIKey != "" && frontendKey == cfg.Frontend.APIKey {
			c.Locals("client_type", "frontend_marker")
			return continueWithCountry(c, cfg)
		}

		// 1. Direct localhost bypass for server-side checks only. A request
		// forwarded by nginx from the public internet is not treated as local,
		// even though c.IP() can be 127.0.0.1 after proxying.
		if isDirectLocalEndpointRequest(c) {
			c.Locals("client_type", "localhost")
			logger.Debug("[FG] tier-1 localhost bypass",
				zap.String("path", path),
				zap.String("ip", clientIP),
				zap.String("real_ip", c.IP()),
			)
			return continueWithCountry(c, cfg)
		}

		// 2. The public api.alemancenter.com host is not a public data surface.
		// Browser traffic must go through alemancenter.com's Node proxy, which
		// injects X-Frontend-Key server-side. This blocks direct curl/Postman and
		// direct browser calls to /api/* before public handlers can disclose data.
		if cfg.App.IsProduction() && isPublicAPIHost {
			logger.Warn("[FG] direct public API host blocked",
				zap.String("path", path),
				zap.String("ip", clientIP),
				zap.String("host", c.Hostname()),
				zap.String("origin", origin),
				zap.Bool("has_auth_header", authToken != ""),
			)
			return utils.NotFound(c)
		}
		logger.Debug("[FG] tier-1 NOT localhost",
			zap.String("path", path),
			zap.String("ip", clientIP),
		)

		// 3. SSR (Server-Side Rendering) detection — Node.js/Next.js
		if utils.IsSSRUserAgent(userAgent) {
			isSSRTrusted := false
			if isDirectLocalEndpointRequest(c) {
				isSSRTrusted = true
			} else {
				for _, ip := range cfg.Frontend.SSRTrustedIPs {
					if strings.TrimSpace(ip) == clientIP {
						isSSRTrusted = true
						break
					}
				}
			}
			if isSSRTrusted {
				c.Locals("client_type", "ssr")
				c.Locals("rate_limit_max", cfg.Frontend.SSRRateLimitMax)
				logger.Debug("[FG] tier-2 SSR bypass",
					zap.String("path", path),
					zap.String("ip", clientIP),
				)
				return continueWithCountry(c, cfg)
			}
			logger.Debug("[FG] tier-2 SSR blocked",
				zap.String("path", path),
				zap.String("ip", clientIP),
			)
		}

		// 4. Authenticated token: Authorization bearer or transitional HttpOnly cookie
		if authToken != "" {
			c.Locals("client_type", "auth_token")
			return continueWithCountry(c, cfg)
		}

		// 5. Origin + Referer validation (browser CORS)
		if origin != "" {
			if isAllowedOrigin(origin, cfg.Frontend.CORSOrigins) {
				// Allow if requested with XMLHttpRequest or if referer is set
				if requestedWith == "XMLHttpRequest" || referer != "" || requestedWith == "" {
					c.Locals("client_type", "browser")
					return continueWithCountry(c, cfg)
				}
			} else {
				if !cfg.App.IsProduction() {
					c.Locals("client_type", "unknown")
					return continueWithCountry(c, cfg)
				}
				return utils.Forbidden(c, "Origin غير مصرح بالوصول")
			}
		}

		// 6. Public API access (cURL, Postman) without strict CORS
		if origin == "" && frontendKey == "" && authToken == "" {
			if !cfg.App.IsProduction() {
				c.Locals("client_type", "unknown")
				return continueWithCountry(c, cfg)
			}
			return utils.Forbidden(c, "غير مصرح بالوصول: يتطلب توثيق")
		}

		// Allow by default for development if we reach here
		if !cfg.App.IsProduction() {
			c.Locals("client_type", "unknown")
			logger.Debug("[FG] tier-fallback dev bypass",
				zap.String("path", path),
				zap.String("ip", clientIP),
				zap.String("user_agent", userAgent),
			)
			return continueWithCountry(c, cfg)
		}

		logger.Warn("[FG] request blocked",
			zap.String("path", path),
			zap.String("ip", clientIP),
			zap.String("origin", origin),
			zap.String("user_agent", userAgent),
		)
		return utils.Forbidden(c, "غير مصرح بالوصول")
	}
}

func isConfiguredAPIHost(c *fiber.Ctx, cfg *config.Config) bool {
	requestHost := strings.ToLower(hostWithoutPort(c.Hostname()))
	if requestHost == "" {
		return false
	}

	configuredHost := configuredAppHost(cfg.App.URL)
	if configuredHost != "" {
		return requestHost == configuredHost
	}

	return strings.HasPrefix(requestHost, "api.")
}

func configuredAppHost(appURL string) string {
	appURL = strings.TrimSpace(appURL)
	if appURL == "" {
		return ""
	}
	if u, err := url.Parse(appURL); err == nil && u.Host != "" {
		return strings.ToLower(hostWithoutPort(u.Host))
	}
	return strings.ToLower(hostWithoutPort(appURL))
}

// continueWithCountry sets the country database connection and proceeds
func continueWithCountry(c *fiber.Ctx, cfg *config.Config) error {
	countryHeader := c.Get("X-Country-Id")
	if countryHeader == "" {
		countryHeader = c.Query("country", "1")
	}

	countryID := database.CountryIDFromHeader(countryHeader)
	c.Locals("country_id", countryID)
	c.Locals("country_code", database.CountryCode(countryID))

	// Apply frontend rate limiting
	if cfg.Frontend.RateLimit {
		if err := applyRateLimit(c, cfg, countryID); err != nil {
			return err
		}
	}

	return c.Next()
}

// applyRateLimit applies Redis-backed rate limiting
func applyRateLimit(c *fiber.Ctx, cfg *config.Config, countryID database.CountryID) error {
	clientIP := utils.GetClientIP(c)
	maxReqs := cfg.Frontend.RateLimitMax
	window := cfg.Frontend.RateLimitWindow

	// SSR gets higher limit
	if limit, ok := c.Locals("rate_limit_max").(int); ok {
		maxReqs = limit
	}

	// Login endpoints get stricter limits
	path := c.Path()
	if strings.Contains(path, "/auth/login") || strings.Contains(path, "/auth/register") {
		maxReqs = cfg.Frontend.LoginRateLimit
		window = 60
	}

	rdb := database.GetRedis()
	ctx := context.Background()

	// Incorporate countryCode into the rate limit key to isolate limits per country if needed
	countryCode := database.CountryCode(countryID)
	key := rdb.Key("ratelimit", countryCode, clientIP, path)

	count, err := rdb.IncrBy(ctx, key, 1)
	if err != nil {
		// Fail closed: Redis unavailable means rate limiting cannot be verified.
		// Log and reject to prevent brute-force bypass during Redis outage.
		logger.Error("rate limit Redis error — failing closed",
			zap.String("ip", clientIP),
			zap.String("path", path),
			zap.Error(err),
		)
		return utils.TooManyRequests(c)
	}

	if count == 1 {
		_ = rdb.Expire(ctx, key, time.Duration(window)*time.Second)
	}

	c.Set("X-RateLimit-Limit", fmt.Sprintf("%d", maxReqs))
	c.Set("X-RateLimit-Remaining", fmt.Sprintf("%d", max(0, maxReqs-int(count))))

	if int(count) > maxReqs {
		return utils.TooManyRequests(c)
	}

	return nil
}

// isAllowedOrigin checks if origin is in the allowed list
func isAllowedOrigin(origin string, allowed []string) bool {
	for _, a := range allowed {
		if strings.TrimSpace(a) == origin {
			return true
		}
	}
	return false
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
