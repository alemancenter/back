package models

import "time"

const (
	GSCIndexStatusIndexed              = "indexed"
	GSCIndexStatusNotIndexed           = "not_indexed"
	GSCIndexStatusCrawledNotIndexed    = "crawled_not_indexed"
	GSCIndexStatusDiscoveredNotCrawled = "discovered_not_crawled"
	GSCIndexStatusUnknownNotSynced     = "unknown_not_synced"

	GSCSyncKindURLInspection   = "url_inspection"
	GSCSyncKindSearchAnalytics = "search_analytics"
	GSCSyncKindSitemapPing     = "sitemap_ping"

	GSCSyncStatusRunning   = "running"
	GSCSyncStatusCompleted = "completed"
	GSCSyncStatusFailed    = "failed"

	GSCSyncTriggerManual    = "manual"
	GSCSyncTriggerScheduled = "scheduled"
)

// GSCProperty maps one country/domain to its Google Search Console property.
// Search Console properties are per-domain, and this site serves several
// countries on separate domains, so this mapping is per-country rather than a
// single global site URL — see
// back/docs/reports/CONTENT_QUALITY_GOVERNANCE_CENTER_PLAN.md §4.1.
type GSCProperty struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	CountryCode string     `gorm:"type:varchar(10);not null;uniqueIndex" json:"country_code"`
	SiteURL     string     `gorm:"type:varchar(255);not null" json:"site_url"` // e.g. "sc-domain:alemancenter.com" or "https://imanjo.com/"
	Active      bool       `gorm:"not null;default:true" json:"active"`
	VerifiedAt  *time.Time `json:"verified_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (GSCProperty) TableName() string { return "gsc_properties" }

// GSCURLStatus is Google's actual index status for one URL, sourced only from
// the URL Inspection API. This is a separate badge from the internal readiness
// state — it is deliberately never read by contentquality.Evaluate/Gate, the
// same rule already applied to ContentPolicyReadiness (see
// models/content_audit.go). Internal readiness answers "did we pass the checks
// we can verify"; this table answers "what does Google actually show" — the two
// must never be merged into one flag.
type GSCURLStatus struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	ContentType     string     `gorm:"type:varchar(30);not null;uniqueIndex:idx_gsc_url_status_item" json:"content_type"`
	ContentID       uint       `gorm:"not null;uniqueIndex:idx_gsc_url_status_item" json:"content_id"`
	CountryCode     string     `gorm:"type:varchar(10);not null;uniqueIndex:idx_gsc_url_status_item" json:"country_code"`
	URL             string     `gorm:"type:varchar(1000);not null" json:"url"`
	IndexStatus     string     `gorm:"type:varchar(30);not null;index" json:"index_status"`
	CoverageVerdict string     `gorm:"type:varchar(20)" json:"coverage_verdict,omitempty"` // raw URL Inspection verdict: PASS/FAIL/NEUTRAL/PARTIAL
	GoogleCanonical string     `gorm:"type:varchar(1000)" json:"google_canonical,omitempty"`
	UserCanonical   string     `gorm:"type:varchar(1000)" json:"user_canonical,omitempty"`
	RobotsTxtState  string     `gorm:"type:varchar(30)" json:"robots_txt_state,omitempty"`
	LastCrawlTime   *time.Time `json:"last_crawl_time,omitempty"`
	RawResponse     string     `gorm:"type:text" json:"raw_response,omitempty"`
	CheckedAt       time.Time  `gorm:"not null;index" json:"checked_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (GSCURLStatus) TableName() string { return "gsc_url_status" }

// GSCSearchAnalyticsDaily is one (url, date) row of Search Analytics performance
// data (clicks/impressions/position) — see plan §4.2 and §10's success metric
// "نمو الانطباعات والنقرات بعد التحسين".
type GSCSearchAnalyticsDaily struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	CountryCode string    `gorm:"type:varchar(10);not null;uniqueIndex:idx_gsc_analytics_row" json:"country_code"`
	URL         string    `gorm:"type:varchar(1000);not null;uniqueIndex:idx_gsc_analytics_row" json:"url"`
	Date        time.Time `gorm:"type:date;not null;uniqueIndex:idx_gsc_analytics_row;index" json:"date"`
	Clicks      int       `gorm:"not null;default:0" json:"clicks"`
	Impressions int       `gorm:"not null;default:0" json:"impressions"`
	CTR         float64   `json:"ctr"`
	Position    float64   `json:"position"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (GSCSearchAnalyticsDaily) TableName() string { return "gsc_search_analytics_daily" }

// GSCSyncRun records one background sync attempt against the Search Console
// APIs, mirroring the existing PolicyAuditRun status-row pattern
// (models/content_audit.go) instead of introducing a new job abstraction.
type GSCSyncRun struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	CountryCode  string     `gorm:"type:varchar(10);not null;index" json:"country_code"`
	Kind         string     `gorm:"type:varchar(20);not null;index" json:"kind"`
	Status       string     `gorm:"type:varchar(20);not null;index" json:"status"`
	TriggeredBy  string     `gorm:"type:varchar(20);not null" json:"triggered_by"`
	URLsChecked  int        `gorm:"not null;default:0" json:"urls_checked"`
	ErrorMessage *string    `gorm:"type:text" json:"error_message,omitempty"`
	StartedAt    time.Time  `gorm:"not null" json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
}

func (GSCSyncRun) TableName() string { return "gsc_sync_runs" }
