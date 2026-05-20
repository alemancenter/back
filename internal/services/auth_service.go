package services

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alemancenter/fiber-api/internal/config"
	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
	"github.com/alemancenter/fiber-api/pkg/logger"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"gorm.io/gorm"
)

var (
	ErrEmailAlreadyExists      = errors.New("البريد الإلكتروني مستخدم بالفعل")
	ErrInvalidCredentials      = errors.New("بيانات الاعتماد غير صحيحة")
	ErrAccountInactive         = errors.New("الحساب غير نشط أو محظور")
	ErrInvalidResetToken       = errors.New("رمز إعادة التعيين غير صالح أو منتهي الصلاحية")
	ErrInvalidVerifyToken      = errors.New("رابط التحقق غير صالح أو منتهي الصلاحية")
	ErrAlreadyVerified         = errors.New("البريد الإلكتروني مُحقق بالفعل")
	ErrVerificationEmailFailed = errors.New("تعذر إرسال رسالة تفعيل البريد الإلكتروني")
)

var invalidateCachedUser = InvalidateUserCache
var assignDefaultRole = AssignDefaultRole

var (
	ErrEmailNotDeliverable     = errors.New("email address cannot receive verification messages")
	ErrVerificationRateLimited = errors.New("verification email resend limit exceeded")
)

type GoogleUserInfo struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	VerifiedEmail bool   `json:"verified_email"`
}

type UpdateProfileInput struct {
	Name             string  `json:"name" form:"name"`
	Phone            *string `json:"phone" form:"phone"`
	JobTitle         *string `json:"job_title" form:"job_title"`
	Gender           *string `json:"gender" form:"gender"`
	Country          *string `json:"country" form:"country"`
	Bio              *string `json:"bio" form:"bio"`
	SocialLinks      *string `json:"social_links" form:"social_links"`
	ProfilePhotoPath *string `json:"profile_photo_path" form:"profile_photo_path"`
}

// AuthService handles business logic for authentication
type AuthService interface {
	Register(name, email, password string) (*models.User, string, bool, error)
	Login(email, password, ip, userAgent, method, path string) (*models.User, string, error)
	Logout(tokenStr string, user *models.User) error
	RefreshToken(refreshTokenStr string) (accessToken, newRefreshToken string, err error)
	GenerateRefreshTokenForUser(userID uint, email string) (string, error)
	UpdateProfile(user *models.User, req *UpdateProfileInput) (*models.User, error)
	ForgotPassword(email string) error
	ResetPassword(token, email, newPassword string) error
	VerifyEmail(id string, hash string) error
	ResendVerification(user *models.User) error
	CheckEmailPreflight(email string) (*EmailPreflightResult, error)
	ChangeUnverifiedEmail(user *models.User, email string) (*models.User, bool, error)
	DeleteAccount(user *models.User, password string) error
	GetGoogleOAuthConfig() *oauth2.Config
	LoginOrRegisterGoogleUser(info *GoogleUserInfo) (*models.User, string, error)
	UpsertPushToken(userID uint, token, platform string) error
	DeletePushToken(userID uint, token string) error
	CheckEmailAvailable(email string) (bool, error)
}

type UserResponse struct {
	ID               uint                `json:"id"`
	Name             string              `json:"name"`
	Email            string              `json:"email"`
	EmailVerifiedAt  *time.Time          `json:"email_verified_at"`
	Phone            *string             `json:"phone"`
	JobTitle         *string             `json:"job_title"`
	Gender           *string             `json:"gender"`
	Country          *string             `json:"country"`
	Bio              *string             `json:"bio"`
	SocialLinks      map[string]string   `json:"social_links"`
	ProfilePhotoURL  *string             `json:"profile_photo_url"`
	ProfilePhotoPath *string             `json:"profile_photo_path"`
	Status           string              `json:"status"`
	Roles            []models.Role       `json:"roles"`
	Permissions      []models.Permission `json:"permissions"`
	CreatedAt        time.Time           `json:"created_at"`
	LastActivity     *time.Time          `json:"last_activity"`
}

