package routes

import (
	"github.com/alemancenter/fiber-api/internal/middleware"
	"github.com/gofiber/fiber/v2"
)

// registerAuthRoutes handles user authentication, profile management,
// roles, permissions, and users management from the dashboard.
func registerAuthRoutes(api, dash fiber.Router, h *Handlers) {
	// =====================
	// AUTH ROUTES
	// =====================
	authGroup := api.Group("/auth")

	// Brute-force-sensitive endpoints get a dedicated per-IP rate limiter
	authGroup.Post("/check-email", middleware.AuthRateLimit(), h.Auth.CheckEmail)
	authGroup.Post("/email/preflight", middleware.AuthRateLimit(), h.Auth.EmailPreflight)
	authGroup.Post("/register", middleware.AuthRateLimit(), h.Auth.Register)
	authGroup.Post("/login", middleware.AuthRateLimit(), h.Auth.Login)
	authGroup.Post("/refresh", middleware.AuthRateLimit(), h.Auth.RefreshToken)
	authGroup.Post("/password/forgot", middleware.AuthRateLimit(), h.Auth.ForgotPassword)

	authGroup.Get("/google/redirect", middleware.AuthRateLimit(), h.Auth.GoogleRedirect)
	authGroup.Get("/google/callback", middleware.AuthRateLimit(), h.Auth.GoogleCallback)
	authGroup.Post("/google/token", middleware.AuthRateLimit(), h.Auth.GoogleTokenLogin)
	authGroup.Get("/facebook/redirect", middleware.AuthRateLimit(), h.Auth.FacebookRedirect)
	authGroup.Get("/facebook/callback", middleware.AuthRateLimit(), h.Auth.FacebookCallback)
	authGroup.Post("/facebook/token", middleware.AuthRateLimit(), h.Auth.FacebookTokenLogin)
	authGroup.Post("/password/reset", middleware.AuthRateLimit(), h.Auth.ResetPassword)
	authGroup.Get("/email/verify/:id/:hash", h.Auth.VerifyEmail)

	// Authenticated auth routes
	authSecure := authGroup.Group("", middleware.Auth(), middleware.UpdateLastActivity())
	authSecure.Post("/logout", h.Auth.Logout)
	authSecure.Get("/user", h.Auth.Me)
	authSecure.Put("/profile", middleware.RequireVerifiedEmail(), h.Auth.UpdateProfile)
	authSecure.Post("/email/resend", middleware.AuthRateLimit(), h.Auth.ResendVerification)
	authSecure.Post("/email/change", middleware.AuthRateLimit(), h.Auth.ChangeUnverifiedEmail)
	authSecure.Post("/account/delete", middleware.RequireVerifiedEmail(), h.Auth.DeleteAccount)
	authSecure.Post("/push-token", middleware.RequireVerifiedEmail(), h.Auth.RegisterPushToken)
	authSecure.Delete("/push-token", middleware.RequireVerifiedEmail(), h.Auth.DeletePushToken)

	// Public team page — no auth required
	api.Get("/team", h.Users.Team)

	// User Routes
	userRoutes := api.Group("/user", middleware.Auth(), middleware.RequireVerifiedEmail(), middleware.UpdateLastActivity())

	// Roles (view only)
	userRoutes.Get("/roles", h.Roles.ListRoles)
	userRoutes.Get("/roles/:id", h.Roles.GetRole)

	// User search (for messaging — requires only auth, not manage users)
	userRoutes.Get("/search", h.Users.Search)

	// =====================
	// ADMIN DASHBOARD ROUTES
	// =====================

	// Roles & Permissions
	dashRoles := dash.Group("/roles", middleware.Can("manage roles"))
	dashRoles.Get("", h.Roles.ListRoles)
	dashRoles.Post("", h.Roles.CreateRole)
	dashRoles.Get("/:id", h.Roles.GetRole)
	dashRoles.Put("/:id", h.Roles.UpdateRole)
	dashRoles.Delete("/:id", h.Roles.DeleteRole)

	dashPermissions := dash.Group("/permissions", middleware.Can("manage roles"))
	dashPermissions.Get("", h.Roles.ListPermissions)
	dashPermissions.Post("", h.Roles.CreatePermission)
	dashPermissions.Put("/:id", h.Roles.UpdatePermission)
	dashPermissions.Delete("/:id", h.Roles.DeletePermission)

	// Users management
	dashUsers := dash.Group("/users", middleware.Can("manage users"))
	dashUsers.Get("/search", h.Users.Search)
	dashUsers.Post("/bulk-delete", h.Users.BulkDelete)
	dashUsers.Post("/update-status", h.Users.UpdateStatus)
	dashUsers.Get("", h.Users.List)
	dashUsers.Post("", h.Users.Create)
	dashUsers.Put("/:user/roles-permissions", h.Users.UpdateRolesPermissions)
	dashUsers.Get("/:user", h.Users.Show)
	dashUsers.Put("/:user", h.Users.Update)
	dashUsers.Delete("/:user", h.Users.Delete)
}
