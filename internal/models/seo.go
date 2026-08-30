package models

import "time"

const (
	SEOContentTypeArticle  = "article"
	SEOContentTypePost     = "post"
	SEOContentTypeCategory = "category"
	SEOContentTypePage     = "page"

	SEORedirectMatchExact  = "exact"
	SEORedirectMatchPrefix = "prefix"
	SEORedirectMatchRegex  = "regex"

	SEOAuditStatusRunning   = "running"
	SEOAuditStatusCompleted = "completed"
	SEOAuditStatusFailed    = "failed"

	SEOIssueSeverityError   = "error"
	SEOIssueSeverityWarning = "warning"
	SEOIssueSeverityNotice  = "notice"
)

// SEOMetadata stores the search/social presentation of one content entity.
// It deliberately lives beside, rather than inside, articles/posts so future
// entity types can use the same workflow without widening every content table.
type SEOMetadata struct {
	ID                 uint      `gorm:"primaryKey" json:"id"`
	ContentType        string    `gorm:"type:varchar(30);not null;uniqueIndex:idx_seo_metadata_entity" json:"content_type"`
	ContentID          uint      `gorm:"not null;uniqueIndex:idx_seo_metadata_entity" json:"content_id"`
	CountryCode        string    `gorm:"type:varchar(10);not null;default:'jo';uniqueIndex:idx_seo_metadata_entity;index" json:"country_code"`
	SEOTitle           string    `gorm:"type:varchar(500)" json:"seo_title"`
	MetaDescription    string    `gorm:"type:varchar(500)" json:"meta_description"`
	FocusKeyword       string    `gorm:"type:varchar(255);index" json:"focus_keyword"`
	AdditionalKeywords string    `gorm:"type:text" json:"additional_keywords"`
	CanonicalURL       string    `gorm:"type:varchar(1000)" json:"canonical_url"`
	RobotsIndex        bool      `gorm:"not null;default:true;index" json:"robots_index"`
	RobotsFollow       bool      `gorm:"not null;default:true" json:"robots_follow"`
	RobotsNoArchive    bool      `gorm:"not null;default:false" json:"robots_noarchive"`
	RobotsNoSnippet    bool      `gorm:"not null;default:false" json:"robots_nosnippet"`
	MaxSnippet         int       `gorm:"not null;default:-1" json:"max_snippet"`
	MaxImagePreview    string    `gorm:"type:varchar(20);not null;default:'large'" json:"max_image_preview"`
	MaxVideoPreview    int       `gorm:"not null;default:-1" json:"max_video_preview"`
	OGTitle            string    `gorm:"type:varchar(500)" json:"og_title"`
	OGDescription      string    `gorm:"type:varchar(500)" json:"og_description"`
	OGImage            string    `gorm:"type:varchar(1000)" json:"og_image"`
	TwitterTitle       string    `gorm:"type:varchar(500)" json:"twitter_title"`
	TwitterDescription string    `gorm:"type:varchar(500)" json:"twitter_description"`
	TwitterImage       string    `gorm:"type:varchar(1000)" json:"twitter_image"`
	SchemaType         string    `gorm:"type:varchar(80);not null;default:'Article'" json:"schema_type"`
	SchemaJSON         string    `gorm:"type:longtext" json:"schema_json"`
	Cornerstone        bool      `gorm:"not null;default:false;index" json:"cornerstone"`
	Score              int       `gorm:"not null;default:0;index" json:"score"`
	AnalysisJSON       string    `gorm:"type:longtext" json:"analysis_json"`
	CreatedBy          *uint     `gorm:"index" json:"created_by,omitempty"`
	UpdatedBy          *uint     `gorm:"index" json:"updated_by,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func (SEOMetadata) TableName() string { return "seo_metadata" }

// SEORevision is an immutable snapshot created for every metadata save or
// restore. Keeping a snapshot rather than a field-level diff makes restoration
// deterministic even when the metadata schema grows later.
type SEORevision struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	MetadataID uint      `gorm:"not null;uniqueIndex:idx_seo_revision_version" json:"metadata_id"`
	Version    int       `gorm:"not null;uniqueIndex:idx_seo_revision_version" json:"version"`
	Snapshot   string    `gorm:"type:longtext;not null" json:"snapshot"`
	ChangeNote string    `gorm:"type:varchar(500)" json:"change_note"`
	ChangedBy  *uint     `gorm:"index" json:"changed_by,omitempty"`
	CreatedAt  time.Time `gorm:"index" json:"created_at"`
}

func (SEORevision) TableName() string { return "seo_revisions" }

// SEORedirect is the database source of truth for redirects. SourceHash keeps
// the uniqueness index small enough for utf8mb4 even when SourcePath is long.
type SEORedirect struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	CountryCode   string     `gorm:"type:varchar(10);not null;default:'jo';uniqueIndex:idx_seo_redirect_source;index" json:"country_code"`
	SourcePath    string     `gorm:"type:varchar(1500);not null" json:"source_path"`
	SourceHash    string     `gorm:"type:char(64);not null;uniqueIndex:idx_seo_redirect_source" json:"-"`
	TargetURL     string     `gorm:"type:varchar(1500)" json:"target_url"`
	StatusCode    int        `gorm:"not null;default:301;index" json:"status_code"`
	MatchType     string     `gorm:"type:varchar(20);not null;default:'exact';uniqueIndex:idx_seo_redirect_source;index" json:"match_type"`
	PreserveQuery bool       `gorm:"not null;default:true" json:"preserve_query"`
	Active        bool       `gorm:"not null;default:true;index" json:"active"`
	HitCount      uint64     `gorm:"not null;default:0" json:"hit_count"`
	LastHitAt     *time.Time `json:"last_hit_at,omitempty"`
	CreatedBy     *uint      `gorm:"index" json:"created_by,omitempty"`
	UpdatedBy     *uint      `gorm:"index" json:"updated_by,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (SEORedirect) TableName() string { return "seo_redirects" }

// SEO404Log aggregates repeated misses without storing visitor IP addresses.
type SEO404Log struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	CountryCode   string    `gorm:"type:varchar(10);not null;default:'jo';uniqueIndex:idx_seo_404_path;index" json:"country_code"`
	Path          string    `gorm:"type:varchar(1500);not null" json:"path"`
	PathHash      string    `gorm:"type:char(64);not null;uniqueIndex:idx_seo_404_path" json:"-"`
	LastQuery     string    `gorm:"type:text" json:"last_query"`
	LastReferrer  string    `gorm:"type:varchar(1500)" json:"last_referrer"`
	LastUserAgent string    `gorm:"type:varchar(500)" json:"last_user_agent"`
	HitCount      uint64    `gorm:"not null;default:1;index" json:"hit_count"`
	Resolved      bool      `gorm:"not null;default:false;index" json:"resolved"`
	RedirectID    *uint     `gorm:"index" json:"redirect_id,omitempty"`
	FirstSeenAt   time.Time `gorm:"not null" json:"first_seen_at"`
	LastSeenAt    time.Time `gorm:"not null;index" json:"last_seen_at"`
}

