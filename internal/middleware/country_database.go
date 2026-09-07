package middleware

import (
	"github.com/gofiber/fiber/v2"
	"github.com/imanjo/fiber-api/internal/database"
)

func RequireCountryDatabase() fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, _ := c.Locals("country_id").(database.CountryID)
		if database.DBForCountry(id).Error != nil {
			return c.Status(503).JSON(fiber.Map{"success": false, "message": "خدمة الدولة المختارة غير متاحة مؤقتًا", "code": "COUNTRY_UNAVAILABLE"})
		}
		return c.Next()
	}
}
