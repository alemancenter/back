package services

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/alemancenter/fiber-api/internal/config"
	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/pkg/logger"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	EmailStatusPending       = "pending"
	EmailStatusDeliverable   = "deliverable"
	EmailStatusInvalidFormat = "invalid_format"
	EmailStatusNoMX          = "no_mx"
	EmailStatusSendFailed    = "send_failed"
	EmailStatusBounced       = "bounced"
	EmailStatusManualInvalid = "manual_invalid"

	defaultReminderLimit         = 3
	defaultReminderIntervalHours = 24
)

type EmailVerificationReminderService interface {
	List(req EmailReminderListRequest) (*EmailReminderListResponse, error)
	Stats() (*EmailReminderStats, error)
	SendReminders(req EmailReminderSendRequest) (*EmailReminderSendResponse, error)
	MarkInvalid(ids []uint, reason string) (int, error)
	ClearStatus(ids []uint) (int, error)
	DeleteUsers(ids []uint, callerID uint) (int, error)
	DeleteFiltered(req EmailReminderListRequest, callerID uint) (int, error)
	SendDueReminders(limit int) (*EmailReminderSendResponse, error)
}

type EmailReminderListRequest struct {
	Search      string
	EmailStatus string
	Only        string
	Page        int
	PerPage     int
}

type EmailReminderUser struct {
	ID                 uint       `json:"id"`
	Name               string     `json:"name"`
	Email              string     `json:"email"`
	Status             string     `json:"status"`
	CreatedAt          time.Time  `json:"created_at"`
	EmailVerifiedAt    *time.Time `json:"email_verified_at,omitempty"`
	EmailStatus        string     `json:"email_status"`
	ReminderCount      int        `json:"reminder_count"`
	LastReminderSentAt *time.Time `json:"last_reminder_sent_at,omitempty"`
	LastCheckedAt      *time.Time `json:"last_checked_at,omitempty"`
	LastError          *string    `json:"last_error,omitempty"`
	RecommendedAction  string     `json:"recommended_action"`
}

type EmailReminderListResponse struct {
	Data       []EmailReminderUser `json:"data"`
	Pagination PaginationMeta      `json:"pagination"`
}

type PaginationMeta struct {
	CurrentPage int   `json:"current_page"`
	LastPage    int   `json:"last_page"`
	PerPage     int   `json:"per_page"`
	Total       int64 `json:"total"`
}

type EmailReminderStats struct {
	Unverified       int64 `json:"unverified"`
	Pending          int64 `json:"pending"`
	Reminder1        int64 `json:"reminder_1"`
	Reminder2        int64 `json:"reminder_2"`
	Reminder3        int64 `json:"reminder_3"`
	Exhausted        int64 `json:"exhausted"`
	Invalid          int64 `json:"invalid"`
	Bounced          int64 `json:"bounced"`
	SendFailed       int64 `json:"send_failed"`
	ReadyForReminder int64 `json:"ready_for_reminder"`
}

type EmailReminderSendRequest struct {
	UserIDs       []uint `json:"user_ids"`
	Limit         int    `json:"limit"`
	Force         bool   `json:"force"`
	MaxReminders  int    `json:"max_reminders"`
	IntervalHours int    `json:"interval_hours"`
}

type EmailReminderSendResponse struct {
	Sent         int      `json:"sent"`
	Skipped      int      `json:"skipped"`
	Failed       int      `json:"failed"`
	Invalid      int      `json:"invalid"`
	ProcessedIDs []uint   `json:"processed_ids"`
	Errors       []string `json:"errors"`
}

type emailVerificationReminderService struct {
	db      *gorm.DB
	mailSvc *MailService
	jwtSvc  *JWTService
	cfg     *config.Config
}

func NewEmailVerificationReminderService(mailSvc *MailService, jwtSvc *JWTService) EmailVerificationReminderService {
	return &emailVerificationReminderService{
		db:      database.DB(),
		mailSvc: mailSvc,
		jwtSvc:  jwtSvc,
		cfg:     config.Get(),
	}
}

