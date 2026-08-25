package models

import "time"

const (
	// RuleTypeOfficialGoogleRequirement rules reflect a documented Google Search/
	// AdSense/Publisher Policy requirement. These are the only rule type allowed
	// to block internal readiness unconditionally, regardless of content type.
	RuleTypeOfficialGoogleRequirement = "official_google_requirement"
	// RuleTypeSiteEditorialStandard rules are this site's own internal quality bar
	// (e.g. a minimum word count for articles). They can block readiness for the
	// content types they apply to, but must never be presented as a Google rule.
	RuleTypeSiteEditorialStandard = "site_editorial_standard"
	// RuleTypeOptimizationRecommendation rules are suggestions only — they never
	// block readiness by themselves.
	RuleTypeOptimizationRecommendation = "optimization_recommendation"

	RuleFixMethodManual    = "manual"
	RuleFixMethodAIPreview = "ai_preview"
	RuleFixMethodAutoApply = "auto_apply"

	RuleScopePage = "page"
	RuleScopeSite = "site"
)

// ContentQualityRule is governance metadata for one quality/SEO/policy check — see
// back/docs/reports/CONTENT_QUALITY_GOVERNANCE_CENTER_PLAN.md §3. The verification
// logic itself always stays in Go (contentaudit/contentquality); this table only
// carries the documentation/governance fields — source, severity, auto-apply
// permission, version — so an internal editorial opinion can never silently pass
// as an official Google requirement, and so the registry can be reviewed without a
// deploy. Only RuleTypeOfficialGoogleRequirement rows may set BlocksReadiness on a
// page unconditionally; editorial-standard rows block only for the content types
// listed in ContentTypes.
type ContentQualityRule struct {
	Code                  string    `gorm:"primaryKey;type:varchar(60)" json:"code"`
	Scope                 string    `gorm:"type:varchar(10);not null" json:"scope"`
	ContentTypes          string    `gorm:"type:text;not null" json:"content_types"` // JSON array, e.g. ["article","post"]
	Severity              string    `gorm:"type:varchar(20);not null" json:"severity"`
	RuleType              string    `gorm:"type:varchar(30);not null;index" json:"rule_type"`
	BlocksReadiness       bool      `gorm:"not null;default:false" json:"blocks_readiness"`
	RequiresHumanDecision bool      `gorm:"not null;default:false" json:"requires_human_decision"`
	SourceURL             *string   `gorm:"type:text" json:"source_url,omitempty"`
	VerificationMethod    string    `gorm:"type:text;not null" json:"verification_method"`
	FixMethod             string    `gorm:"type:varchar(20);not null" json:"fix_method"`
	AutoApplyAllowed      bool      `gorm:"not null;default:false" json:"auto_apply_allowed"`
	Version               int       `gorm:"not null;default:1" json:"version"`
	LastReviewedAt        time.Time `gorm:"not null" json:"last_reviewed_at"`
	UpdatedAt             time.Time `json:"updated_at"`
	CreatedAt             time.Time `json:"created_at"`
}

func (ContentQualityRule) TableName() string { return "content_quality_rules" }