type RegisterResponse struct {
	Success               bool          `json:"success"`
	Message               string        `json:"message"`
	Token                 string        `json:"token"`
	User                  *UserResponse `json:"user"`
	VerificationEmailSent bool          `json:"verification_email_sent"`
}

type EmailPreflightResult struct {
	Email       string `json:"email"`
	Available   bool   `json:"available"`
	Deliverable bool   `json:"deliverable"`
	CanRegister bool   `json:"can_register"`
	Reason      string `json:"reason,omitempty"`
	Suggestion  string `json:"suggestion,omitempty"`
}

type authService struct {
	repo    repositories.UserRepository
	jwtSvc  *JWTService
	mailSvc *MailService
	cfg     *config.Config
}

// NewAuthService creates a new AuthService
func NewAuthService(repo repositories.UserRepository, jwtSvc *JWTService, mailSvc *MailService) AuthService {
	return &authService{
		repo:    repo,
		jwtSvc:  jwtSvc,
		mailSvc: mailSvc,
		cfg:     config.Get(),
	}
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (s *authService) CheckEmailAvailable(email string) (bool, error) {
	count, err := s.repo.CountByEmail(normalizeEmail(email))
	if err != nil {
		return false, MapError(err)
	}
	return count == 0, nil
}

func (s *authService) CheckEmailPreflight(email string) (*EmailPreflightResult, error) {
	email = normalizeEmail(email)
	result := &EmailPreflightResult{Email: email}

	if email == "" {
		result.Reason = "email_required"
		return result, nil
	}

	available, err := s.CheckEmailAvailable(email)
	if err != nil {
		return nil, err
	}
	result.Available = available
	if !available {
		result.Reason = "already_used"
		result.Suggestion = suggestEmailDomain(email)
		return result, nil
	}

	if isDisposableEmailDomain(email) {
		result.Reason = "disposable_email"
		result.Suggestion = suggestEmailDomain(email)
		return result, nil
	}

	if isTrustedEmailDomain(email) {
		result.Deliverable = true
		result.CanRegister = true
		result.Suggestion = suggestEmailDomain(email)
		return result, nil
	}

	if status, validationErr := validateDeliverableEmail(email); validationErr != nil {
		result.Reason = status
		result.Suggestion = suggestEmailDomain(email)
		return result, nil
	}

	result.Deliverable = true
	result.CanRegister = true
	result.Suggestion = suggestEmailDomain(email)
	return result, nil
}

func (s *authService) Register(name, email, password string) (*models.User, string, bool, error) {
	email = normalizeEmail(email)
	preflight, err := s.CheckEmailPreflight(email)
	if err != nil {
		return nil, "", false, err
	}
	if !preflight.Available {
		return nil, "", false, ErrEmailAlreadyExists
	}
	if !preflight.CanRegister {
		return nil, "", false, ErrEmailNotDeliverable
	}

	count, err := s.repo.CountByEmail(email)
	if err != nil {
		return nil, "", false, MapError(err)
	}
	if count > 0 {
		return nil, "", false, ErrEmailAlreadyExists
	}

	user := models.User{
		Name:   name,
		Email:  email,
		Status: "active",
	}

	if err := user.HashPassword(password); err != nil {
		return nil, "", false, MapError(err)
	}

	if err := s.repo.Create(&user); err != nil {
		return nil, "", false, MapError(err)
	}

	assignDefaultRole(user.ID)

	verificationSent := true
	if err := s.sendVerificationEmail(&user); err != nil {
		verificationSent = false
		logger.Error("failed to send verification email during registration",
			zap.Uint("user_id", user.ID),
			zap.String("email", user.Email),
			zap.Error(err),
		)
	}

	token, err := s.jwtSvc.GenerateToken(user.ID, user.Email)
	if err != nil {
		return nil, "", verificationSent, MapError(err)
	}

	return &user, token, verificationSent, nil
}

func (s *authService) Login(email, password, ip, userAgent, method, path string) (*models.User, string, error) {
	user, err := s.repo.FindByEmail(email)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, "", ErrInvalidCredentials
		}
		return nil, "", MapError(err)
	}

	if !user.CheckPassword(password) {
		// Create security service directly since we don't inject it here yet to avoid circular deps
		secRepo := repositories.NewSecurityRepository()
		secSvc := NewSecurityService(secRepo)
		secSvc.LogEvent(
			ip,
			models.EventLoginFailed,
			"فشل تسجيل الدخول للبريد: "+email,
			models.SeverityWarning,
			WithRoute(path),
			WithMethod(method),
			WithUserAgent(userAgent),
		)
		return nil, "", ErrInvalidCredentials
	}

	if !user.IsActive() {
		return nil, "", ErrAccountInactive
	}

	token, err := s.jwtSvc.GenerateToken(user.ID, user.Email)
	if err != nil {
		return nil, "", MapError(err)
	}

	// Update last activity
	now := time.Now()
	user.LastActivity = &now
	_ = s.repo.Update(user)

	return user, token, nil
}

