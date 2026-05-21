package models

import "time"

const (
	PolicyAuditStatusRunning   = "running"
	PolicyAuditStatusCompleted = "completed"
	PolicyAuditStatusFailed    = "failed"

	PolicyAuditTriggerManual    = "manual"
	PolicyAuditTriggerScheduled = "scheduled"
)

type PolicyAuditRun struct {
	ID                uint       `gorm:"primaryKey" json:"id"`
	Status            string     `gorm:"type:varchar(20);not null;index" json:"status"`
	TriggeredBy       string     `gorm:"type:varchar(30);not null;index" json:"triggered_by"`
	TriggeredByUserID *uint      `gorm:"index" json:"triggered_by_user_id,omitempty"`
	StartedAt         time.Time  `gorm:"not null;index" json:"started_at"`
	FinishedAt        *time.Time `json:"finished_at,omitempty"`
	FindingsCount     int        `gorm:"default:0" json:"findings_count"`
	ErrorMessage      *string    `gorm:"type:text" json:"error_message,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`

	Findings []PolicyAuditFinding `gorm:"foreignKey:RunID" json:"findings,omitempty"`
}

func (PolicyAuditRun) TableName() string { return "policy_audit_runs" }

type PolicyAuditFinding struct {
	ID                uint      `gorm:"primaryKey" json:"finding_id"`
	RunID             uint      `gorm:"not null;index" json:"run_id"`
	ContentType       string    `gorm:"column:content_type;type:varchar(30);not null;index" json:"type"`
	ContentID         string    `gorm:"type:varchar(50);not null;index" json:"id"`
	Title             string    `gorm:"type:text" json:"title"`
	Risk              string    `gorm:"type:varchar(80);not null;index" json:"risk"`
	Reason            string    `gorm:"type:text" json:"reason"`
	URL               string    `gorm:"type:varchar(1000)" json:"url"`
	RecommendedAction string    `gorm:"type:text" json:"recommended_action"`
	CreatedAt         time.Time `json:"created_at"`
}

func (PolicyAuditFinding) TableName() string { return "policy_audit_findings" }

const (
	ContentAIJobStatusQueued     = "queued"
	ContentAIJobStatusRunning    = "running"
	ContentAIJobStatusCancelling = "cancelling"
	ContentAIJobStatusCancelled  = "cancelled"
	ContentAIJobStatusCompleted  = "completed"
	ContentAIJobStatusFailed     = "failed"

	ContentAIJobItemStatusPending   = "pending"
	ContentAIJobItemStatusRunning   = "running"
	ContentAIJobItemStatusCompleted = "completed"
	ContentAIJobItemStatusFailed    = "failed"
	ContentAIJobItemStatusCancelled = "cancelled"
)

// ContentAIJob stores long-running content quality batch jobs durably.
// Jobs are preview-first: they can create AI decisions and fix previews,
// but they never apply generated content without human approval.
type ContentAIJob struct {
	ID              string     `gorm:"primaryKey;type:varchar(80)" json:"id"`
	Status          string     `gorm:"type:varchar(30);not null;index" json:"status"`
	Mode            string     `gorm:"type:varchar(40);not null;index" json:"mode"`
	ModelStrategy   string     `gorm:"type:varchar(40);not null;index" json:"model_strategy"`
	CountryCode     string     `gorm:"type:varchar(10);not null;default:'jo';index" json:"country_code"`
	ContentType     string     `gorm:"type:varchar(30);not null;index" json:"content_type"`
	Level           string     `gorm:"type:varchar(30);not null;index" json:"level"`
	Query           string     `gorm:"type:varchar(500)" json:"q,omitempty"`
	Source          string     `gorm:"type:varchar(40);not null;default:'adsense_readiness';index" json:"source"`
	Preset          string     `gorm:"type:varchar(60);not null;default:'weak_first';index" json:"preset"`
	Limit           int        `gorm:"not null;default:50" json:"limit"`
	Concurrency     int        `gorm:"not null;default:2" json:"concurrency"`
	TotalItems      int        `gorm:"not null;default:0" json:"total_items"`
	ProcessedItems  int        `gorm:"not null;default:0" json:"processed_items"`
	SuccessfulItems int        `gorm:"not null;default:0" json:"successful_items"`
	FailedItems     int        `gorm:"not null;default:0" json:"failed_items"`
	PendingItems    int        `gorm:"not null;default:0" json:"pending_items"`
	Progress        int        `gorm:"not null;default:0" json:"progress"`
	CancelRequested bool       `gorm:"not null;default:false" json:"cancel_requested"`
	Error           string     `gorm:"type:text" json:"error,omitempty"`
	CreatedByUserID *uint      `gorm:"index" json:"created_by_user_id,omitempty"`
	CreatedAt       time.Time  `gorm:"index" json:"created_at"`
	StartedAt       *time.Time `gorm:"index" json:"started_at,omitempty"`
	FinishedAt      *time.Time `gorm:"index" json:"finished_at,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at"`

	Items []ContentAIJobItem `gorm:"foreignKey:JobID;references:ID;constraint:OnDelete:CASCADE" json:"items,omitempty"`
}

