package settings

import (
	"encoding/json"
	"fmt"
	"html"
	"net/mail"
	"regexp"
	"strconv"
	"strings"

	"github.com/alemancenter/fiber-api/internal/config"
	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
)

// smtpRequest holds SMTP settings sent from the dashboard for on-the-fly testing.
type smtpRequest struct {
	Email       string `json:"email"` // destination address for test email (optional)
	Host        string `json:"host"`
	Port        string `json:"port"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	Encryption  string `json:"encryption"`
	FromAddress string `json:"from_address"`
	FromName    string `json:"from_name"`
}

// mailConfigFromRequest converts the dashboard request body into a MailConfig,
// falling back to the global config for any field that is not provided.
func mailConfigFromRequest(req smtpRequest) config.MailConfig {
	base := config.Get().Mail
	if req.Host != "" {
		base.Host = req.Host
	}
	if req.Port != "" {
		if p, err := strconv.Atoi(req.Port); err == nil {
			base.Port = p
		}
	}
	if req.Username != "" {
		base.Username = req.Username
	}
	if req.Password != "" {
		base.Password = req.Password
	}
	if req.Encryption != "" {
		base.Encryption = req.Encryption
	}
	if req.FromAddress != "" {
		base.FromAddress = req.FromAddress
	}
	if req.FromName != "" {
		base.FromName = req.FromName
	}
	return base
}

// envKeyMap maps dashboard setting keys to their .env variable names.
var envKeyMap = map[string]string{
	"mail_host":            "MAIL_HOST",
	"mail_port":            "MAIL_PORT",
	"mail_username":        "MAIL_USERNAME",
	"mail_password":        "MAIL_PASSWORD",
	"mail_encryption":      "MAIL_ENCRYPTION",
	"mail_from_address":    "MAIL_FROM_ADDRESS",
	"mail_from_name":       "MAIL_FROM_NAME",
	"google_client_id":     "GOOGLE_CLIENT_ID",
	"google_client_secret": "GOOGLE_CLIENT_SECRET",
	"google_redirect_uri":  "GOOGLE_REDIRECT_URI",
	// Facebook OAuth
	"facebook_app_id":       "FACEBOOK_APP_ID",
	"facebook_app_secret":   "FACEBOOK_APP_SECRET",
	"facebook_redirect_uri": "FACEBOOK_REDIRECT_URI",
	// Bounce mailbox settings
	"mail_bounce_address":      "MAIL_BOUNCE_ADDRESS",
	"bounce_processor_enabled": "BOUNCE_PROCESSOR_ENABLED",
	"bounce_imap_host":         "BOUNCE_IMAP_HOST",
	"bounce_imap_port":         "BOUNCE_IMAP_PORT",
	"bounce_imap_username":     "BOUNCE_IMAP_USERNAME",
	"bounce_imap_password":     "BOUNCE_IMAP_PASSWORD",
	"bounce_imap_tls":          "BOUNCE_IMAP_TLS",
}

// applyEnvAndConfigUpdates writes changed env-backed settings to .env and syncs in-memory configs.
func applyEnvAndConfigUpdates(updates map[string]string) {
	envUpdates := make(map[string]string)
	for settingKey, envKey := range envKeyMap {
		if v, ok := updates[settingKey]; ok {
			envUpdates[envKey] = v
		}
	}
	if len(envUpdates) == 0 {
		return
	}

	// Update the .env file so the values survive a server restart.
	if err := utils.UpdateEnvFile(".env", envUpdates); err != nil {
		// Non-fatal: in-memory update still applies.
		_ = err
	}

	// Sync in-memory mail config.
	cur := config.Get().Mail
	if v, ok := updates["mail_host"]; ok {
		cur.Host = v
	}
	if v, ok := updates["mail_port"]; ok {
		if p, err := strconv.Atoi(v); err == nil {
			cur.Port = p
		}
	}
	if v, ok := updates["mail_username"]; ok {
		cur.Username = v
	}
	if v, ok := updates["mail_password"]; ok {
		cur.Password = v
	}
	if v, ok := updates["mail_encryption"]; ok {
		cur.Encryption = v
	}
	if v, ok := updates["mail_from_address"]; ok {
		cur.FromAddress = v
	}
	if v, ok := updates["mail_from_name"]; ok {
		cur.FromName = v
	}
	if v, ok := updates["mail_bounce_address"]; ok {
		cur.BounceAddress = v
		config.UpdateBounceAddress(v)
	}
	config.UpdateMailConfig(cur)

	// Sync in-memory bounce mailbox config.
	bCur := config.Get().Mail.Bounce
	changed := false
	if v, ok := updates["bounce_processor_enabled"]; ok {
		bCur.Enabled = v == "true" || v == "1"
		changed = true
	}
	if v, ok := updates["bounce_imap_host"]; ok {
		bCur.Host = v
		changed = true
	}
	if v, ok := updates["bounce_imap_port"]; ok {
		if p, err := strconv.Atoi(v); err == nil {
			bCur.Port = p
		}
		changed = true
	}
	if v, ok := updates["bounce_imap_username"]; ok {
		bCur.Username = v
		changed = true
	}
	if v, ok := updates["bounce_imap_password"]; ok {
		bCur.Password = v
		changed = true
	}
	if v, ok := updates["bounce_imap_tls"]; ok {
		bCur.TLS = v == "true" || v == "1"
		changed = true
	}
	if changed {
		config.UpdateBounceConfig(bCur)
	}

	// Sync in-memory Google config.
	gCur := config.Get().Google
	if v, ok := updates["google_client_id"]; ok {
		gCur.ClientID = v
	}
	if v, ok := updates["google_client_secret"]; ok {
		gCur.ClientSecret = v
	}
	if v, ok := updates["google_redirect_uri"]; ok {
		gCur.RedirectURI = v
	}
	config.UpdateGoogleConfig(gCur)

	// Sync in-memory Facebook config.
	fCur := config.Get().Facebook
	if v, ok := updates["facebook_app_id"]; ok {
		fCur.AppID = v
	}
	if v, ok := updates["facebook_app_secret"]; ok {
		fCur.AppSecret = v
	}
	if v, ok := updates["facebook_redirect_uri"]; ok {
		fCur.RedirectURI = v
	}
	config.UpdateFacebookConfig(fCur)
}

var (
	adsenseClientRe  = regexp.MustCompile(`^ca-pub-\d+$`)
	adAllowedKeys    = map[string]bool{"ad_slot": true, "format": true, "responsive": true}
	adForbiddenWords = []string{"<script", "<iframe", "javascript:", "data:", "vbscript:"}
)

// validateAdUpdates rejects any google_ads_* value that is not empty or a safe
// JSON object containing only {ad_slot, format, responsive}.
func validateAdUpdates(updates map[string]string) error {
	for key, value := range updates {
		if key == "adsense_client" {
			if value != "" && !adsenseClientRe.MatchString(strings.TrimSpace(value)) {
				return fmt.Errorf("adsense_client: invalid format, expected ca-pub-XXXXXXXXXX")
			}
			continue
		}
		if !strings.HasPrefix(key, "google_ads_") {
			continue
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		lower := strings.ToLower(value)
		for _, forbidden := range adForbiddenWords {
			if strings.Contains(lower, forbidden) {
				return fmt.Errorf("%s: forbidden content", key)
			}
		}
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(value), &parsed); err != nil {
			return fmt.Errorf("%s: must be empty or a JSON object {ad_slot, format, responsive}", key)
		}
		for k := range parsed {
			if !adAllowedKeys[k] {
				return fmt.Errorf("%s: unknown field '%s'", key, k)
			}
		}
		slot, ok := parsed["ad_slot"].(string)
		if !ok || strings.TrimSpace(slot) == "" {
			return fmt.Errorf("%s: ad_slot must be a non-empty string", key)
		}
	}
	return nil
}

// Handler contains settings route handlers
type Handler struct {
	svc          services.SettingService
	notification services.NotificationService
}

// New creates a new settings Handler
func New(svc services.SettingService, notification ...services.NotificationService) *Handler {
	h := &Handler{svc: svc}
	if len(notification) > 0 {
		h.notification = notification[0]
	}
	return h
}

func countryIDFromContext(c *fiber.Ctx) database.CountryID {
	if countryID, ok := c.Locals("country_id").(database.CountryID); ok && countryID != 0 {
		return countryID
	}
	return database.CountryJordan
}

// GetAll returns all settings as a flat key→value map for the dashboard.
// @Summary Get Dashboard Settings
// @Description Returns all system settings as a key-value map for the admin dashboard
// @Tags Settings
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Success 200 {object} utils.APIResponse{data=map[string]string}
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/settings [get]
func (h *Handler) GetAll(c *fiber.Ctx) error {
	m, err := h.svc.GetAll(c.Context(), countryIDFromContext(c))
	if err != nil {
		return utils.InternalError(c)
	}
	// Populate mail settings from the current in-memory config for any key missing in DB.
	// This ensures the settings page always displays the actual active values, even before
	// the admin has saved them through the dashboard for the first time.
	mailCfg := config.Get().Mail
	googleCfg := config.Get().Google
	facebookCfg := config.Get().Facebook
	bounceCfg := mailCfg.Bounce
	bounceEnabled := "false"
	if bounceCfg.Enabled {
		bounceEnabled = "true"
	}
	bounceTLS := "true"
	if !bounceCfg.TLS {
		bounceTLS = "false"
	}
	envDefaults := map[string]string{
		"mail_host":            mailCfg.Host,
		"mail_port":            strconv.Itoa(mailCfg.Port),
		"mail_username":        mailCfg.Username,
		"mail_password":        mailCfg.Password,
		"mail_encryption":      mailCfg.Encryption,
		"mail_from_address":    mailCfg.FromAddress,
		"mail_from_name":       mailCfg.FromName,
		"google_client_id":     googleCfg.ClientID,
		"google_client_secret": googleCfg.ClientSecret,
		"google_redirect_uri":  googleCfg.RedirectURI,
		// Facebook OAuth
		"facebook_app_id":       facebookCfg.AppID,
		"facebook_app_secret":   facebookCfg.AppSecret,
		"facebook_redirect_uri": facebookCfg.RedirectURI,
		// Bounce mailbox
		"mail_bounce_address":      mailCfg.BounceAddress,
		"bounce_processor_enabled": bounceEnabled,
		"bounce_imap_host":         bounceCfg.Host,
		"bounce_imap_port":         strconv.Itoa(bounceCfg.Port),
		"bounce_imap_username":     bounceCfg.Username,
		"bounce_imap_password":     bounceCfg.Password,
		"bounce_imap_tls":          bounceTLS,
	}
	for k, v := range envDefaults {
		if existing, ok := m[k]; !ok || existing == "" {
			m[k] = v
		}
	}
	return utils.Success(c, "success", m)
}

// allowedSettingImageKeys lists the setting keys that accept file uploads.
var allowedSettingImageKeys = map[string]bool{
	"site_logo":     true,
	"site_favicon":  true,
	"site_og_image": true,
}

// Update saves settings using a batch upsert (INSERT … ON DUPLICATE KEY UPDATE).
// Accepts both application/json (key/value map) and multipart/form-data (for image uploads).
// @Summary Update Settings
// @Description Update system settings. Supports both JSON and Multipart Form Data for logo/favicon uploads.
// @Tags Settings
// @Accept json
// @Accept mpfd
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param settings body map[string]string false "Settings key-value pairs (if JSON)"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Router /dashboard/settings [post]
func (h *Handler) Update(c *fiber.Ctx) error {
	updates := make(map[string]string)

	ct := string(c.Request().Header.ContentType())
	if strings.Contains(ct, "multipart/form-data") {
		// Parse multipart: handle text fields and image file fields
		form, err := c.MultipartForm()
		if err != nil {
			return utils.BadRequest(c, "بيانات غير صحيحة")
		}
		for key, vals := range form.Value {
			if len(vals) > 0 {
				updates[key] = vals[0]
			}
		}
		fileRepo := repositories.NewFileRepository()
		fileSvc := services.NewFileService(fileRepo)
		for key, files := range form.File {
			if !allowedSettingImageKeys[key] || len(files) == 0 {
				continue
			}
			if uploaded, err := fileSvc.UploadImage(files[0], "settings"); err == nil {
				updates[key] = uploaded.Path
			}
		}
	} else {
		var body map[string]string
		if err := c.BodyParser(&body); err != nil {
			return utils.BadRequest(c, "بيانات غير صحيحة")
		}
		updates = body
	}

	if len(updates) == 0 {
		return utils.BadRequest(c, "لا توجد بيانات للحفظ")
	}

	if err := validateAdUpdates(updates); err != nil {
		return utils.BadRequest(c, err.Error())
	}

	var userID uint
	if user, ok := c.Locals("user").(*models.User); ok && user != nil {
		userID = user.ID
	}

	if err := h.svc.Update(c.Context(), countryIDFromContext(c), updates, userID); err != nil {
		return utils.InternalError(c, "فشل حفظ الإعدادات")
	}

	// Persist env-backed settings to .env and sync in-memory configs immediately.
	applyEnvAndConfigUpdates(updates)

	return utils.Success(c, "تم حفظ الإعدادات بنجاح", updates)
}

// TestSMTP tests the SMTP connection
// @Summary Test SMTP Connection
// @Description Tests the configured SMTP server connection
// @Tags Settings
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Router /dashboard/settings/smtp/test [post]
func (h *Handler) TestSMTP(c *fiber.Ctx) error {
	var req smtpRequest
	_ = c.BodyParser(&req)
	mailSvc := services.NewMailServiceWithConfig(mailConfigFromRequest(req))
	if err := mailSvc.TestSMTP(); err != nil {
		return utils.BadRequest(c, "فشل الاتصال بخادم البريد: "+err.Error())
	}
	return utils.Success(c, "تم الاتصال بخادم البريد بنجاح", nil)
}

// SendTestEmail sends a test email to the current user
// @Summary Send Test Email
// @Description Sends a test email via SMTP to the authenticated user's email address
// @Tags Settings
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Success 200 {object} utils.APIResponse
// @Failure 401 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Router /dashboard/settings/smtp/send-test [post]
func (h *Handler) SendTestEmail(c *fiber.Ctx) error {
	user, _ := c.Locals("user").(*models.User)
	if user == nil {
		return utils.Unauthorized(c)
	}

	var req smtpRequest
	_ = c.BodyParser(&req)
	toEmail := user.Email
	if req.Email != "" {
		toEmail = req.Email
	}
	mailSvc := services.NewMailServiceWithConfig(mailConfigFromRequest(req))
	if err := mailSvc.Send(toEmail, "رسالة اختبار - Alemancenter",
		"<p>هذه رسالة اختبار لإعدادات البريد الإلكتروني.</p>", true); err != nil {
		return utils.BadRequest(c, "فشل إرسال البريد: "+err.Error())
	}

	return utils.Success(c, "تم إرسال رسالة الاختبار بنجاح", nil)
}

// RobotsRequest represents the robots.txt update payload
type RobotsRequest struct {
	Content string `json:"content" validate:"required"`
}

// UpdateRobots updates the robots.txt content
// @Summary Update robots.txt
// @Description Updates the site's robots.txt content
// @Tags Settings
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param body body RobotsRequest true "robots.txt content"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Router /dashboard/settings/robots [post]
func (h *Handler) UpdateRobots(c *fiber.Ctx) error {
	var req RobotsRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	var userID uint
	if user, ok := c.Locals("user").(*models.User); ok && user != nil {
		userID = user.ID
	}

	if err := h.svc.Update(c.Context(), countryIDFromContext(c), map[string]string{"robots_txt": req.Content}, userID); err != nil {
		return utils.InternalError(c, "فشل تحديث ملف robots.txt")
	}

	return utils.Success(c, "تم تحديث ملف robots.txt بنجاح", nil)
}

// GetPublic returns public-facing settings.
// Result is cached in Redis for settingsCacheTTL to avoid a full table scan on every request.
// @Summary Get Public Settings
// @Description Returns public-facing settings required by the frontend application
// @Tags Public
// @Produce json
// @Param X-Country-Id header string false "Country ID"
// @Success 200 {object} utils.APIResponse{data=map[string]string}
// @Router /front/settings [get]
func (h *Handler) GetPublic(c *fiber.Ctx) error {
	result, err := h.svc.GetPublic(c.Context(), countryIDFromContext(c))
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "success", result)
}

// ContactRequest represents the contact form data
type ContactRequest struct {
	Name      string `json:"name"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
	Subject   string `json:"subject"`
	Message   string `json:"message"`
	Recaptcha string `json:"g-recaptcha-response"`
	PageURL   string `json:"page_url"`
	FormTime  int64  `json:"form_time_ms"`
}

