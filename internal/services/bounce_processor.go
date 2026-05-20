package services

import (
	"strings"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/pkg/logger"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// BounceType values stored in email_bounce_status and email_bounce_events.bounce_type.
const (
	BounceTypeHard    = "hard_bounce"
	BounceTypeSoft    = "soft_bounce"
	BounceTypeUnknown = "unknown"

	// softBounceBlockThreshold: how many soft bounces before we set email_bounce_status = soft_bounce.
	softBounceBlockThreshold = 3
)

// hardBounceSignals are substrings that — when found in a DSN diagnostic code —
// confirm the address is permanently unreachable.
var hardBounceSignals = []string{
	"550 5.1.1",
	"5.1.1",
	"nosuchuser",
	"no such user",
	"mailbox not found",
	"mailbox unavailable",
	"user unknown",
	"recipient address rejected",
	"account does not exist",
	"does not exist",
	"invalid recipient",
	"address rejected",
	"no mailbox here",
}

// softBounceSignals are substrings that indicate a temporary delivery failure.
var softBounceSignals = []string{
	"mailbox full",
	"over quota",
	"insufficient storage",
	"temporary failure",
	"try again later",
	"temporarily unavailable",
	"rate limited",
	"greylisted",
	"service unavailable",
	"421",
	"450",
	"451",
	"452",
}

// ClassifyBounce inspects a diagnostic-code string and returns the bounce type.
// Unknown is returned only when no known signal is detected — callers must NOT
// automatically promote Unknown to hard_bounce.
func ClassifyBounce(diagnosticCode string) string {
	lower := strings.ToLower(diagnosticCode)
	for _, sig := range hardBounceSignals {
		if strings.Contains(lower, sig) {
			return BounceTypeHard
		}
	}
	for _, sig := range softBounceSignals {
		if strings.Contains(lower, sig) {
			return BounceTypeSoft
		}
	}
	return BounceTypeUnknown
}

// ProcessBounceInput is the data extracted from a single DSN message.
type ProcessBounceInput struct {
	RecipientEmail string
	SmtpStatus     string
	DiagnosticCode string
	// MessageID is the RFC 2822 Message-ID from the bounce envelope — used for deduplication.
	MessageID  string
	RawMessage string
}

// BounceProcessorService updates the database when a bounce is received.
type BounceProcessorService struct{}

func NewBounceProcessorService() *BounceProcessorService {
	return &BounceProcessorService{}
}

// Process records a bounce event and updates the affected user on every database shard.
// Rules:
//   - Hard bounce  → email_bounce_status = hard_bounce immediately (no further mail ever)
//   - Soft bounce  → increment count; set status = soft_bounce only after threshold
//   - Unknown      → record event only; do NOT change email_bounce_status
func (s *BounceProcessorService) Process(input ProcessBounceInput) {
	if input.RecipientEmail == "" {
		return
	}

	bounceType := ClassifyBounce(input.DiagnosticCode)

	for _, countryID := range []database.CountryID{
		database.CountryJordan,
		database.CountrySaudi,
		database.CountryEgypt,
		database.CountryPalestine,
	} {
		db := database.GetManager().Get(countryID)

		// ── Deduplication ──────────────────────────────────────────────────────
		// Skip if we already recorded an event with the same MessageID on this shard.
		if input.MessageID != "" {
			var count int64
			db.Model(&models.EmailBounceEvent{}).
				Where("message_id = ?", input.MessageID).
				Count(&count)
			if count > 0 {
				continue
			}
		}

		// ── Record the event ───────────────────────────────────────────────────
		event := models.EmailBounceEvent{
			Email:          input.RecipientEmail,
			BounceType:     bounceType,
			SmtpStatus:     input.SmtpStatus,
			DiagnosticCode: input.DiagnosticCode,
			MessageID:      input.MessageID,
			RawMessage:     input.RawMessage,
		}
		if err := db.Create(&event).Error; err != nil {
			logger.Error("bounce_processor: failed to insert event",
				zap.String("email", input.RecipientEmail),
				zap.String("bounce_type", bounceType),
				zap.Error(err),
			)
		}

		// ── Update user bounce fields ──────────────────────────────────────────
		// Unknown bounces: record the event but leave email_bounce_status untouched.
		if bounceType == BounceTypeUnknown {
			continue
		}

		now := time.Now()
		reason := input.DiagnosticCode

		updates := map[string]interface{}{
			"email_bounce_count":    gorm.Expr("email_bounce_count + 1"),
			"email_last_bounce_at":  now,
			"email_bounce_reason":   reason,
		}

		if bounceType == BounceTypeHard {
			// Hard bounce: block immediately.
			updates["email_bounce_status"] = BounceTypeHard
		} else {
			// Soft bounce: block only after reaching the threshold.
			var user models.User
			db.Select("email_bounce_count").
				Where("email = ?", input.RecipientEmail).
				First(&user)
			if user.EmailBounceCount+1 >= softBounceBlockThreshold {
				updates["email_bounce_status"] = BounceTypeSoft
			}
		}

		if res := db.Model(&models.User{}).
			Where("email = ?", input.RecipientEmail).
			Updates(updates); res.Error != nil {
			logger.Error("bounce_processor: failed to update user",
				zap.String("email", input.RecipientEmail),
				zap.Error(res.Error),
			)
		}
	}

	logger.Info("bounce_processor: processed",
		zap.String("email", input.RecipientEmail),
		zap.String("bounce_type", bounceType),
	)
}

// CanSendToEmail checks the Jordan shard (primary) and returns false when the
// address is permanently blocked.  Unknown/active addresses are permitted.
func CanSendToEmail(email string) (bool, string) {
	db := database.GetManager().Get(database.CountryJordan)
	var user models.User
	if err := db.Select("email_bounce_status").
		Where("email = ?", email).
		First(&user).Error; err != nil {
		return true, ""
	}
	if !user.CanReceiveEmail() {
		return false, user.EmailBounceStatus
	}
	return true, ""
}