func (s *emailVerificationReminderService) List(req EmailReminderListRequest) (*EmailReminderListResponse, error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PerPage < 1 || req.PerPage > 100 {
		req.PerPage = 25
	}

	query := s.listQuery(req)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, MapError(err)
	}

	var rows []EmailReminderUser
	err := query.
		Select(`users.id, users.name, users.email, users.status, users.created_at, users.email_verified_at,
			COALESCE(evr.email_status, ?) AS email_status,
			COALESCE(evr.reminder_count, 0) AS reminder_count,
			evr.last_reminder_sent_at, evr.last_checked_at, evr.last_error`, EmailStatusPending).
		Order("users.created_at DESC").
		Limit(req.PerPage).
		Offset((req.Page - 1) * req.PerPage).
		Scan(&rows).Error
	if err != nil {
		return nil, MapError(err)
	}
	for i := range rows {
		rows[i].RecommendedAction = recommendedEmailAction(rows[i])
	}

	lastPage := int((total + int64(req.PerPage) - 1) / int64(req.PerPage))
	return &EmailReminderListResponse{
		Data: rows,
		Pagination: PaginationMeta{
			CurrentPage: req.Page,
			LastPage:    lastPage,
			PerPage:     req.PerPage,
			Total:       total,
		},
	}, nil
}

func (s *emailVerificationReminderService) Stats() (*EmailReminderStats, error) {
	stats := &EmailReminderStats{}
	base := s.db.Table("users").
		Joins("LEFT JOIN email_verification_reminders evr ON evr.user_id = users.id").
		Where("users.email_verified_at IS NULL")

	count := func(q *gorm.DB) int64 {
		var n int64
		_ = q.Count(&n).Error
		return n
	}

	stats.Unverified = count(base.Session(&gorm.Session{}))
	stats.Pending = count(base.Session(&gorm.Session{}).Where("COALESCE(evr.reminder_count, 0) = 0"))
	stats.Reminder1 = count(base.Session(&gorm.Session{}).Where("COALESCE(evr.reminder_count, 0) = 1"))
	stats.Reminder2 = count(base.Session(&gorm.Session{}).Where("COALESCE(evr.reminder_count, 0) = 2"))
	stats.Reminder3 = count(base.Session(&gorm.Session{}).Where("COALESCE(evr.reminder_count, 0) >= 3"))
	stats.Exhausted = stats.Reminder3
	stats.Invalid = count(base.Session(&gorm.Session{}).Where("COALESCE(evr.email_status, ?) IN ?", EmailStatusPending, []string{EmailStatusInvalidFormat, EmailStatusNoMX, EmailStatusManualInvalid}))
	stats.Bounced = count(base.Session(&gorm.Session{}).Where("COALESCE(evr.email_status, ?) = ?", EmailStatusPending, EmailStatusBounced))
	stats.SendFailed = count(base.Session(&gorm.Session{}).Where("COALESCE(evr.email_status, ?) = ?", EmailStatusPending, EmailStatusSendFailed))
	stats.ReadyForReminder = count(s.readyQuery(defaultReminderLimit, defaultReminderIntervalHours, 500, nil))
	return stats, nil
}

func (s *emailVerificationReminderService) SendReminders(req EmailReminderSendRequest) (*EmailReminderSendResponse, error) {
	maxReminders := req.MaxReminders
	if maxReminders <= 0 {
		maxReminders = defaultReminderLimit
	}
	intervalHours := req.IntervalHours
	if intervalHours <= 0 {
		intervalHours = defaultReminderIntervalHours
	}
	limit := req.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	query := s.readyQuery(maxReminders, intervalHours, limit, req.UserIDs)
	if req.Force {
		query = s.db.Table("users").
			Select("users.*").
			Joins("LEFT JOIN email_verification_reminders evr ON evr.user_id = users.id").
			Where("users.email_verified_at IS NULL").
			Where("users.status NOT IN ?", []string{"inactive", "banned"}).
			Where("COALESCE(evr.email_status, ?) NOT IN ?", EmailStatusPending, terminalEmailStatuses()).
			Limit(limit)
		if len(req.UserIDs) > 0 {
			query = query.Where("users.id IN ?", req.UserIDs)
		}
	}

	var users []models.User
	if err := query.Find(&users).Error; err != nil {
		return nil, MapError(err)
	}
	return s.sendToUsers(users, maxReminders, req.Force)
}