func (SEO404Log) TableName() string { return "seo_404_logs" }

type SEOAuditRun struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	CountryCode  string     `gorm:"type:varchar(10);not null;default:'jo';index" json:"country_code"`
	Status       string     `gorm:"type:varchar(20);not null;index" json:"status"`
	TotalURLs    int        `gorm:"not null;default:0" json:"total_urls"`
	CheckedURLs  int        `gorm:"not null;default:0" json:"checked_urls"`
	Errors       int        `gorm:"not null;default:0" json:"errors"`
	Warnings     int        `gorm:"not null;default:0" json:"warnings"`
	Notices      int        `gorm:"not null;default:0" json:"notices"`
	TriggeredBy  *uint      `gorm:"index" json:"triggered_by,omitempty"`
	ErrorMessage string     `gorm:"type:text" json:"error_message"`
	StartedAt    time.Time  `gorm:"not null" json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
}

func (SEOAuditRun) TableName() string { return "seo_audit_runs" }

type SEOIssue struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	RunID       uint       `gorm:"not null;index" json:"run_id"`
	CountryCode string     `gorm:"type:varchar(10);not null;default:'jo';index" json:"country_code"`
	ContentType string     `gorm:"type:varchar(30);not null;index" json:"content_type"`
	ContentID   uint       `gorm:"not null;index" json:"content_id"`
	URL         string     `gorm:"type:varchar(1500)" json:"url"`
	Code        string     `gorm:"type:varchar(80);not null;index" json:"code"`
	Severity    string     `gorm:"type:varchar(20);not null;index" json:"severity"`
	Message     string     `gorm:"type:text;not null" json:"message"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (SEOIssue) TableName() string { return "seo_issues" }

// SEOAuthorProfile contains only public authorship/E-E-A-T fields. It is
// stored in the primary Jordan database because users are global identities,
// not duplicated across the per-country content databases.
type SEOAuthorProfile struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	UserID          uint      `gorm:"not null;uniqueIndex" json:"user_id"`
	PublicName      string    `gorm:"type:varchar(255);not null" json:"public_name"`
	Headline        string    `gorm:"type:varchar(500)" json:"headline"`
	Biography       string    `gorm:"type:text" json:"biography"`
	Expertise       string    `gorm:"type:text" json:"expertise"`
	Credentials     string    `gorm:"type:text" json:"credentials"`
	Education       string    `gorm:"type:text" json:"education"`
	Awards          string    `gorm:"type:text" json:"awards"`
	Employer        string    `gorm:"type:varchar(500)" json:"employer"`
	ProfileURL      string    `gorm:"type:varchar(1000)" json:"profile_url"`
	ImageURL        string    `gorm:"type:varchar(1000)" json:"image_url"`
	SocialLinksJSON string    `gorm:"type:longtext" json:"social_links_json"`
	KnowsAboutJSON  string    `gorm:"type:longtext" json:"knows_about_json"`
	Active          bool      `gorm:"not null;default:true;index" json:"active"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (SEOAuthorProfile) TableName() string { return "seo_author_profiles" }

type SEOIndexNowSubmission struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	CountryCode  string     `gorm:"type:varchar(10);not null;default:'jo';index" json:"country_code"`
	URL          string     `gorm:"type:varchar(1500);not null" json:"url"`
	Status       string     `gorm:"type:varchar(20);not null;index" json:"status"`
	HTTPStatus   int        `json:"http_status"`
	ErrorMessage string     `gorm:"type:text" json:"error_message"`
	SubmittedAt  time.Time  `gorm:"not null;index" json:"submitted_at"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
}

func (SEOIndexNowSubmission) TableName() string { return "seo_indexnow_submissions" }
