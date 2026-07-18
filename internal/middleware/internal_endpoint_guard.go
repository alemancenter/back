package middleware

import (
	"crypto/subtle"
	"net"
	"strings"

	"github.com/alemancenter/fiber-api/internal/config"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
)

// InternalEndpointGuard protects operational endpoints such as health checks
// and metrics. Local server checks are allowed only when the request is direct,
// not forwarded by nginx from the public internet.
func InternalEndpointGuard() fiber.Handler {
	cfg := config.Get()
	return internalEndpointGuard(cfg.Security.InternalMonitorKey, cfg.Frontend.APIKey)
}

func internalEndpointGuard(internalMonitorKey string, frontendAPIKey string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if isDirectLocalEndpointRequest(c) {
			return c.Next()
		}

		if secureHeaderEqual(internalMonitorKey, c.Get("X-Internal-Monitor-Key")) {
			return c.Next()
		}

		if secureHeaderEqual(frontendAPIKey, c.Get("X-Frontend-Key")) {
			return c.Next()
		}

		return utils.NotFound(c)
	}
}

func secureHeaderEqual(expected string, actual string) bool {
	expected = strings.TrimSpace(expected)
	actual = strings.TrimSpace(actual)
	if expected == "" || actual == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}

func isDirectLocalEndpointRequest(c *fiber.Ctx) bool {
	if !utils.IsLocalhost(hostWithoutPort(c.IP())) {
		return false
	}

	if c.Get("Forwarded") != "" ||
		c.Get("X-Forwarded-For") != "" ||
		c.Get("X-Real-IP") != "" ||
		c.Get("CF-Connecting-IP") != "" {
		return false
	}

	host := hostWithoutPort(c.Hostname())
	return host == "" || utils.IsLocalhost(host)
}

func hostWithoutPort(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		return strings.Trim(h, "[]")
	}
	return strings.Trim(host, "[]")
}
