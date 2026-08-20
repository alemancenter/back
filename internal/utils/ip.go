package utils

import (
	"net"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// GetClientIP resolves one canonical, syntactically valid client IP.
//
// Production Nginx overwrites X-Real-IP with $remote_addr after its trusted
// Cloudflare real-ip processing, so that value is preferred over user-supplied
// X-Forwarded-For data.
//
// Internal Astro -> Go calls do not pass through the API Nginx vhost, so they
// fall back to X-Forwarded-For, which Astro sets from its request context.
//
// Invalid values are never returned to analytics/security callers.
func GetClientIP(c *fiber.Ctx) string {
	// Canonical value written by our Nginx proxy.
	if ip := normalizeIP(c.Get("X-Real-IP")); ip != "" {
		return ip
	}

	// Cloudflare direct requests.
	if ip := normalizeIP(c.Get("CF-Connecting-IP")); ip != "" {
		return ip
	}

	// Astro internal proxy / conventional proxy chain.
	if ip := firstValidForwardedIP(c.Get("X-Forwarded-For")); ip != "" {
		return ip
	}

	// Direct/local fallback.
	return normalizeIP(c.IP())
}

// firstValidForwardedIP walks an X-Forwarded-For chain left-to-right but only
// accepts entries that are syntactically valid IP addresses.
func firstValidForwardedIP(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}

	for _, part := range strings.Split(raw, ",") {
		if ip := normalizeIP(part); ip != "" {
			return ip
		}
	}

	return ""
}

// normalizeIP validates and canonicalizes IPv4/IPv6.
//
// It accepts normal host:port and bracketed IPv6 forms, but intentionally does
// not try to "repair" malformed addresses. Analytics must never persist
// arbitrary header content as an IP address.
func normalizeIP(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}

	value = strings.Trim(value, "\"'")
	value = strings.TrimSpace(value)

	if strings.HasPrefix(strings.ToLower(value), "for=") {
		value = strings.TrimSpace(value[4:])
		value = strings.Trim(value, "\"'")
	}

	// host:port, including [IPv6]:port
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}

	value = strings.TrimSpace(value)
	value = strings.Trim(value, "[]")

	ip := net.ParseIP(value)
	if ip == nil {
		return ""
	}

	if ipv4 := ip.To4(); ipv4 != nil {
		return ipv4.String()
	}

	return ip.String()
}

// IsLocalhost checks if an IP is localhost.
func IsLocalhost(ip string) bool {
	parsed := net.ParseIP(normalizeIP(ip))
	return parsed != nil && parsed.IsLoopback()
}

// IsPrivateIP checks if an IP is private/local.
func IsPrivateIP(ipStr string) bool {
	ip := net.ParseIP(normalizeIP(ipStr))
	if ip == nil {
		return false
	}

	return ip.IsPrivate() || ip.IsLoopback()
}

// IsSSRUserAgent checks if the user agent belongs to a server-side rendering engine.
func IsSSRUserAgent(ua string) bool {
	ua = strings.ToLower(ua)

	ssrAgents := []string{
		"node",
		"undici",
		"next.js",
		"nextjs",
		"nuxt",
		"gatsby",
	}

	for _, agent := range ssrAgents {
		if strings.Contains(ua, agent) {
			return true
		}
	}

	return false
}

// cleanIP remains for internal backward compatibility.
func cleanIP(ip string) string {
	return normalizeIP(ip)
}