func (ContentAIJob) TableName() string { return "content_ai_jobs" }

// ContentAIJobItem stores the durable status for one content item in a batch job.
type ContentAIJobItem struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	JobID        string     `gorm:"type:varchar(80);not null;uniqueIndex:idx_content_ai_job_item_order,priority:1;index" json:"job_id"`
	ItemIndex    int        `gorm:"not null;uniqueIndex:idx_content_ai_job_item_order,priority:2" json:"item_index"`
	ContentType  string     `gorm:"type:varchar(30);not null;index:idx_content_ai_job_item_content,priority:1" json:"content_type"`
	ContentID    uint       `gorm:"not null;index:idx_content_ai_job_item_content,priority:2" json:"content_id"`
	CountryCode  string     `gorm:"type:varchar(10);not null;default:'jo';index" json:"country_code"`
	Title        string     `gorm:"type:text" json:"title"`
	URL          string     `gorm:"type:varchar(1000)" json:"url"`
	Status       string     `gorm:"type:varchar(30);not null;index" json:"status"`
	ScoreBefore  int        `gorm:"not null;default:0" json:"score_before"`
	ScoreAfter   int        `gorm:"not null;default:0" json:"score_after,omitempty"`
	DecisionID   *uint      `gorm:"index" json:"decision_id,omitempty"`
	FixPreviewID *uint      `gorm:"index" json:"fix_preview_id,omitempty"`
	Message      string     `gorm:"type:text" json:"message,omitempty"`
	Error        string     `gorm:"type:text" json:"error,omitempty"`
	StartedAt    *time.Time `gorm:"index" json:"started_at,omitempty"`
	FinishedAt   *time.Time `gorm:"index" json:"finished_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (ContentAIJobItem) TableName() string { return "content_ai_job_items" }

// ContentAIModelRun is prepared for cost and reliability tracking per model call.
// The router can populate it in the next phase without changing the public API.
type ContentAIModelRun struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	JobID            string    `gorm:"type:varchar(80);index" json:"job_id,omitempty"`
	JobItemID        *uint     `gorm:"index" json:"job_item_id,omitempty"`
	ContentType      string    `gorm:"type:varchar(30);index" json:"content_type,omitempty"`
	ContentID        string    `gorm:"type:varchar(80);index" json:"content_id,omitempty"`
	CountryCode      string    `gorm:"type:varchar(10);index" json:"country_code,omitempty"`
	TaskType         string    `gorm:"type:varchar(60);not null;index" json:"task_type"`
	ModelStrategy    string    `gorm:"type:varchar(40);index" json:"model_strategy,omitempty"`
	Provider         string    `gorm:"type:varchar(60);index" json:"provider,omitempty"`
	Model            string    `gorm:"type:varchar(180);not null;index" json:"model"`
	Role             string    `gorm:"type:varchar(40);index" json:"role,omitempty"`
	InputTokens      int       `gorm:"not null;default:0" json:"input_tokens"`
	OutputTokens     int       `gorm:"not null;default:0" json:"output_tokens"`
	EstimatedCostUSD float64   `gorm:"type:decimal(12,6);not null;default:0" json:"estimated_cost_usd"`
	DurationMS       int64     `gorm:"not null;default:0" json:"duration_ms"`
	Status           string    `gorm:"type:varchar(30);not null;index" json:"status"`
	Error            string    `gorm:"type:text" json:"error,omitempty"`
	CreatedAt        time.Time `gorm:"index" json:"created_at"`
}

func (ContentAIModelRun) TableName() string { return "content_ai_model_runs" }

const (
	AIDecisionApproved      = "approved"
	AIDecisionNeedsFix      = "needs_fix"
	AIDecisionRestrictedAds = "restricted_ads"
	AIDecisionRejected      = "rejected"

	AIFixStatusPreviewed = "previewed"
	AIFixStatusApplied   = "applied"
	AIFixStatusRejected  = "rejected"
)

type ContentAIDecision struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	RunID            *uint     `gorm:"index" json:"run_id,omitempty"`
	FindingID        *uint     `gorm:"index" json:"finding_id,omitempty"`
	ContentType      string    `gorm:"type:varchar(30);not null;index:idx_ai_decision_content" json:"content_type"`
	ContentID        string    `gorm:"type:varchar(50);not null;index:idx_ai_decision_content" json:"content_id"`
	CountryCode      string    `gorm:"type:varchar(10);not null;default:'jo';index" json:"country_code"`
	Title            string    `gorm:"type:text" json:"title"`
	Decision         string    `gorm:"type:varchar(30);not null;index" json:"decision"`
	AdSenseRisk      string    `gorm:"type:varchar(30);not null;index" json:"adsense_risk"`
	Score            int       `gorm:"not null;default:0" json:"score"`
	PolicyScore      int       `gorm:"not null;default:0" json:"policy_score"`
	SEOScore         int       `gorm:"not null;default:0" json:"seo_score"`
	LanguageScore    int       `gorm:"not null;default:0" json:"language_score"`
	SafetyLinksScore int       `gorm:"not null;default:0" json:"safety_links_score"`
	StructureScore   int       `gorm:"not null;default:0" json:"structure_score"`
	CanAutoFix       bool      `gorm:"not null;default:false" json:"can_auto_fix"`
	Provider         string    `gorm:"type:varchar(50);not null;default:'local'" json:"provider"`
	Model            string    `gorm:"type:varchar(150)" json:"model,omitempty"`
	PromptVersion    string    `gorm:"type:varchar(80);not null;default:'content-intelligence-v1'" json:"prompt_version,omitempty"`
	AITokens         int       `gorm:"not null;default:0" json:"ai_tokens,omitempty"`
	ProcessingTimeMS int64     `gorm:"not null;default:0" json:"processing_time_ms,omitempty"`
	Summary          string    `gorm:"type:text" json:"summary"`
	ReportJSON       string    `gorm:"type:longtext" json:"report_json,omitempty"`
	CreatedByUserID  *uint     `gorm:"index" json:"created_by_user_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`

	Issues      []ContentAIIssue      `gorm:"foreignKey:DecisionID" json:"issues,omitempty"`
	Suggestions []ContentAISuggestion `gorm:"foreignKey:DecisionID" json:"suggestions,omitempty"`
}

