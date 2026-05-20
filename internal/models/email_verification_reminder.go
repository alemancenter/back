package models

import "time"

// EmailVerificationReminder stores dashboard-managed verification reminder state.
type EmailVerificationReminder struct {
	ID                 uint       `gorm:"primaryKey" json:"id"`
	UserID             uint       `gorm:"uniqueIndex;not null" json:"user_id"`
	EmailStatus        string     `gorm:"type:varchar(50);not null;default:'pending'" json:"email_status"`
	ReminderCount      int        `gorm:"not null;default:0" json:"reminder_count"`
	LastReminderSentAt *time.Time `json:"last_reminder_sent_at,omitempty"`
	LastCheckedAt      *time.Time `json:"last_checked_at,omitempty"`
	LastError          *string    `gorm:"type:text" json:"last_error,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

func (EmailVerificationReminder) TableName() string { return "email_verification_reminders" }
