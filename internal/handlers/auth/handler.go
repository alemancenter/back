package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/alemancenter/fiber-api/internal/config"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
	"golang.org/x/oauth2"
)

// RegisterRequest contains fields for user registration
type RegisterRequest struct {
	Name                 string `json:"name" validate:"required,min=2,max=255"`
	Email                string `json:"email" validate:"required,email"`
	Password             string `json:"password" validate:"required,min=8"`
	PasswordConfirmation string `json:"password_confirmation" validate:"required"`
}

// LoginRequest contains fields for user login
type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

type EmailPreflightRequest struct {
	Email string `json:"email" validate:"required,email"`
}

type ChangeEmailRequest struct {
	Email string `json:"email" validate:"required,email"`
}

// ForgotPasswordRequest contains the email for password reset
type ForgotPasswordRequest struct {
	Email string `json:"email" validate:"required,email"`
}

// ResetPasswordRequest contains fields to reset the password
type ResetPasswordRequest struct {
	Token                string `json:"token" validate:"required"`
	Email                string `json:"email" validate:"required,email"`
	Password             string `json:"password" validate:"required,min=8"`
	PasswordConfirmation string `json:"password_confirmation" validate:"required"`
}

// Handler contains auth route handlers
type Handler struct {
	svc services.AuthService
	cfg *config.Config
}

// New creates a new auth Handler
func New(svc services.AuthService) *Handler {
	return &Handler{
		svc: svc,
		cfg: config.Get(),
	}
}

