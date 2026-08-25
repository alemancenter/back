// Package rulesregistry is the single documented source of truth for content
// quality/SEO/policy rule metadata (see
// back/docs/reports/CONTENT_QUALITY_GOVERNANCE_CENTER_PLAN.md §3). It is
// deliberately a leaf package with no dependency on contentaudit or
// contentquality, so both the verification code (services/contentaudit,
// contentquality) and the API layer (handlers/contentaudit) can depend on it
// without an import cycle. The registry only documents rules; it never
// evaluates them — that logic stays where it already lives.
package rulesregistry

import (
	"time"

	"github.com/imanjo/fiber-api/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var reviewedAt = time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)

// Registry documents every problem code currently produced by
// handlers/contentaudit/readiness_problems.go. Codes here and codes in that
// file's readinessProblems map must always match 1:1 — see
// readiness_registry_consistency_test.go, which fails the build if they drift.
var Registry = []models.ContentQualityRule{
	{
		Code:                  "unaudited",
		Scope:                 models.RuleScopePage,
		ContentTypes:          `["article","post"]`,
		Severity:              "high",
		RuleType:              models.RuleTypeSiteEditorialStandard,
		BlocksReadiness:       true,
		RequiresHumanDecision: false,
		VerificationMethod:    "contentquality.Gate.Audited (handlers/contentaudit/readiness_problems.go classifyReadinessProblems)",
		FixMethod:             models.RuleFixMethodManual,
		AutoApplyAllowed:      false,
		Version:               1,
		LastReviewedAt:        reviewedAt,
	},
	{
		Code:                  "policy_blocked",
		Scope:                 models.RuleScopePage,
		ContentTypes:          `["article","post"]`,
		Severity:              "critical",
		RuleType:              models.RuleTypeOfficialGoogleRequirement,
		BlocksReadiness:       true,
		RequiresHumanDecision: true,
		// SourceURL intentionally left unset — link to Google's Publisher Policies /
		// Search Essentials pages once a human has verified the exact, current URL.
		VerificationMethod: "contentquality.Gate.Indexable == false (rejected decision or critical AdSense risk)",
		FixMethod:          models.RuleFixMethodManual,
		AutoApplyAllowed:   false,
		Version:            1,
		LastReviewedAt:     reviewedAt,
	},
	{
		Code:                  "ads_not_eligible",
		Scope:                 models.RuleScopePage,
		ContentTypes:          `["article","post"]`,
		Severity:              "high",
		RuleType:              models.RuleTypeSiteEditorialStandard,
		BlocksReadiness:       true,
		RequiresHumanDecision: false,
		VerificationMethod:    "contentquality.Gate: Indexable && !AdsEligible with no more specific problem matched",
		FixMethod:             models.RuleFixMethodAIPreview,
		AutoApplyAllowed:      false,
		Version:               1,
		LastReviewedAt:        reviewedAt,
	},
	{
		Code:                  "thin_content",
		Scope:                 models.RuleScopePage,
		ContentTypes:          `["article","post"]`,
		Severity:              "high",
		RuleType:              models.RuleTypeSiteEditorialStandard,
		BlocksReadiness:       true,
		RequiresHumanDecision: false,
		VerificationMethod:    "contentquality.DiagnosticReviewMinWords (word_count < 120)",
		FixMethod:             models.RuleFixMethodAIPreview,
		AutoApplyAllowed:      false,
		Version:               1,
		LastReviewedAt:        reviewedAt,
	},
	{
		Code:                  "undocumented_attachment",
		Scope:                 models.RuleScopePage,
		ContentTypes:          `["article","post"]`,
		Severity:              "high",
		RuleType:              models.RuleTypeSiteEditorialStandard,
		BlocksReadiness:       true,
		RequiresHumanDecision: false,
		// File pages do not exist as a standalone content type today (no public
		// URL, no description column on models.File) — see plan §5's revised
		// scope. Instead this rule checks the hosting article/post: attached
		// files with under 120 words of surrounding text read as a bare download
		// button, which Google Publisher Policies treat as low-value/ad-heavy
		// content risk, not merely "short".
		VerificationMethod: "contentquality.Diagnostics: FilesCount > 0 && word_count < DiagnosticShortFileMaxWords (180) — same threshold as the existing short_file_pages batch preset",
		FixMethod:          models.RuleFixMethodAIPreview,
		AutoApplyAllowed:   false,
		Version:            1,
		LastReviewedAt:     reviewedAt,
	},
	{
		Code:                  "needs_enrichment",
		Scope:                 models.RuleScopePage,
		ContentTypes:          `["article","post"]`,
		Severity:              "medium",
		RuleType:              models.RuleTypeSiteEditorialStandard,
		BlocksReadiness:       true,
		RequiresHumanDecision: false,
		VerificationMethod:    "contentquality.DiagnosticStrongMinWords (120 <= word_count < 300)",
		FixMethod:             models.RuleFixMethodAIPreview,
		AutoApplyAllowed:      false,
		Version:               1,
		LastReviewedAt:        reviewedAt,
	},
	{
		Code:                  "meta_description",
		Scope:                 models.RuleScopePage,
		ContentTypes:          `["article","post"]`,
		Severity:              "medium",
		RuleType:              models.RuleTypeSiteEditorialStandard,
		BlocksReadiness:       true,
		RequiresHumanDecision: false,
		VerificationMethod:    "contentquality.DiagnosticMetaMinChars (meta description < 80 chars or missing)",
		FixMethod:             models.RuleFixMethodAutoApply,
		AutoApplyAllowed:      true,
		Version:               1,
		LastReviewedAt:        reviewedAt,
	},
	{
		Code:                  "short_title",
		Scope:                 models.RuleScopePage,
		ContentTypes:          `["article","post"]`,
		Severity:              "medium",
		RuleType:              models.RuleTypeOptimizationRecommendation,
		BlocksReadiness:       false,
		RequiresHumanDecision: false,
		VerificationMethod:    "contentquality.DiagnosticTitleMinChars (title < 20 chars) — internal editorial guidance, not a Google ranking requirement",
		FixMethod:             models.RuleFixMethodAIPreview,
		AutoApplyAllowed:      false,
		Version:               1,
		LastReviewedAt:        reviewedAt,
	},
	{
		Code:                  "unpublished",
		Scope:                 models.RuleScopePage,
		ContentTypes:          `["article","post"]`,
		Severity:              "low",
		RuleType:              models.RuleTypeSiteEditorialStandard,
		BlocksReadiness:       true,
		RequiresHumanDecision: false,
		VerificationMethod:    "published flag on the content row",
		FixMethod:             models.RuleFixMethodManual,
		AutoApplyAllowed:      false,
		Version:               1,
		LastReviewedAt:        reviewedAt,
	},
}

// Codes returns the set of rule codes currently documented in the registry.
func Codes() map[string]bool {
	out := make(map[string]bool, len(Registry))
	for _, rule := range Registry {
		out[rule.Code] = true
	}
	return out
}

// MissingFrom reports registry codes that do not appear in knownCodes — i.e. a
// rule is documented here but no longer produced by the engine (or was renamed
// without updating this file).
func MissingFrom(knownCodes map[string]bool) []string {
	var missing []string
	for _, rule := range Registry {
		if !knownCodes[rule.Code] {
			missing = append(missing, rule.Code)
		}
	}
	return missing
}

// UndocumentedIn reports engine-produced codes that have no registry entry —
// i.e. a rule exists in code but was never documented/classified here.
func UndocumentedIn(knownCodes map[string]bool) []string {
	registered := Codes()
	var undocumented []string
	for code := range knownCodes {
		if !registered[code] {
			undocumented = append(undocumented, code)
		}
	}
	return undocumented
}

// Seed upserts every documented rule into db by primary key (Code), so the
// registry table always reflects what's compiled into this binary. Call once
// per country database at startup, alongside AutoMigrate. Safe to call
// repeatedly — existing rows are overwritten with the current Go-defined values,
// so a manual DB edit to a Code this package still owns will not stick across a
// restart; that is intentional until an admin-editable version field is added.
func Seed(db *gorm.DB) error {
	if db == nil || len(Registry) == 0 {
		return nil
	}
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "code"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"scope", "content_types", "severity", "rule_type", "blocks_readiness",
			"requires_human_decision", "source_url", "verification_method",
			"fix_method", "auto_apply_allowed", "version", "last_reviewed_at", "updated_at",
		}),
	}).Create(&Registry).Error
}
