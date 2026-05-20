package models

import "time"

// SecurityLog represents a security event log entry
type SecurityLog struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	IPAddress   string     `gorm:"type:varchar(45);not null;index" json:"ip_address"`
	EventType   string     `gorm:"type:varchar(100);not null" json:"event_type"`
	Description string     `gorm:"type:text" json:"description"`
	UserAgent   *string    `gorm:"type:text" json:"user_agent,omitempty"`
	Route       *string    `gorm:"type:varchar(500)" json:"route,omitempty"`
	Method      *string    `gorm:"type:varchar(10)" json:"method,omitempty"`
	RequestData *string    `gorm:"type:json" json:"request_data,omitempty"`
	RiskScore   int        `gorm:"default:0" json:"risk_score"`
	CountryCode *string    `gorm:"type:varchar(10)" json:"country_code,omitempty"`
	City        *string    `gorm:"type:varchar(100)" json:"city,omitempty"`
	AttackType  *string    `gorm:"type:varchar(100)" json:"attack_type,omitempty"`
	IsBlocked   bool       `gorm:"default:false" json:"is_blocked"`
	IsTrusted   bool       `gorm:"default:false" json:"is_trusted"`
	IsResolved  bool       `gorm:"default:false" json:"is_resolved"`
	Severity    string     `gorm:"type:enum('info','warning','danger','critical');default:'info'" json:"severity"`
	UserID      *uint      `gorm:"index" json:"user_id,omitempty"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (SecurityLog) TableName() string { return "security_logs" }

// EventType constants
const (
	EventLoginFailed       = "login_failed"
	EventSuspicious        = "suspicious_activity"
	EventBlockedAccess     = "blocked_access"
	EventRateLimitExceeded = "rate_limit_exceeded"
	EventSQLInjection      = "sql_injection_attempt"
	EventXSSAttempt        = "xss_attempt"
	EventInvalidToken      = "invalid_token"
	EventUnauthorized      = "unauthorized_access"
)

// Severity constants
const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityDanger   = "danger"
	SeverityCritical = "critical"
)

// BlockedIP represents a blocked IP address
type BlockedIP struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	IPAddress   string     `gorm:"type:varchar(45);unique;not null;index" json:"ip_address"`
	Reason      *string    `gorm:"type:text" json:"reason,omitempty"`
	BlockedBy   *uint      `json:"blocked_by,omitempty"`
	ExpiresAt   *time.Time `gorm:"index" json:"expires_at,omitempty"`
	IsAutoBlock bool       `gorm:"default:false" json:"is_auto_block"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`

	// Computed API fields. They are not stored in the database.
	IsActive      bool   `gorm:"-" json:"is_active"`
	IsExpired     bool   `gorm:"-" json:"is_expired"`
	Status        string `gorm:"-" json:"status"`
	RemainingDays *int   `gorm:"-" json:"remaining_days,omitempty"`
}

func (BlockedIP) TableName() string { return "blocked_ips" }

// IsExpired checks if the block has expired
func (b *BlockedIP) HasExpired() bool {
	if b.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*b.ExpiresAt)
}

// EnrichStatus populates computed fields used by dashboard clients.
func (b *BlockedIP) EnrichStatus(now time.Time) {
	expired := b.HasExpired()
	b.IsExpired = expired
	b.IsActive = !expired
	if expired {
		b.Status = "expired"
	} else {
		b.Status = "active"
	}

	if b.ExpiresAt != nil && !expired {
		days := int(time.Until(*b.ExpiresAt).Hours() / 24)
		if days < 0 {
			days = 0
		}
		b.RemainingDays = &days
	}
}

// LegacyBannedIP represents the old Laravel-compatible banned_ips table.
// It is kept for backward compatibility only; blocked_ips is the canonical table.
type LegacyBannedIP struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	IP          string     `gorm:"column:ip;type:varchar(45);unique;not null" json:"ip"`
	Reason      *string    `gorm:"type:text" json:"reason,omitempty"`
	BannedBy    *uint      `gorm:"column:banned_by" json:"banned_by,omitempty"`
	BannedUntil *time.Time `gorm:"column:banned_until" json:"banned_until,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (LegacyBannedIP) TableName() string { return "banned_ips" }

