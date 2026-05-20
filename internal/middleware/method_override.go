package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

// MethodOverride is a middleware that intercepts POST requests and checks
// for a "_method" form field (common in frontend frameworks like Next.js/React or Laravel)
// to override the HTTP method to PUT, PATCH, or DELETE.
func MethodOverride() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Only intercept POST requests
		if c.Method() == fiber.MethodPost {
			// Check if the content type is multipart/form-data or application/x-www-form-urlencoded
			contentType := string(c.Request().Header.ContentType())
			if strings.HasPrefix(contentType, "multipart/form-data") || strings.HasPrefix(contentType, "application/x-www-form-urlencoded") {
				method := c.FormValue("_method")
				if method != "" {
					method = strings.ToUpper(method)
					if method == fiber.MethodPut || method == fiber.MethodPatch || method == fiber.MethodDelete {
						// Override the method
						c.Method(method)
					}
				}
			}
		}
		return c.Next()
	}
}
