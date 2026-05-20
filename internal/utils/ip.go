package utils

import (
	"net"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// GetClientIP resolves the real client IP considering proxy headers
func GetClientIP(c *fiber.Ctx) string {
	// Cloudflare
	if ip := c.Get("CF-Connecting-IP"); ip != "" {
		return cleanIP(ip)
	}
	// X-Forwarded-For (first trusted IP)
	if xff := c.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return cleanIP(ip)
		}
	}
	// X-Real-IP
	if ip := c.Get("X-Real-IP"); ip != "" {
		return cleanIP(ip)
	}
	return c.IP()
}

// IsLocalhost checks if an IP is localhost
func IsLocalhost(ip string) bool {
	ip = cleanIP(ip)
	return ip == "127.0.0.1" || ip == "::1" || ip == "localhost"
}

// IsPrivateIP checks if an IP is in a private range
func IsPrivateIP(ipStr string) bool {
	ip := net.ParseIP(cleanIP(ipStr))
	if ip == nil {
		return false
	}
	private := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"::1/128",
		"fc00::/7",
	}
	for _, cidr := range private {
		_, network, err := net.ParseCIDR(cidr)
		if err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

// IsSSRUserAgent checks if the user agent belongs to a server-side rendering engine
func IsSSRUserAgent(ua string) bool {
	ua = strings.ToLower(ua)
	ssrAgents := []string{"node", "undici", "next.js", "nextjs", "nuxt", "gatsby"}
	for _, agent := range ssrAgents {
		if strings.Contains(ua, agent) {
			return true
		}
	}
	return false
}

func cleanIP(ip string) string {
	// Remove port if present
	if host, _, err := net.SplitHostPort(ip); err == nil {
		return host
	}
	return strings.TrimSpace(ip)
}