// ToBlockedIP maps the legacy banned_ips shape to the canonical blocked_ips shape.
func (b LegacyBannedIP) ToBlockedIP() BlockedIP {
	return BlockedIP{
		IPAddress:   b.IP,
		Reason:      b.Reason,
		BlockedBy:   b.BannedBy,
		ExpiresAt:   b.BannedUntil,
		IsAutoBlock: b.BannedBy == nil,
		CreatedAt:   b.CreatedAt,
		UpdatedAt:   b.UpdatedAt,
	}
}

// TrustedIP represents a trusted IP address
type TrustedIP struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	IPAddress string     `gorm:"type:varchar(45);unique;not null" json:"ip_address"`
	Note      *string    `gorm:"type:text" json:"note,omitempty"`
	AddedBy   *uint      `json:"added_by,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func (TrustedIP) TableName() string { return "trusted_ips" }

// ActivityLog represents the Spatie-compatible activity log
type ActivityLog struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	LogName     *string   `gorm:"type:varchar(255)" json:"log_name,omitempty"`
	Description string    `gorm:"type:text;not null" json:"description"`
	SubjectType *string   `gorm:"type:varchar(255)" json:"subject_type,omitempty"`
	SubjectID   *uint     `gorm:"index" json:"subject_id,omitempty"`
	CauserType  *string   `gorm:"type:varchar(255)" json:"causer_type,omitempty"`
	CauserID    *uint     `gorm:"index" json:"causer_id,omitempty"`
	Properties  *string   `gorm:"type:json" json:"properties,omitempty"`
	Event       *string   `gorm:"type:varchar(255)" json:"event,omitempty"`
	BatchUUID   *string   `gorm:"type:char(36)" json:"batch_uuid,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (ActivityLog) TableName() string { return "activity_log" }

// VisitorTracking represents visitor analytics data
type VisitorTracking struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	IPAddress    string    `gorm:"type:varchar(255);not null" json:"ip_address"`
	UserAgent    string    `gorm:"type:text" json:"user_agent"`
	Country      *string   `gorm:"type:varchar(255)" json:"country,omitempty"`
	City         *string   `gorm:"type:varchar(255)" json:"city,omitempty"`
	Browser      *string   `gorm:"type:varchar(255)" json:"browser,omitempty"`
	OS           *string   `gorm:"type:varchar(255)" json:"os,omitempty"`
	URL          *string   `gorm:"type:text" json:"url,omitempty"`
	Referer      *string   `gorm:"type:text" json:"referer,omitempty"`
	Latitude     *float64  `gorm:"type:decimal(10,8)" json:"latitude,omitempty"`
	Longitude    *float64  `gorm:"type:decimal(11,8)" json:"longitude,omitempty"`
	UserID       *uint     `gorm:"index" json:"user_id,omitempty"`
	StatusCode   *int      `json:"status_code,omitempty"`
	LastActivity time.Time `gorm:"not null" json:"last_activity"`
	ResponseTime *float64  `gorm:"type:decimal(8,2)" json:"response_time,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (VisitorTracking) TableName() string { return "visitors_tracking" }

// VisitorSession represents a visitor session
type VisitorSession struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	SessionID string    `gorm:"type:varchar(100);unique;not null" json:"session_id"`
	IPAddress string    `gorm:"type:varchar(45)" json:"ip_address"`
	UserID    *uint     `gorm:"index" json:"user_id,omitempty"`
	LastSeen  time.Time `json:"last_seen"`
	Database  string    `gorm:"type:varchar(10);default:'jo'" json:"database"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (VisitorSession) TableName() string { return "visitor_sessions" }
