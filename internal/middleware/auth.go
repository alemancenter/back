package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/models"
	"github.com/imanjo/fiber-api/internal/services"
	"github.com/imanjo/fiber-api/internal/utils"
	"github.com/imanjo/fiber-api/pkg/logger"
	"go.uber.org/zap"
)

// userCacheTTL is a fallback bound for stale user data if explicit eviction is missed.
// Security-sensitive role, permission, and status mutations invalidate affected users immediately.
const userCacheTTL = 15 * time.Minute

// loadUserCached returns the user from Redis cache or falls back to DB.
// On a cache miss it loads with Preload("Roles.Permissions","Permissions") and writes the result to cache.
func loadUserCached(userID uint) (*models.User, error) {
	ctx := context.Background()
	rdb := database.Redis()
	key := rdb.Key("user", fmt.Sprintf("%d", userID))

	if cached, err := rdb.Get(ctx, key); err == nil {
		var user models.User
		if json.Unmarshal([]byte(cached), &user) == nil {
			return &user, nil
		}
	}

	db := database.DB()
	var user models.User
	if err := db.Preload("Roles.Permissions").Preload("Permissions").
		First(&user, userID).Error; err != nil {
		return nil, err
	}

	if data, err := json.Marshal(user); err == nil {
		_ = rdb.Set(ctx, key, data, userCacheTTL)
	}
	return &user, nil
}

// InvalidateUserCache delegates to services.InvalidateUserCache.
// Kept as a shim so existing callers in this package don't need to import services.
func InvalidateUserCache(userID uint) { services.InvalidateUserCache(userID) }

// authTokenFromRequest accepts the current Bearer header and the transitional
// HttpOnly token cookie used by the Next.js frontend.
func authTokenFromRequest(c *fiber.Ctx) string {
	authHeader := c.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	}
	return strings.TrimSpace(c.Cookies("token"))
}

// IMANJO_AUTH_BLACKLIST_UNIFY_V1
//
// isTokenBlacklisted centralizes access-token revocation checks so mandatory
// and optional authentication paths apply the exact same logout semantics.
//
// Redis lookup failures intentionally preserve the existing Auth() behavior:
// a Redis error is not treated as a positive blacklist match.
func isTokenBlacklisted(tokenStr string) bool {
	if strings.TrimSpace(tokenStr) == "" {
		return false
	}

	ctx := context.Background()
	rdb := database.Redis()

	hash := sha256.Sum256([]byte(tokenStr))
	key := rdb.Key("blacklist", fmt.Sprintf("%x", hash))

	exists, _ := rdb.Exists(ctx, key)
	return exists
}

// isAuthVersionAllowed compares the JWT against the durable MariaDB state.
//
// This intentionally bypasses Redis user-cache state: a password reset must
// remain authoritative even after Redis restart, cache loss, or cache staleness.
func isAuthVersionAllowed(claims *services.JWTClaims) bool {
	if claims == nil || claims.UserID == 0 {
		return false
	}

	var user models.User
	if err := database.DB().
		Select("auth_version").
		First(&user, claims.UserID).Error; err != nil {
		// Revocation state is security-sensitive: fail closed.
		return false
	}

	return claims.AuthVersion == user.AuthVersion
}

// Auth validates JWT token and loads the current user (with Redis caching).
func Auth() fiber.Handler {
	return func(c *fiber.Ctx) error {
		tokenStr := authTokenFromRequest(c)
		if tokenStr == "" {
			return utils.UnauthorizedCode(c, "AUTH_REQUIRED", "يرجى تسجيل الدخول أولًا")
		}

		jwtSvc := services.NewJWTService()

		claims, err := jwtSvc.ValidateToken(tokenStr)
		if err != nil {
			return utils.UnauthorizedCode(c, "TOKEN_INVALID", "رمز المصادقة غير صالح أو منتهي الصلاحية")
		}

		// Check token blacklist (tokens invalidated by logout).
		if isTokenBlacklisted(tokenStr) {
			return utils.UnauthorizedCode(c, "TOKEN_INVALID", "رمز المصادقة غير صالح أو منتهي الصلاحية")
		}

		if !isAuthVersionAllowed(claims) {
			return utils.UnauthorizedCode(c, "TOKEN_REVOKED", "انتهت صلاحية جلسة تسجيل الدخول")
		}

		// Load user from Redis cache (falls back to DB on miss)
		user, err := loadUserCached(claims.UserID)
		if err != nil {
			return utils.UnauthorizedCode(c, "USER_NOT_FOUND", "المستخدم غير موجود")
		}

		if !user.IsActive() {
			return utils.UnauthorizedCode(c, "ACCOUNT_INACTIVE", "الحساب غير نشط أو محظور")
		}

		c.Locals("user", user)
		c.Locals("user_id", user.ID)
		c.Locals("auth_token", tokenStr)
		// If country_id was not set by FrontendGuard (e.g. mobile client) but the
		// token carries one, use it so downstream handlers always have a country.
		if c.Locals("country_id") == nil && claims.CountryID != 0 {
			c.Locals("country_id", database.CountryID(claims.CountryID))
		}
		return c.Next()
	}
}