func (s *authService) GenerateRefreshTokenForUser(userID uint, email string) (string, error) {
	return s.jwtSvc.GenerateRefreshToken(userID, email)
}

func (s *authService) Logout(tokenStr string, user *models.User) error {
	rdb := database.Redis()
	ctx := context.Background()

	if tokenStr != "" {
		hash := sha256.Sum256([]byte(tokenStr))
		key := rdb.Key("blacklist", fmt.Sprintf("%x", hash))
		_ = rdb.Set(ctx, key, "1", time.Duration(s.cfg.JWT.ExpireHours)*time.Hour)
	}

	// Evict user cache
	if user != nil {
		_ = rdb.Del(ctx, rdb.Key("user", fmt.Sprintf("%d", user.ID)))
	}

	return nil
}

func (s *authService) RefreshToken(refreshTokenStr string) (string, string, error) {
	claims, err := s.jwtSvc.ValidateRefreshToken(refreshTokenStr)
	if err != nil {
		return "", "", ErrInvalidCredentials
	}

	user, err := s.repo.FindByID(uint64(claims.UserID))
	if err != nil {
		return "", "", ErrInvalidCredentials
	}
	if !user.IsActive() {
		return "", "", ErrAccountInactive
	}

	accessToken, err := s.jwtSvc.GenerateToken(user.ID, user.Email)
	if err != nil {
		return "", "", MapError(err)
	}
	newRefresh, err := s.jwtSvc.GenerateRefreshToken(user.ID, user.Email)
	if err != nil {
		return "", "", MapError(err)
	}
	return accessToken, newRefresh, nil
}

func (s *authService) UpdateProfile(user *models.User, req *UpdateProfileInput) (*models.User, error) {
	updates := make(map[string]interface{})

	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Phone != nil {
		updates["phone"] = *req.Phone
	}
	if req.JobTitle != nil {
		updates["job_title"] = *req.JobTitle
	}
	if req.Gender != nil {
		if *req.Gender == "" {
			updates["gender"] = nil
		} else {
			updates["gender"] = *req.Gender
		}
	}
	if req.Country != nil {
		updates["country"] = *req.Country
	}
	if req.Bio != nil {
		updates["bio"] = *req.Bio
	}
	if req.SocialLinks != nil {
		updates["social_links"] = *req.SocialLinks
	}
	if req.ProfilePhotoPath != nil {
		updates["profile_photo_path"] = *req.ProfilePhotoPath
	}

	if len(updates) > 0 {
		if err := s.repo.UpdateFields(user.ID, updates); err != nil {
			return nil, MapError(err)
		}
		invalidateCachedUser(user.ID)
	}

	// Reload user
	updatedUser, err := s.repo.FindByID(uint64(user.ID))
	if err != nil {
		return nil, MapError(err)
	}

	return updatedUser, nil
}