func (s *emailVerificationReminderService) SendDueReminders(limit int) (*EmailReminderSendResponse, error) {
	return s.SendReminders(EmailReminderSendRequest{Limit: limit})
}

func (s *emailVerificationReminderService) MarkInvalid(ids []uint, reason string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "Marked invalid from dashboard"
	}
	now := time.Now()
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if err := s.upsertState(id, map[string]interface{}{
			"email_status":    EmailStatusManualInvalid,
			"last_checked_at": now,
			"last_error":      reason,
		}); err != nil {
			return 0, MapError(err)
		}
	}
	return len(ids), nil
}

func (s *emailVerificationReminderService) ClearStatus(ids []uint) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	now := time.Now()
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if err := s.upsertState(id, map[string]interface{}{
			"email_status":    EmailStatusPending,
			"last_checked_at": now,
			"last_error":      nil,
		}); err != nil {
			return 0, MapError(err)
		}
	}
	return len(ids), nil
}

func (s *emailVerificationReminderService) DeleteUsers(ids []uint, callerID uint) (int, error) {
	filtered := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id > 0 && id != callerID {
			filtered = append(filtered, id)
		}
	}
	if len(filtered) == 0 {
		return 0, nil
	}
	if err := s.db.Where("id IN ? AND email_verified_at IS NULL", filtered).Delete(&models.User{}).Error; err != nil {
		return 0, MapError(err)
	}
	_ = s.db.Where("user_id IN ?", filtered).Delete(&models.EmailVerificationReminder{}).Error
	return len(filtered), nil
}

