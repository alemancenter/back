package middleware

import (
	"strings"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
)

// DownloadAuthGate conditionally requires authentication for file downloads
// based on the public "require_login_for_download" setting. When the setting
// is "false"/"0"/"no", anonymous downloads are allowed. Otherwise (default),
// the user must be authenticated with a verified email.
//
// In both modes a Bearer/cookie token is loaded if present so handlers can
// attribute downloads to the user when available.
func DownloadAuthGate(svc services.SettingService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Best-effort attach user when a token is present (no failure on miss).
		attachOptionalUser(c)

		countryID, _ := c.Locals("country_id").(database.CountryID)
		if countryID == 0 {
			countryID = database.CountryJordan
		}

		requireLogin := true
		if settings, err := svc.GetPublic(c.Context(), countryID); err == nil && settings != nil {
			if v, ok := settings["require_login_for_download"]; ok {
				switch strings.ToLower(strings.TrimSpace(v)) {
				case "false", "0", "no", "off":
					requireLogin = false
				}
			}
		}

		if !requireLogin {
			return c.Next()
		}

		user, _ := c.Locals("user").(*models.User)
		if user == nil {
			return utils.UnauthorizedCode(c, "AUTH_REQUIRED", "يرجى تسجيل الدخول أولًا")
		}
		if !user.IsVerified() && !user.IsAdmin() {
			return utils.ForbiddenCode(c, "EMAIL_NOT_VERIFIED", "يرجى تفعيل بريدك الإلكتروني قبل استخدام هذه الخدمة")
		}

		return c.Next()
	}
}

// attachOptionalUser mirrors OptionalAuth but only sets locals — it does not
// call c.Next() so the caller can continue running additional checks.
func attachOptionalUser(c *fiber.Ctx) {
	tokenStr := authTokenFromRequest(c)
	if tokenStr == "" {
		return
	}

	jwtSvc := services.NewJWTService()
	claims, err := jwtSvc.ValidateToken(tokenStr)
	if err != nil {
		return
	}

	user, err := loadUserCached(claims.UserID)
	if err != nil {
		return
	}

	c.Locals("user", user)
	c.Locals("user_id", user.ID)
	c.Locals("auth_token", tokenStr)
}