func (s *authService) ForgotPassword(email string) error {
	user, err := s.repo.FindByEmail(email)
	if err != nil {
		// Return nil even if email not found (prevent enumeration)
		return nil
	}

	// Generate reset token
	// Assuming generateSecureToken is available in utils or similar.
	// For now we'll implement a simple one here if not available globally.
	// We'll use the JWTService as a workaround to get a secure random string or generate it directly.
	token := s.jwtSvc.GenerateRandomString(32) // We will need to add this method to JWTService or use a helper
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))

	// Store in Redis with 60-minute expiry
	rdb := database.Redis()
	ctx := context.Background()
	key := rdb.Key("password_reset", hash)
	_ = rdb.Set(ctx, key, fmt.Sprintf("%d", user.ID), 60*time.Minute)

	// Send reset email
	resetURL := fmt.Sprintf("%s/reset-password?token=%s&email=%s",
		s.cfg.Frontend.URL, token, email)
	go func() {
		if err := s.mailSvc.SendPasswordResetEmail(user.Email, user.Name, resetURL); err != nil {
			logger.Error("failed to send password reset email",
				zap.Uint("user_id", user.ID),
				zap.String("email", user.Email),
				zap.Error(err),
			)
		}
	}()

	return nil
}

func (s *authService) ResetPassword(token, email, newPassword string) error {
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
	rdb := database.Redis()
	ctx := context.Background()
	key := rdb.Key("password_reset", hash)

	userIDStr, err := rdb.Get(ctx, key)
	if err != nil {
		return ErrInvalidResetToken
	}

	user, err := s.repo.FindByEmail(email)
	// Return the same generic token error whether email is wrong or ID mismatches —
	// different errors here allow an attacker to enumerate valid email addresses.
	if err != nil || fmt.Sprintf("%d", user.ID) != userIDStr {
		return ErrInvalidResetToken
	}

	if err := user.HashPassword(newPassword); err != nil {
		return MapError(err)
	}

	if err := s.repo.Update(user); err != nil {
		return MapError(err)
	}

	// Delete used token
	_ = rdb.Del(ctx, key)

	return nil
}

func (s *authService) VerifyEmail(id string, token string) error {
	var userID uint64
	fmt.Sscanf(strings.TrimSpace(id), "%d", &userID)
	if userID == 0 || strings.TrimSpace(token) == "" {
		return ErrInvalidVerifyToken
	}

	user, err := s.repo.FindByID(userID)
	if err != nil {
		return MapError(err)
	}
	if user.IsVerified() {
		return ErrAlreadyVerified
	}

	rdb := database.Redis()
	ctx := context.Background()
	tokenHash := hashVerificationToken(token)
	key := emailVerificationKey(rdb, uint(userID), tokenHash)

	storedUserID, err := rdb.Get(ctx, key)
	if err != nil || storedUserID != fmt.Sprintf("%d", userID) {
		return ErrInvalidVerifyToken
	}

	now := time.Now()
	user.EmailVerifiedAt = &now
	if err := s.repo.Update(user); err != nil {
		return MapError(err)
	}
	invalidateCachedUser(user.ID)
	assignDefaultRole(user.ID)

	_ = rdb.Del(ctx, key)
	_, _ = rdb.DeleteByPattern(ctx, rdb.Key("email_verify", fmt.Sprintf("%d", user.ID), "*"))
	return nil
}

func (s *authService) ResendVerification(user *models.User) error {
	if user.IsVerified() {
		return ErrAlreadyVerified
	}
	if !user.CanReceiveEmail() {
		return ErrEmailNotDeliverable
	}
	if err := s.ensureResendAllowed(user.ID); err != nil {
		return err
	}

	if err := s.sendVerificationEmail(user); err != nil {
		logger.Error("failed to resend verification email",
			zap.Uint("user_id", user.ID),
			zap.String("email", user.Email),
			zap.Error(err),
		)
		return ErrVerificationEmailFailed
	}
	return nil
}