func (s *emailVerificationReminderService) DeleteFiltered(req EmailReminderListRequest, callerID uint) (int, error) {
	query := s.listQuery(req)
	if callerID > 0 {
		query = query.Where("users.id <> ?", callerID)
	}

	var ids []uint
	if err := query.Select("users.id").Pluck("users.id", &ids).Error; err != nil {
		return 0, MapError(err)
	}
	if len(ids) == 0 {
		return 0, nil
	}

	deleted := 0
	err := s.db.Transaction(func(tx *gorm.DB) error {
		const chunkSize = 500
		for start := 0; start < len(ids); start += chunkSize {
			end := start + chunkSize
			if end > len(ids) {
				end = len(ids)
			}
			chunk := ids[start:end]
			result := tx.Where("id IN ? AND email_verified_at IS NULL", chunk).Delete(&models.User{})
			if result.Error != nil {
				return result.Error
			}
			deleted += int(result.RowsAffected)
			if err := tx.Where("user_id IN ?", chunk).Delete(&models.EmailVerificationReminder{}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, MapError(err)
	}

	return deleted, nil
}

func (s *emailVerificationReminderService) listQuery(req EmailReminderListRequest) *gorm.DB {
	query := s.db.Table("users").
		Joins("LEFT JOIN email_verification_reminders evr ON evr.user_id = users.id").
		Where("users.email_verified_at IS NULL")
	if req.Search != "" {
		like := "%" + strings.TrimSpace(req.Search) + "%"
		query = query.Where("users.name LIKE ? OR users.email LIKE ?", like, like)
	}
	if req.EmailStatus != "" {
		query = query.Where("COALESCE(evr.email_status, ?) = ?", EmailStatusPending, req.EmailStatus)
	}
	switch req.Only {
	case "ready":
		cutoff := time.Now().Add(-defaultReminderIntervalHours * time.Hour)
		query = query.Where("COALESCE(evr.reminder_count, 0) < ?", defaultReminderLimit).
			Where("COALESCE(evr.email_status, ?) NOT IN ?", EmailStatusPending, terminalEmailStatuses()).
			Where("evr.last_reminder_sent_at IS NULL OR evr.last_reminder_sent_at <= ?", cutoff)
	case "exhausted":
		query = query.Where("COALESCE(evr.reminder_count, 0) >= ?", defaultReminderLimit)
	case "invalid":
		query = query.Where("COALESCE(evr.email_status, ?) IN ?", EmailStatusPending, terminalEmailStatuses())
	}
	return query
}

func (s *emailVerificationReminderService) readyQuery(maxReminders, intervalHours, limit int, ids []uint) *gorm.DB {
	cutoff := time.Now().Add(-time.Duration(intervalHours) * time.Hour)
	query := s.db.Table("users").
		Select("users.*").
		Joins("LEFT JOIN email_verification_reminders evr ON evr.user_id = users.id").
		Where("users.email_verified_at IS NULL").
		Where("users.status NOT IN ?", []string{"inactive", "banned"}).
		Where("COALESCE(evr.reminder_count, 0) < ?", maxReminders).
		Where("COALESCE(evr.email_status, ?) NOT IN ?", EmailStatusPending, terminalEmailStatuses()).
		Where("evr.last_reminder_sent_at IS NULL OR evr.last_reminder_sent_at <= ?", cutoff).
		Order("users.created_at ASC").
		Limit(limit)
	if len(ids) > 0 {
		query = query.Where("users.id IN ?", ids)
	}
	return query
}

func (s *emailVerificationReminderService) sendToUsers(users []models.User, maxReminders int, force bool) (*EmailReminderSendResponse, error) {
	res := &EmailReminderSendResponse{
		ProcessedIDs: make([]uint, 0, len(users)),
		Errors:       make([]string, 0),
	}
	for _, user := range users {
		res.ProcessedIDs = append(res.ProcessedIDs, user.ID)

		state, err := s.getOrCreateState(user.ID)
		if err != nil {
			res.Failed++
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", user.Email, err))
			continue
		}
		if !force && state.ReminderCount >= maxReminders {
			res.Skipped++
			continue
		}

		status, validationErr := validateDeliverableEmail(user.Email)
		if validationErr != nil {
			res.Invalid++
			_ = s.upsertState(user.ID, map[string]interface{}{
				"email_status":    status,
				"last_checked_at": time.Now(),
				"last_error":      validationErr.Error(),
			})
			continue
		}

		if err := s.sendVerificationEmail(&user); err != nil {
			res.Failed++
			errMsg := err.Error()
			_ = s.upsertState(user.ID, map[string]interface{}{
				"email_status":    EmailStatusSendFailed,
				"last_checked_at": time.Now(),
				"last_error":      errMsg,
			})
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %s", user.Email, errMsg))
			continue
		}

		now := time.Now()
		err = s.upsertState(user.ID, map[string]interface{}{
			"email_status":          EmailStatusDeliverable,
			"reminder_count":        gorm.Expr("reminder_count + 1"),
			"last_reminder_sent_at": now,
			"last_checked_at":       now,
			"last_error":            nil,
		})
		if err != nil {
			res.Failed++
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", user.Email, err))
			continue
		}
		res.Sent++
	}
	return res, nil
}

func (s *emailVerificationReminderService) getOrCreateState(userID uint) (*models.EmailVerificationReminder, error) {
	var state models.EmailVerificationReminder
	err := s.db.Where("user_id = ?", userID).FirstOrCreate(&state, models.EmailVerificationReminder{
		UserID:      userID,
		EmailStatus: EmailStatusPending,
	}).Error
	if err != nil {
		return nil, MapError(err)
	}
	return &state, nil
}

func (s *emailVerificationReminderService) upsertState(userID uint, updates map[string]interface{}) error {
	if userID == 0 {
		return nil
	}

	result := s.db.Model(&models.EmailVerificationReminder{}).Where("user_id = ?", userID).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}

	initial := &models.EmailVerificationReminder{
		UserID:        userID,
		EmailStatus:   EmailStatusPending,
		ReminderCount: 0,
	}
	if err := s.db.Create(initial).Error; err != nil {
		return err
	}

	return s.db.Model(&models.EmailVerificationReminder{}).Where("user_id = ?", userID).Updates(updates).Error
}

func (s *emailVerificationReminderService) sendVerificationEmail(user *models.User) error {
	rdb := database.Redis()
	ctx := context.Background()
	_, _ = rdb.DeleteByPattern(ctx, rdb.Key("email_verify", fmt.Sprintf("%d", user.ID), "*"))

	token := s.jwtSvc.GenerateRandomString(48)
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	tokenHash := fmt.Sprintf("%x", sum)
	key := rdb.Key("email_verify", fmt.Sprintf("%d", user.ID), tokenHash)
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

func (s *emailVerificationReminderService) verificationFrontendURL(user *models.User, token string) string {
	frontendURL := strings.TrimRight(strings.TrimSpace(s.cfg.Frontend.URL), "/")
	if frontendURL == "" {
		frontendURL = strings.TrimRight(strings.TrimSpace(s.cfg.App.URL), "/")
	}
	return fmt.Sprintf("%s/verify-email/%d/%s", frontendURL, user.ID, token)
}

func validateDeliverableEmail(email string) (string, error) {
	parsed, err := mail.ParseAddress(strings.TrimSpace(email))
	if err != nil || parsed.Address == "" {
		return EmailStatusInvalidFormat, errors.New("invalid email format")
	}
	parts := strings.Split(parsed.Address, "@")
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		return EmailStatusInvalidFormat, errors.New("invalid email domain")
	}
	domain := strings.ToLower(strings.TrimSpace(parts[1]))
	if trustedEmailDomains[domain] {
		return EmailStatusDeliverable, nil
	}
	mx, mxErr := net.LookupMX(domain)
	if mxErr == nil && len(mx) > 0 {
		return EmailStatusDeliverable, nil
	}
	if hosts, hostErr := net.LookupHost(domain); hostErr == nil && len(hosts) > 0 {
		return EmailStatusDeliverable, nil
	}
	return EmailStatusNoMX, fmt.Errorf("email domain has no MX/A record: %s", domain)
}

func terminalEmailStatuses() []string {
	return []string{EmailStatusInvalidFormat, EmailStatusNoMX, EmailStatusManualInvalid, EmailStatusBounced}
}

func recommendedEmailAction(row EmailReminderUser) string {
	switch row.EmailStatus {
	case EmailStatusInvalidFormat, EmailStatusNoMX, EmailStatusManualInvalid, EmailStatusBounced:
		return "review_delete"
	case EmailStatusSendFailed:
		return "review_smtp_or_mark_invalid"
	}
	if row.ReminderCount >= defaultReminderLimit {
		return "review_delete_or_disable"
	}
	return "send_reminder"
}

func StartEmailVerificationReminderScheduler(svc EmailVerificationReminderService) {
	if strings.ToLower(strings.TrimSpace(envOrDefault("EMAIL_VERIFICATION_REMINDER_ENABLED", "false"))) != "true" {
		return
	}
	intervalHours, _ := strconv.Atoi(envOrDefault("EMAIL_VERIFICATION_REMINDER_INTERVAL_HOURS", strconv.Itoa(defaultReminderIntervalHours)))
	if intervalHours <= 0 {
		intervalHours = defaultReminderIntervalHours
	}
	limit, _ := strconv.Atoi(envOrDefault("EMAIL_VERIFICATION_REMINDER_BATCH_LIMIT", "100"))
	if limit <= 0 {
		limit = 100
	}
	go func() {
		ticker := time.NewTicker(time.Duration(intervalHours) * time.Hour)
		defer ticker.Stop()
		for {
			res, err := svc.SendDueReminders(limit)
			if err != nil {
				logger.Warn("email verification reminder scheduler failed", zap.Error(err))
			} else if res.Sent > 0 || res.Failed > 0 || res.Invalid > 0 {
				logger.Info("email verification reminder scheduler finished",
					zap.Int("sent", res.Sent),
					zap.Int("failed", res.Failed),
					zap.Int("invalid", res.Invalid),
				)
			}
			<-ticker.C
		}
	}()
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