func (ContentAIDecision) TableName() string { return "content_ai_decisions" }

type ContentAIIssue struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	DecisionID uint      `gorm:"not null;index" json:"decision_id"`
	Type       string    `gorm:"type:varchar(40);not null;index" json:"type"`
	Severity   string    `gorm:"type:varchar(20);not null;index" json:"severity"`
	Message    string    `gorm:"type:text;not null" json:"message"`
	Action     string    `gorm:"type:varchar(80)" json:"action"`
	Evidence   string    `gorm:"type:text" json:"evidence,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

func (ContentAIIssue) TableName() string { return "content_ai_issues" }

type ContentAISuggestion struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	DecisionID uint      `gorm:"not null;index" json:"decision_id"`
	Type       string    `gorm:"type:varchar(40);not null;index" json:"type"`
	Priority   string    `gorm:"type:varchar(20);not null;index" json:"priority"`
	Message    string    `gorm:"type:text;not null" json:"message"`
	CreatedAt  time.Time `json:"created_at"`
}

func (ContentAISuggestion) TableName() string { return "content_ai_suggestions" }

type ContentAIFixPreview struct {
	ID               uint       `gorm:"primaryKey" json:"id"`
	DecisionID       uint       `gorm:"not null;index" json:"decision_id"`
	ContentType      string     `gorm:"type:varchar(30);not null;index" json:"content_type"`
	ContentID        string     `gorm:"type:varchar(50);not null;index" json:"content_id"`
	CountryCode      string     `gorm:"type:varchar(10);not null;default:'jo';index" json:"country_code"`
	OriginalTitle    string     `gorm:"type:text" json:"original_title"`
	OriginalContent  string     `gorm:"type:longtext" json:"original_content"`
	FixedTitle       string     `gorm:"type:text" json:"fixed_title"`
	FixedContent     string     `gorm:"type:longtext" json:"fixed_content"`
	FixSummary       string     `gorm:"type:text" json:"fix_summary"`
	Status           string     `gorm:"type:varchar(20);not null;default:'previewed';index" json:"status"`
	AppliedByUserID  *uint      `gorm:"index" json:"applied_by_user_id,omitempty"`
	AppliedAt        *time.Time `json:"applied_at,omitempty"`
	RejectedByUserID *uint      `gorm:"index" json:"rejected_by_user_id,omitempty"`
	RejectedAt       *time.Time `json:"rejected_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func (ContentAIFixPreview) TableName() string { return "content_ai_fix_previews" }

type ContentAIApprovalLog struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	FixPreviewID uint      `gorm:"not null;index" json:"fix_preview_id"`
	DecisionID   uint      `gorm:"not null;index" json:"decision_id"`
	Action       string    `gorm:"type:varchar(30);not null;index" json:"action"`
	UserID       *uint     `gorm:"index" json:"user_id,omitempty"`
	Note         string    `gorm:"type:text" json:"note,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

func (ContentAIApprovalLog) TableName() string { return "content_ai_approval_logs" }