func (s *authService) ChangeUnverifiedEmail(user *models.User, email string) (*models.User, bool, error) {
	if user.IsVerified() {
		return nil, false, ErrAlreadyVerified
	}

	email = normalizeEmail(email)
	if email == "" {
		return nil, false, ErrEmailNotDeliverable
	}

	if email != normalizeEmail(user.Email) {
		preflight, err := s.CheckEmailPreflight(email)
		if err != nil {
			return nil, false, err
		}
		if !preflight.Available {
			return nil, false, ErrEmailAlreadyExists
		}
		if !preflight.CanRegister {
			return nil, false, ErrEmailNotDeliverable
		}

		if err := s.repo.UpdateFields(user.ID, map[string]interface{}{
			"email":                email,
			"email_bounce_status":  "active",
			"email_bounce_count":   0,
			"email_last_bounce_at": nil,
			"email_bounce_reason":  nil,
		}); err != nil {
			return nil, false, MapError(err)
		}
		invalidateCachedUser(user.ID)
	}

	updated, err := s.repo.FindByID(uint64(user.ID))
	if err != nil {
		return nil, false, MapError(err)
	}

	verificationSent := true
	if err := s.sendVerificationEmail(updated); err != nil {
		verificationSent = false
		logger.Error("failed to send verification email after email change",
			zap.Uint("user_id", updated.ID),
			zap.String("email", updated.Email),
			zap.Error(err),
		)
	}

	return updated, verificationSent, nil
}

func (s *authService) DeleteAccount(user *models.User, password string) error {
	storedUser, err := s.repo.FindByID(uint64(user.ID))
	if err != nil {
		return MapError(err)
	}

	if !storedUser.CheckPassword(password) {
		return ErrInvalidCredentials
	}

	return s.repo.Delete(storedUser)
}

func (s *authService) GetGoogleOAuthConfig() *oauth2.Config {
	cfg := s.cfg.Google
	return &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURI,
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     google.Endpoint,
	}
}

func (s *authService) LoginOrRegisterGoogleUser(info *GoogleUserInfo) (*models.User, string, error) {
	user, err := s.repo.FindByEmailOrGoogleID(info.Email, info.ID)

	if err == gorm.ErrRecordNotFound {
		// Register new user
		now := time.Now()
		user = &models.User{
			Name:            info.Name,
			Email:           info.Email,
			GoogleID:        &info.ID,
			EmailVerifiedAt: &now,
			Status:          "active",
		}
		if info.Picture != "" {
			user.ProfilePhotoPath = &info.Picture
		}
		_ = user.HashPassword(s.jwtSvc.GenerateRandomString(32))
		if err := s.repo.Create(user); err != nil {
			return nil, "", MapError(err)
		}
		assignDefaultRole(user.ID)
	} else if err != nil {
		return nil, "", MapError(err)
	} else {
		needsUpdate := false
		if user.GoogleID == nil || *user.GoogleID != info.ID {
			user.GoogleID = &info.ID
			needsUpdate = true
		}
		// Sync Google photo if user has no local photo
		if info.Picture != "" && (user.ProfilePhotoPath == nil || *user.ProfilePhotoPath == "") {
			user.ProfilePhotoPath = &info.Picture
			needsUpdate = true
		}
		if needsUpdate {
			_ = s.repo.Update(user)
		}
	}

	token, err := s.jwtSvc.GenerateToken(user.ID, user.Email)
	if err != nil {
		return nil, "", MapError(err)
	}

	return user, token, nil
}

func (s *authService) UpsertPushToken(userID uint, token, platform string) error {
	pushToken := &models.PushToken{
		UserID:   userID,
		Token:    token,
		Platform: platform,
	}
	return s.repo.UpsertPushToken(pushToken)
}

func (s *authService) DeletePushToken(userID uint, token string) error {
	return s.repo.DeletePushToken(userID, token)
}

// --- Helper functions ---

const emailVerificationTTL = 24 * time.Hour

var typoDomainSuggestions = map[string]string{
	"gamil.com":   "gmail.com",
	"gmial.com":   "gmail.com",
	"gmail.co":    "gmail.com",
	"gmail.con":   "gmail.com",
	"hotmial.com": "hotmail.com",
	"hotmai.com":  "hotmail.com",
	"outlok.com":  "outlook.com",
	"outlook.co":  "outlook.com",
	"yaho.com":    "yahoo.com",
	"yahoo.co":    "yahoo.com",
	"icloud.co":   "icloud.com",
}