// CheckEmail checks whether an email address is available for registration.
func (h *Handler) CheckEmail(c *fiber.Ctx) error {
	var req struct {
		Email string `json:"email" validate:"required,email"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.JSON(fiber.Map{"available": false})
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if errs := utils.Validate(req); errs != nil {
		return c.JSON(fiber.Map{"available": false})
	}
	available, err := h.svc.CheckEmailAvailable(req.Email)
	if err != nil {
		return c.JSON(fiber.Map{"available": false})
	}
	return c.JSON(fiber.Map{"available": available})
}

func (h *Handler) EmailPreflight(c *fiber.Ctx) error {
	var req EmailPreflightRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "invalid request")
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if errs := utils.Validate(req); errs != nil {
		return utils.ValidationError(c, errs)
	}
	result, err := h.svc.CheckEmailPreflight(req.Email)
	if err != nil {
		if err == services.ErrEmailNotDeliverable {
			return utils.BadRequest(c, "email cannot receive verification messages")
		}
		if err == services.ErrVerificationRateLimited {
			return utils.TooManyRequests(c)
		}
		return utils.InternalError(c)
	}
	return utils.Success(c, "email checked", result)
}

// Register handles user registration
// @Summary Register User
// @Description Register a new user account
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body RegisterRequest true "Registration data"
// @Success 201 {object} services.RegisterResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 422 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /auth/register [post]
func (h *Handler) Register(c *fiber.Ctx) error {
	var req RegisterRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	// Sanitize inputs
	req.Name = utils.SanitizeInput(req.Name)
	req.Email = strings.ToLower(strings.TrimSpace(utils.SanitizeInput(req.Email)))

	// Validate
	if errs := utils.Validate(req); errs != nil {
		return utils.ValidationError(c, errs)
	}

	if req.Password != req.PasswordConfirmation {
		return utils.ValidationError(c, map[string]string{
			"password_confirmation": "كلمة المرور غير متطابقة",
		})
	}

	user, token, verificationSent, err := h.svc.Register(req.Name, req.Email, req.Password)
	if err != nil {
		if err == services.ErrEmailAlreadyExists {
			return utils.ValidationError(c, map[string]string{
				"email": "البريد الإلكتروني مستخدم بالفعل",
			})
		}
		if err == services.ErrEmailNotDeliverable {
			return utils.ValidationError(c, map[string]string{
				"email": "email cannot receive verification messages",
			})
		}
		if err == services.ErrVerificationEmailFailed {
			return utils.InternalError(c, "تم إنشاء الحساب، لكن تعذر إرسال رسالة التفعيل. يرجى المحاولة لاحقًا أو طلب إعادة الإرسال.")
		}
		return utils.InternalError(c, "فشل إنشاء الحساب")
	}

	return c.Status(fiber.StatusCreated).JSON(services.RegisterResponse{
		Success:               true,
		Message:               "تم إنشاء الحساب بنجاح. يرجى التحقق من بريدك الإلكتروني.",
		Token:                 token,
		User:                  buildUserResponse(user, h.cfg.Storage.URL),
		VerificationEmailSent: verificationSent,
	})
}

// Login handles user authentication
// @Summary Login User
// @Description Authenticate user and return JWT token
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body LoginRequest true "Login credentials"
// @Success 200 {object} utils.APIResponse{data=map[string]interface{}}
// @Failure 400 {object} utils.APIResponse
// @Failure 401 {object} utils.APIResponse
// @Failure 422 {object} utils.APIResponse
// @Router /auth/login [post]
func (h *Handler) Login(c *fiber.Ctx) error {
	var req LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	if errs := utils.Validate(req); errs != nil {
		return utils.ValidationError(c, errs)
	}

	user, token, err := h.svc.Login(
		req.Email,
		req.Password,
		utils.GetClientIP(c),
		c.Get("User-Agent"),
		c.Method(),
		c.Path(),
	)

	if err != nil {
		if err == services.ErrInvalidCredentials || err == services.ErrAccountInactive {
			return utils.Unauthorized(c, err.Error())
		}
		return utils.InternalError(c)
	}

	refreshToken, _ := h.svc.GenerateRefreshTokenForUser(user.ID, user.Email)

	return c.JSON(fiber.Map{
		"success":       true,
		"message":       "تم تسجيل الدخول بنجاح",
		"token":         token,
		"refresh_token": refreshToken,
		"data":          buildUserResponse(user, h.cfg.Storage.URL),
	})
}

// RefreshRequest contains the refresh token (optional in body — also accepted from cookie)
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// RefreshToken issues a new access token from a valid refresh token
// @Summary Refresh Token
// @Description Issue a new JWT access token using a refresh token
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body RefreshRequest true "Refresh Token"
// @Success 200 {object} utils.APIResponse{data=map[string]string}
// @Failure 400 {object} utils.APIResponse
// @Failure 401 {object} utils.APIResponse
// @Router /auth/refresh [post]
func (h *Handler) RefreshToken(c *fiber.Ctx) error {
	// 1. Try JSON body
	var req RefreshRequest
	_ = c.BodyParser(&req)

	// 2. Fall back to HttpOnly cookie (sent automatically when frontend uses credentials:include)
	refreshTokenStr := strings.TrimSpace(req.RefreshToken)
	if refreshTokenStr == "" {
		refreshTokenStr = strings.TrimSpace(c.Cookies("refresh_token"))
	}

	if refreshTokenStr == "" {
		return utils.BadRequest(c, "رمز التحديث مطلوب")
	}

	accessToken, newRefresh, err := h.svc.RefreshToken(refreshTokenStr)
	if err != nil {
		return utils.Unauthorized(c, "رمز التحديث غير صالح أو منتهي الصلاحية")
	}

	return utils.Success(c, "تم تجديد الرمز بنجاح", fiber.Map{
		"token":         accessToken,
		"refresh_token": newRefresh,
	})
}

// Logout invalidates the current token
// @Summary Logout User
// @Description Invalidate the current JWT token
// @Tags Auth
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Success 200 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /auth/logout [post]
func (h *Handler) Logout(c *fiber.Ctx) error {
	authHeader := c.Get("Authorization")
	var tokenStr string
	if token, ok := c.Locals("auth_token").(string); ok {
		tokenStr = token
	} else if len(authHeader) > 7 {
		tokenStr = authHeader[7:]
	} else {
		tokenStr = c.Cookies("token")
	}

	var user *models.User
	if u, ok := c.Locals("user").(*models.User); ok && u != nil {
		user = u
	}

	if err := h.svc.Logout(tokenStr, user); err != nil {
		return utils.InternalError(c, "فشل تسجيل الخروج")
	}

	return utils.Success(c, "تم تسجيل الخروج بنجاح", nil)
}

// Me returns the current authenticated user
// @Summary Get Current User
// @Description Returns the profile data of the currently authenticated user
// @Tags Auth
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Success 200 {object} utils.APIResponse{data=map[string]interface{}}
// @Failure 401 {object} utils.APIResponse
// @Router /auth/user [get]
func (h *Handler) Me(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	return utils.Success(c, "success", buildUserResponse(user, h.cfg.Storage.URL))
}

// UpdateProfile updates the authenticated user's profile
// @Summary Update User Profile
// @Description Updates the profile details (name, bio, job title, photo) of the authenticated user
// @Tags Auth
// @Accept json
// @Accept mpfd
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param request body services.UpdateProfileInput false "Profile Data"
// @Success 200 {object} utils.APIResponse{data=map[string]interface{}}
// @Failure 400 {object} utils.APIResponse
// @Failure 401 {object} utils.APIResponse
// @Router /auth/profile [put]
func (h *Handler) UpdateProfile(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)

	var req services.UpdateProfileInput
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	if errs := utils.Validate(req); errs != nil {
		return utils.ValidationError(c, errs)
	}

	if req.Name != "" {
		req.Name = utils.SanitizeInput(req.Name)
	}
	if req.JobTitle != nil && *req.JobTitle != "" {
		sanitized := utils.SanitizeInput(*req.JobTitle)
		req.JobTitle = &sanitized
	}
	if req.Bio != nil && *req.Bio != "" {
		sanitized := utils.SanitizeInput(*req.Bio)
		req.Bio = &sanitized
	}

	normalizeProfileSocialLinks(c, &req)

	// Handle profile photo upload
	if photo, err := c.FormFile("profile_photo"); err == nil {
		fileRepo := repositories.NewFileRepository()
		fileSvc := services.NewFileService(fileRepo)
		uploaded, err := fileSvc.UploadImage(photo, "profiles")
		if err != nil {
			return utils.BadRequest(c, err.Error())
		}
		// Delete old photo
		if user.ProfilePhotoPath != nil {
			fileSvc.Delete(*user.ProfilePhotoPath)
		}
		req.ProfilePhotoPath = &uploaded.Path
	}

	updatedUser, err := h.svc.UpdateProfile(user, &req)
	if err != nil {
		return utils.InternalError(c, "فشل تحديث الملف الشخصي")
	}

	return utils.Success(c, "تم تحديث الملف الشخصي بنجاح", buildUserResponse(updatedUser, h.cfg.Storage.URL))
}

// ForgotPassword initiates password reset
// @Summary Forgot Password
// @Description Initiates the password reset process by sending an email
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body ForgotPasswordRequest true "User email"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Router /auth/password/forgot [post]
func (h *Handler) ForgotPassword(c *fiber.Ctx) error {
	var req ForgotPasswordRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	if errs := utils.Validate(req); errs != nil {
		return utils.ValidationError(c, errs)
	}

	// We don't care about the error, we always return success to prevent enumeration
	_ = h.svc.ForgotPassword(req.Email)

	return utils.Success(c, "إذا كان البريد الإلكتروني مسجلاً، ستتلقى رسالة إعادة التعيين", nil)
}

// ResetPassword processes the password reset
// @Summary Reset Password
// @Description Reset user password using the token sent to their email
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body ResetPasswordRequest true "Reset password data"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 422 {object} utils.APIResponse
// @Router /auth/password/reset [post]
func (h *Handler) ResetPassword(c *fiber.Ctx) error {
	var req ResetPasswordRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	if errs := utils.Validate(req); errs != nil {
		return utils.ValidationError(c, errs)
	}

	if req.Password != req.PasswordConfirmation {
		return utils.ValidationError(c, map[string]string{
			"password_confirmation": "كلمة المرور غير متطابقة",
		})
	}

	err := h.svc.ResetPassword(req.Token, req.Email, req.Password)
	if err != nil {
		if err == services.ErrInvalidResetToken || err == services.ErrInvalidCredentials {
			return utils.BadRequest(c, err.Error())
		}
		return utils.InternalError(c, "فشل تحديث كلمة المرور")
	}

	return utils.Success(c, "تم إعادة تعيين كلمة المرور بنجاح", nil)
}

// VerifyEmail verifies a user's email address
// @Summary Verify Email
// @Description Verifies a user's email address using ID and Hash from email link
// @Tags Auth
// @Produce json
// @Param id path string true "User ID"
// @Param hash path string true "Verification Hash"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Router /auth/email/verify/{id}/{hash} [get]
func (h *Handler) VerifyEmail(c *fiber.Ctx) error {
	id := c.Params("id")
	token := c.Params("hash")

	err := h.svc.VerifyEmail(id, token)
	if err != nil {
		if err == services.ErrAlreadyVerified {
			return utils.Success(c, err.Error(), nil)
		}
		if err == services.ErrInvalidVerifyToken {
			return utils.BadRequest(c, err.Error())
		}
		return utils.NotFound(c)
	}

	return utils.Success(c, "تم التحقق من البريد الإلكتروني بنجاح", nil)
}

// ResendVerification resends the email verification
// @Summary Resend Verification Email
// @Description Resends the verification email to the authenticated user
// @Tags Auth
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /auth/email/resend [post]
func (h *Handler) ResendVerification(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)

	err := h.svc.ResendVerification(user)
	if err != nil {
		if err == services.ErrAlreadyVerified {
			return utils.BadRequest(c, err.Error())
		}
		if err == services.ErrEmailNotDeliverable {
			return utils.BadRequest(c, "email cannot receive verification messages")
		}
		if err == services.ErrVerificationRateLimited {
			return utils.TooManyRequests(c)
		}
		if err == services.ErrVerificationEmailFailed {
			return utils.InternalError(c, "تعذر إرسال رسالة التفعيل. تحقق من إعدادات البريد وحاول مرة أخرى.")
		}
		return utils.InternalError(c)
	}

	return utils.Success(c, "تم إرسال رسالة التحقق", nil)
}

func (h *Handler) ChangeUnverifiedEmail(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	var req ChangeEmailRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "invalid request")
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if errs := utils.Validate(req); errs != nil {
		return utils.ValidationError(c, errs)
	}

	updated, verificationSent, err := h.svc.ChangeUnverifiedEmail(user, req.Email)
	if err != nil {
		if err == services.ErrAlreadyVerified {
			return utils.BadRequest(c, err.Error())
		}
		if err == services.ErrEmailAlreadyExists {
			return utils.ValidationError(c, map[string]string{"email": "email already exists"})
		}
		if err == services.ErrEmailNotDeliverable {
			return utils.ValidationError(c, map[string]string{"email": "email cannot receive verification messages"})
		}
		return utils.InternalError(c)
	}

	return utils.Success(c, "email updated", fiber.Map{
		"user":                    buildUserResponse(updated, h.cfg.Storage.URL),
		"verification_email_sent": verificationSent,
	})
}

// DeleteRequest contains the password for account deletion
type DeleteRequest struct {
	Password string `json:"password" validate:"required"`
}

// DeleteAccount permanently deletes the user account
// @Summary Delete Account
// @Description Permanently deletes the authenticated user's account (requires password)
// @Tags Auth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param request body DeleteRequest true "User Password"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 401 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /auth/account/delete [post]
func (h *Handler) DeleteAccount(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)

	var req DeleteRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	err := h.svc.DeleteAccount(user, req.Password)
	if err != nil {
		if err == services.ErrInvalidCredentials {
			return utils.Unauthorized(c, err.Error())
		}
		return utils.InternalError(c, "فشل حذف الحساب")
	}

	return utils.Success(c, "تم حذف الحساب بنجاح", nil)
}

// GoogleRedirect redirects to Google OAuth
// GET /api/auth/google/redirect
func (h *Handler) GoogleRedirect(c *fiber.Ctx) error {
	oauthCfg := h.svc.GetGoogleOAuthConfig()
	url := oauthCfg.AuthCodeURL("state", oauth2.AccessTypeOffline)
	return c.Redirect(url)
}

// GoogleCallback handles the Google OAuth callback
// GET /api/auth/google/callback
func (h *Handler) GoogleCallback(c *fiber.Ctx) error {
	frontendURL := strings.TrimRight(h.cfg.Frontend.URL, "/")
	callbackBase := frontendURL + "/auth/google/callback"

	code := c.Query("code")
	if code == "" {
		return c.Redirect(callbackBase + "?error=missing_code")
	}

	_, userInfo, err := h.exchangeGoogleCode(code)
	if err != nil {
		return c.Redirect(callbackBase + "?error=google_auth_failed")
	}

	user, token, err := h.svc.LoginOrRegisterGoogleUser(userInfo)
	if err != nil || user == nil {
		return c.Redirect(callbackBase + "?error=login_failed")
	}

	return c.Redirect(callbackBase + "?token=" + token)
}

// TokenRequest contains the Google OAuth token
type TokenRequest struct {
	Token string `json:"token" validate:"required"`
}

// GoogleTokenLogin handles mobile Google OAuth token
// @Summary Google Login via Token
// @Description Authenticate user using a Google OAuth Token (from mobile apps or frontend)
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body TokenRequest true "Google OAuth Token"
// @Success 200 {object} utils.APIResponse{data=map[string]interface{}}
// @Failure 400 {object} utils.APIResponse
// @Failure 401 {object} utils.APIResponse
// @Router /auth/google/token [post]
func (h *Handler) GoogleTokenLogin(c *fiber.Ctx) error {
	var req TokenRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	userInfo, err := h.verifyGoogleToken(req.Token)
	if err != nil {
		return utils.Unauthorized(c, "رمز Google غير صالح")
	}

	return h.loginOrRegisterGoogleUser(c, userInfo)
}

// FacebookRedirect redirects to Facebook OAuth
// GET /api/auth/facebook/redirect
func (h *Handler) FacebookRedirect(c *fiber.Ctx) error {
	oauthCfg := h.svc.GetFacebookOAuthConfig()
	url := oauthCfg.AuthCodeURL("state")
	return c.Redirect(url)
}

// FacebookCallback handles the Facebook OAuth callback
// GET /api/auth/facebook/callback
func (h *Handler) FacebookCallback(c *fiber.Ctx) error {
	frontendURL := strings.TrimRight(h.cfg.Frontend.URL, "/")
	callbackBase := frontendURL + "/auth/facebook/callback"

	code := c.Query("code")
	if code == "" {
		return c.Redirect(callbackBase + "?error=missing_code")
	}

	userInfo, err := h.exchangeFacebookCode(code)
	if err != nil {
		return c.Redirect(callbackBase + "?error=facebook_auth_failed")
	}

	user, token, err := h.svc.LoginOrRegisterFacebookUser(userInfo)
	if err != nil || user == nil {
		return c.Redirect(callbackBase + "?error=login_failed")
	}

	return c.Redirect(callbackBase + "?token=" + token)
}

// FacebookTokenRequest contains the Facebook OAuth access token
type FacebookTokenRequest struct {
	Token string `json:"token" validate:"required"`
}

// FacebookTokenLogin handles mobile Facebook OAuth token
// POST /api/auth/facebook/token
func (h *Handler) FacebookTokenLogin(c *fiber.Ctx) error {
	var req FacebookTokenRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	userInfo, err := h.verifyFacebookToken(req.Token)
	if err != nil {
		return utils.Unauthorized(c, "رمز Facebook غير صالح")
	}

	return h.loginOrRegisterFacebookUser(c, userInfo)
}

// PushTokenRequest represents the push token registration payload
type PushTokenRequest struct {
	Token    string `json:"token" validate:"required"`
	Platform string `json:"platform" validate:"required,oneof=fcm onesignal apns web"`
}

// RegisterPushToken saves an FCM/OneSignal push token
// @Summary Register Push Token
// @Description Register a device push token for the authenticated user
// @Tags Auth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param request body PushTokenRequest true "Push Token Data"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /auth/push-token [post]
func (h *Handler) RegisterPushToken(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)

	var req PushTokenRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	if errs := utils.Validate(req); errs != nil {
		return utils.ValidationError(c, errs)
	}

	err := h.svc.UpsertPushToken(user.ID, req.Token, req.Platform)
	if err != nil {
		return utils.InternalError(c, "فشل تسجيل رمز الإشعارات")
	}

	return utils.Success(c, "تم تسجيل رمز الإشعارات بنجاح", nil)
}

// DeletePushToken removes a push notification token
// DELETE /api/auth/push-token
func (h *Handler) DeletePushToken(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)

	type DeleteRequest struct {
		Token string `json:"token" validate:"required"`
	}

	var req DeleteRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	err := h.svc.DeletePushToken(user.ID, req.Token)
	if err != nil {
		return utils.InternalError(c, "فشل حذف رمز الإشعارات")
	}

	return utils.Success(c, "تم حذف رمز الإشعارات بنجاح", nil)
}

// --- Helper functions ---

func (h *Handler) exchangeGoogleCode(code string) (*oauth2.Token, *services.GoogleUserInfo, error) {
	ctx := context.Background()
	oauthCfg := h.svc.GetGoogleOAuthConfig()

	token, err := oauthCfg.Exchange(ctx, code)
	if err != nil {
		return nil, nil, err
	}

	client := oauthCfg.Client(ctx, token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	var userInfo services.GoogleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return nil, nil, err
	}

	return token, &userInfo, nil
}

func (h *Handler) verifyGoogleToken(token string) (*services.GoogleUserInfo, error) {
	req, err := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v3/userinfo", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("invalid google token")
	}

	var userInfo services.GoogleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return nil, err
	}

	return &userInfo, nil
}

func (h *Handler) loginOrRegisterGoogleUser(c *fiber.Ctx, info *services.GoogleUserInfo) error {
	user, token, err := h.svc.LoginOrRegisterGoogleUser(info)
	if err != nil {
		return utils.InternalError(c, "فشل معالجة حساب Google")
	}

	return utils.WithToken(c, "تم تسجيل الدخول بنجاح", buildUserResponse(user, h.cfg.Storage.URL), token)
}

func buildUserResponse(user *models.User, storageURL string) *services.UserResponse {
	if user == nil {
		return nil
	}

	var photoURL *string
	if user.ProfilePhotoPath != nil && *user.ProfilePhotoPath != "" {
		var url string
		p := *user.ProfilePhotoPath
		if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
			url = p
		} else {
			url = storageURL + "/" + p
		}
		photoURL = &url
	}

	return &services.UserResponse{
		ID:               user.ID,
		Name:             user.Name,
		Email:            user.Email,
		EmailVerifiedAt:  user.EmailVerifiedAt,
		Phone:            user.Phone,
		JobTitle:         user.JobTitle,
		Gender:           user.Gender,
		Country:          user.Country,
		Bio:              user.Bio,
		SocialLinks:      parseSocialLinks(user.SocialLinks),
		ProfilePhotoURL:  photoURL,
		ProfilePhotoPath: user.ProfilePhotoPath,
		Status:           user.Status,
		Roles:            user.Roles,
		Permissions:      user.Permissions,
		CreatedAt:        user.CreatedAt,
		LastActivity:     user.LastActivity,
	}
}

var socialLinkKeys = []string{"facebook", "twitter", "linkedin", "instagram", "github"}

func normalizeProfileSocialLinks(c *fiber.Ctx, req *services.UpdateProfileInput) {
	if req.SocialLinks != nil {
		return
	}

	if value, ok := formValue(c, "social_links"); ok {
		req.SocialLinks = &value
		return
	}

	links := make(map[string]string, len(socialLinkKeys))
	hasAny := false
	for _, key := range socialLinkKeys {
		if value, ok := formValue(c, "social_links["+key+"]"); ok {
			links[key] = value
			hasAny = true
		}
	}
	if !hasAny {
		return
	}

	data, err := json.Marshal(links)
	if err != nil {
		return
	}
	value := string(data)
	req.SocialLinks = &value
}

func formValue(c *fiber.Ctx, key string) (string, bool) {
	if form, err := c.MultipartForm(); err == nil && form != nil {
		if values, ok := form.Value[key]; ok {
			if len(values) == 0 {
				return "", true
			}
			return values[0], true
		}
	}

	if value := c.Request().PostArgs().Peek(key); value != nil {
		return string(value), true
	}

	return "", false
}

func (h *Handler) exchangeFacebookCode(code string) (*services.FacebookUserInfo, error) {
	ctx := context.Background()
	oauthCfg := h.svc.GetFacebookOAuthConfig()

	token, err := oauthCfg.Exchange(ctx, code)
	if err != nil {
		return nil, err
	}

	return h.fetchFacebookUserInfo(token.AccessToken)
}

func (h *Handler) verifyFacebookToken(accessToken string) (*services.FacebookUserInfo, error) {
	return h.fetchFacebookUserInfo(accessToken)
}

func (h *Handler) fetchFacebookUserInfo(accessToken string) (*services.FacebookUserInfo, error) {
	url := "https://graph.facebook.com/me?fields=id,name,email,picture.type(large)&access_token=" + accessToken

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("invalid facebook token")
	}

	var userInfo services.FacebookUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return nil, err
	}

	if userInfo.ID == "" {
		return nil, errors.New("facebook user info missing id")
	}

	return &userInfo, nil
}

func (h *Handler) loginOrRegisterFacebookUser(c *fiber.Ctx, info *services.FacebookUserInfo) error {
	user, token, err := h.svc.LoginOrRegisterFacebookUser(info)
	if err != nil {
		return utils.InternalError(c, "فشل معالجة حساب Facebook")
	}

	return utils.WithToken(c, "تم تسجيل الدخول بنجاح", buildUserResponse(user, h.cfg.Storage.URL), token)
}

func parseSocialLinks(raw *string) map[string]string {
	links := make(map[string]string, len(socialLinkKeys))
	for _, key := range socialLinkKeys {
		links[key] = ""
	}
	if raw == nil || *raw == "" {
		return links
	}

	var parsed map[string]string
	if err := json.Unmarshal([]byte(*raw), &parsed); err != nil {
		return links
	}
	for _, key := range socialLinkKeys {
		if value, ok := parsed[key]; ok {
			links[key] = value
		}
	}
	return links
}
