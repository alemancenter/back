package middleware

import (
	"context"
	"encoding/json"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
)

const ipCacheTTL = 5 * time.Minute

type ipStatus struct {
	Blocked bool `json:"blocked"`
	Trusted bool `json:"trusted"`
}

// IPGuard blocks requests from blocked IPs and skips checks for trusted IPs.
// Results are cached in Redis for ipCacheTTL to avoid a DB hit on every request.
func IPGuard() fiber.Handler {
	return func(c *fiber.Ctx) error {
		clientIP := utils.GetClientIP(c)

		if utils.IsLocalhost(clientIP) {
			return c.Next()
		}

		status, err := getIPStatus(clientIP)
		if err != nil {
			// On cache/DB error fall through — don't block legitimate traffic
			return c.Next()
		}

		if status.Blocked {
			return utils.Forbidden(c, "عنوان IP الخاص بك محظور")
		}
		if status.Trusted {
			c.Locals("trusted_ip", true)
		}

		return c.Next()
	}
}

// getIPStatus returns the cached (or freshly loaded) block/trust status for an IP.
func getIPStatus(ip string) (ipStatus, error) {
	rdb := database.Redis()
	ctx := context.Background()
	cacheKey := rdb.Key("ip_guard", ip)

	// Try cache first
	if cached, err := rdb.Get(ctx, cacheKey); err == nil {
		var s ipStatus
		if json.Unmarshal([]byte(cached), &s) == nil {
			return s, nil
		}
	}

	// Cache miss — query DB
	db := database.DB()
	s := ipStatus{}

	var blocked models.BlockedIP
	if err := db.Where("ip_address = ?", ip).First(&blocked).Error; err == nil {
		if !blocked.HasExpired() {
			s.Blocked = true
		} else {
			// Auto-remove expired block
			db.Delete(&blocked)
		}
	} else if db.Migrator().HasTable("banned_ips") {
		// Backward compatibility with the old Laravel table.
		var legacy models.LegacyBannedIP
		if legacyErr := db.Where("ip = ?", ip).First(&legacy).Error; legacyErr == nil {
			mapped := legacy.ToBlockedIP()
			if !mapped.HasExpired() {
				s.Blocked = true
				_ = db.Where(models.BlockedIP{IPAddress: mapped.IPAddress}).Assign(mapped).FirstOrCreate(&mapped).Error
			} else {
				db.Delete(&legacy)
			}
		}
	}

	if !s.Blocked {
		var trusted models.TrustedIP
		if err := db.Where("ip_address = ?", ip).First(&trusted).Error; err == nil {
			s.Trusted = true
		}
	}

	// Cache result
	if data, err := json.Marshal(s); err == nil {
		_ = rdb.Set(ctx, cacheKey, data, ipCacheTTL)
	}

	return s, nil
}

// InvalidateIPCache removes the cached IP status so the next request re-checks DB.
// Call this after blocking or unblocking an IP via the dashboard.
func InvalidateIPCache(ip string) {
	rdb := database.Redis()
	_ = rdb.Del(context.Background(), rdb.Key("ip_guard", ip))
}