// Contact accepts public contact form submissions.
// @Summary Submit Contact Form
// @Description Accepts public contact form submissions and sends an email to the site administrator
// @Tags Public
// @Accept json
// @Produce json
// @Param X-Country-Id header string false "Country ID"
// @Param request body ContactRequest true "Contact form data"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /front/contact [post]
func (h *Handler) Contact(c *fiber.Ctx) error {
	var req ContactRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "invalid contact payload")
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.TrimSpace(req.Email)
	req.Phone = strings.TrimSpace(req.Phone)
	req.Subject = strings.TrimSpace(req.Subject)
	req.Message = strings.TrimSpace(req.Message)
	req.Recaptcha = strings.TrimSpace(req.Recaptcha)
	req.PageURL = strings.TrimSpace(req.PageURL)

	if req.Name == "" || req.Email == "" || req.Subject == "" || req.Message == "" {
		return utils.BadRequest(c, "name, email, subject and message are required")
	}
	if _, err := mail.ParseAddress(req.Email); err != nil {
		return utils.BadRequest(c, "invalid email address")
	}
	if req.Recaptcha == "" {
		return utils.BadRequest(c, "recaptcha token is required")
	}
	if req.FormTime > 0 && req.FormTime < 1200 {
		return utils.BadRequest(c, "contact form submitted too quickly")
	}

	settings, _ := h.svc.GetPublic(c.Context(), countryIDFromContext(c))
	recipient := firstSetting(settings, "contact_email", "site_email")
	if recipient == "" {
		recipient = config.Get().Mail.FromAddress
	}
	if recipient == "" {
		return utils.BadRequest(c, "contact email is not configured")
	}

	body := fmt.Sprintf(`
<div dir="rtl" style="font-family: Arial, sans-serif; line-height: 1.8">
  <h2>Contact form message</h2>
  <p><strong>Name:</strong> %s</p>
  <p><strong>Email:</strong> %s</p>
  <p><strong>Phone:</strong> %s</p>
  <p><strong>Page:</strong> %s</p>
  <hr>
  <p>%s</p>
</div>`,
		html.EscapeString(req.Name),
		html.EscapeString(req.Email),
		html.EscapeString(req.Phone),
		html.EscapeString(req.PageURL),
		html.EscapeString(req.Message),
	)

	subject := "Contact form: " + req.Subject
	if err := services.NewMailService().Send(recipient, subject, body, true); err != nil {
		return utils.InternalError(c, "failed to send contact message")
	}

	// Persist to DB so the dashboard inbox can display it
	contactMsg := &models.ContactMessage{
		Name:    req.Name,
		Email:   req.Email,
		Phone:   req.Phone,
		Subject: req.Subject,
		Message: req.Message,
		PageURL: req.PageURL,
	}
	_ = repositories.NewContactMessageRepository().Create(contactMsg)

	if h.notification != nil {
		go func() {
			_ = h.notification.NotifyUsersWithPermissions(
				"App\\Notifications\\ContactMessageReceived",
				"رسالة تواصل جديدة",
				fmt.Sprintf("رسالة جديدة من %s: %s", req.Name, req.Subject),
				"/dashboard/messages?tab=contact",
				[]string{"manage messages", "manage notifications"},
			)
		}()
	}

	return utils.Success(c, "contact message sent successfully", nil)
}

func firstSetting(settings map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(settings[key]); value != "" {
			return value
		}
	}
	return ""
}
