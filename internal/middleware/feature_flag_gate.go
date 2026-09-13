package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/services"
	"github.com/imanjo/fiber-api/internal/utils"
)

// FeatureFlagGate blocks a route while the given public boolean setting is off (missing or any
// value other than "false"/"0"/"no"/"off" is treated as enabled, matching every other toggle's
// convention in this codebase — see DownloadAuthGate). Factored out because two settings toggles
// (enable_registration, enable_teacher_subscriptions) existed in the dashboard with matching
// descriptions implying they gated the underlying API, but had no server-side enforcement at
// all — only a frontend/BFF-level hide, which a request made directly against the backend
// bypasses entirely.
func FeatureFlagGate(svc services.SettingService, settingKey, errorCode, deniedMessage string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		countryID, _ := c.Locals("country_id").(database.CountryID)
		if countryID == 0 {
			countryID = database.CountryJordan
		}

		enabled := true
		if settings, err := svc.GetPublic(c.Context(), countryID); err == nil && settings != nil {
			if v, ok := settings[settingKey]; ok {
				switch strings.ToLower(strings.TrimSpace(v)) {
				case "false", "0", "no", "off":
					enabled = false
				}
			}
		}

		if !enabled {
			return utils.ForbiddenCode(c, errorCode, deniedMessage)
		}

		return c.Next()
	}
}
