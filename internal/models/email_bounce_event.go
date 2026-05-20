package models

import "time"

// EmailBounceEvent records every bounce notification received for an email address.
// BounceType values: hard_bounce | soft_bounce | unknown
type EmailBounceEvent struct {
	ID             uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Email          string    `gorm:"type:varchar(255);not null;index:idx_email_bounce_email" json:"email"`
	BounceType     string    `gorm:"type:varchar(30);not null;index:idx_email_bounce_type" json:"bounce_type"`
	SmtpStatus     string    `gorm:"type:varchar(30)" json:"smtp_status"`
	DiagnosticCode string    `gorm:"type:text" json:"diagnostic_code"`
	MessageID      string    `gorm:"type:varchar(512);index:idx_email_bounce_msgid" json:"message_id"`
	RawMessage     string    `gorm:"type:mediumtext" json:"-"`
	CreatedAt      time.Time `json:"created_at"`
}

func (EmailBounceEvent) TableName() string { return "email_bounce_events" }