var disposableEmailDomains = map[string]bool{
	"10minutemail.com":  true,
	"guerrillamail.com": true,
	"mailinator.com":    true,
	"tempmail.com":      true,
	"temp-mail.org":     true,
	"yopmail.com":       true,
	"throwawaymail.com": true,
	"trashmail.com":     true,
}

var trustedEmailDomains = map[string]bool{
	"gmail.com":      true,
	"googlemail.com": true,
	"hotmail.com":    true,
	"outlook.com":    true,
	"live.com":       true,
	"msn.com":        true,
	"yahoo.com":      true,
	"icloud.com":     true,
	"me.com":         true,
	"proton.me":      true,
	"protonmail.com": true,
}

func hashVerificationToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return fmt.Sprintf("%x", sum)
}

func emailVerificationKey(rdb *database.RedisManager, userID uint, tokenHash string) string {
	return rdb.Key("email_verify", fmt.Sprintf("%d", userID), tokenHash)
}

func emailDomain(email string) string {
	parts := strings.Split(normalizeEmail(email), "@")
	if len(parts) != 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func isDisposableEmailDomain(email string) bool {
	return disposableEmailDomains[emailDomain(email)]
}

func isTrustedEmailDomain(email string) bool {
	return trustedEmailDomains[emailDomain(email)]
}

func suggestEmailDomain(email string) string {
	email = normalizeEmail(email)
	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" {
		return ""
	}
	if suggestion := typoDomainSuggestions[parts[1]]; suggestion != "" {
		return parts[0] + "@" + suggestion
	}
	return ""
}

func (s *authService) ensureResendAllowed(userID uint) error {
	rdb := database.Redis()
	ctx := context.Background()
	minuteKey := rdb.Key("email_verify_resend_minute", fmt.Sprintf("%d", userID))
	if exists, _ := rdb.Exists(ctx, minuteKey); exists {
		return ErrVerificationRateLimited
	}

	hourKey := rdb.Key("email_verify_resend_hour", fmt.Sprintf("%d", userID))
	current, _ := rdb.Get(ctx, hourKey)
	count := 0
	if current != "" {
		_, _ = fmt.Sscanf(current, "%d", &count)
	}
	if count >= 5 {
		return ErrVerificationRateLimited
	}

	_ = rdb.Set(ctx, minuteKey, "1", time.Minute)
	_ = rdb.Set(ctx, hourKey, fmt.Sprintf("%d", count+1), time.Hour)
	return nil
}

func (s *authService) verificationFrontendURL(user *models.User, token string) string {
	frontendURL := strings.TrimRight(strings.TrimSpace(s.cfg.Frontend.URL), "/")
	if frontendURL == "" {
		frontendURL = strings.TrimRight(strings.TrimSpace(s.cfg.App.URL), "/")
	}
	return fmt.Sprintf("%s/verify-email/%d/%s", frontendURL, user.ID, token)
}

func (s *authService) sendVerificationEmail(user *models.User) error {
	rdb := database.Redis()
	ctx := context.Background()

	// Invalidate older verification links for this user before issuing a new one.
	_, _ = rdb.DeleteByPattern(ctx, rdb.Key("email_verify", fmt.Sprintf("%d", user.ID), "*"))

	token := s.jwtSvc.GenerateRandomString(48)
	tokenHash := hashVerificationToken(token)
	key := emailVerificationKey(rdb, user.ID, tokenHash)
	if err := rdb.Set(ctx, key, fmt.Sprintf("%d", user.ID), emailVerificationTTL); err != nil {
		return MapError(err)
	}

	verifyURL := s.verificationFrontendURL(user, token)
	if err := s.mailSvc.SendVerificationEmail(user.Email, user.Name, verifyURL); err != nil {
		_ = rdb.Del(ctx, key)
		return MapError(err)
	}
	return nil
}