// OptionalAuth loads user if token present, but doesn't require it (with Redis caching).
func OptionalAuth() fiber.Handler {
	return func(c *fiber.Ctx) error {
		tokenStr := authTokenFromRequest(c)
		if tokenStr == "" {
			logger.Debug("[OA] no token, passing through", zap.String("path", c.Path()))
			return c.Next()
		}

		jwtSvc := services.NewJWTService()
		claims, err := jwtSvc.ValidateToken(tokenStr)
		if err != nil {
			return c.Next()
		}

		// A revoked token must never restore an optional user session.
		if isTokenBlacklisted(tokenStr) || !isAuthVersionAllowed(claims) {
			return c.Next()
		}

		if user, err := loadUserCached(claims.UserID); err == nil {
			c.Locals("user", user)
			c.Locals("user_id", user.ID)
			c.Locals("auth_token", tokenStr)
		}
		return c.Next()
	}
}

// RequireVerifiedEmail blocks authenticated-but-unverified accounts from protected APIs.
// Keep /auth/user, /auth/logout, /auth/email/resend, and /auth/email/verify public to authenticated users
// so the frontend can show the verification screen and resend links.
func RequireVerifiedEmail() fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*models.User)
		if !ok || user == nil {
			return utils.UnauthorizedCode(c, "AUTH_REQUIRED", "يرجى تسجيل الدخول أولًا")
		}
		if user.IsVerified() || user.IsAdmin() {
			return c.Next()
		}
		return utils.ForbiddenCode(c, "EMAIL_NOT_VERIFIED", "يرجى تفعيل بريدك الإلكتروني قبل استخدام هذه الخدمة")
	}
}

// Can is a permission-based authorization middleware
func Can(permission string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*models.User)
		if !ok || user == nil {
			return utils.UnauthorizedCode(c, "AUTH_REQUIRED", "يرجى تسجيل الدخول أولًا")
		}

		if !user.HasPermission(permission) && !user.IsAdmin() {
			return utils.Forbidden(c)
		}

		return c.Next()
	}
}

// HasRole checks if the authenticated user has a specific role
func HasRole(role string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*models.User)
		if !ok || user == nil {
			return utils.UnauthorizedCode(c, "AUTH_REQUIRED", "يرجى تسجيل الدخول أولًا")
		}

		if !user.HasRole(role) && !user.IsAdmin() {
			return utils.Forbidden(c)
		}

		return c.Next()
	}
}

// AdminOnly ensures only admins can access the route
func AdminOnly() fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*models.User)
		if !ok || user == nil {
			return utils.UnauthorizedCode(c, "AUTH_REQUIRED", "يرجى تسجيل الدخول أولًا")
		}

		if !user.IsAdmin() {
			return utils.Forbidden(c)
		}

		return c.Next()
	}
}

// GetUser retrieves the authenticated user from context
func GetUser(c *fiber.Ctx) *models.User {
	user, _ := c.Locals("user").(*models.User)
	return user
}

// CanAny allows access when the user has any of the provided permissions or is admin.
func CanAny(permissions ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*models.User)
		if !ok || user == nil {
			return utils.UnauthorizedCode(c, "AUTH_REQUIRED", "يرجى تسجيل الدخول أولًا")
		}
		if user.IsAdmin() {
			return c.Next()
		}
		for _, permission := range permissions {
			if user.HasPermission(permission) {
				return c.Next()
			}
		}
		return utils.Forbidden(c)
	}
}
