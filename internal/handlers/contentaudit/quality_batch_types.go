package contentaudit

import (
	"strconv"
	"strings"
	"time"
)

// Content quality batch processing is preview-first for substantive content.
// The sole auto-apply exception is the guarded meta_description repair path.
type contentQualityBatchRequest struct {
	CountryCode   string `json:"country_code"`
	ContentType   string `json:"content_type"`
	Level         string `json:"level"`
	Query         string `json:"q"`
	Limit         int    `json:"limit"`
	Concurrency   int    `json:"concurrency"`
	Mode          string `json:"mode"`
	ModelStrategy string `json:"model_strategy"`
	Source        string `json:"source"`
	Preset        string `json:"preset"`
	Targets       []contentQualityBatchTarget `json:"targets,omitempty"`
}

type contentQualityBatchTarget struct {
	ContentType string `json:"content_type"`
	ContentID   uint   `json:"content_id"`
}

type contentQualityBatchItem struct {
	ContentType  string     `json:"content_type"`
	ContentID    uint       `json:"content_id"`
	CountryCode  string     `json:"country_code"`
	Title        string     `json:"title"`
	URL          string     `json:"url"`
	Status       string     `json:"status"`
	ScoreBefore  int        `json:"score_before"`
	ScoreAfter   int        `json:"score_after,omitempty"`
	DecisionID   *uint      `json:"decision_id,omitempty"`
	FixPreviewID *uint      `json:"fix_preview_id,omitempty"`
	Message      string     `json:"message,omitempty"`
	Error        string     `json:"error,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
}

type contentQualityBatchJob struct {
	ID              string                    `json:"id"`
	Status          string                    `json:"status"`
	Mode            string                    `json:"mode"`
	ModelStrategy   string                    `json:"model_strategy"`
	CountryCode     string                    `json:"country_code"`
	ContentType     string                    `json:"content_type"`
	Level           string                    `json:"level"`
	Query           string                    `json:"q,omitempty"`
	Source          string                    `json:"source"`
	Preset          string                    `json:"preset"`
	Limit           int                       `json:"limit"`
	Concurrency     int                       `json:"concurrency"`
	TotalItems      int                       `json:"total_items"`
	ProcessedItems  int                       `json:"processed_items"`
	SuccessfulItems int                       `json:"successful_items"`
	FailedItems     int                       `json:"failed_items"`
	PendingItems    int                       `json:"pending_items"`
	Progress        int                       `json:"progress"`
	CancelRequested bool                      `json:"cancel_requested"`
	Error           string                    `json:"error,omitempty"`
	CreatedByUserID *uint                     `json:"created_by_user_id,omitempty"`
	CreatedAt       time.Time                 `json:"created_at"`
	StartedAt       *time.Time                `json:"started_at,omitempty"`
	FinishedAt      *time.Time                `json:"finished_at,omitempty"`
	Items           []contentQualityBatchItem `json:"items"`
}

func sanitizeCountryCode(value string) string {
	code := strings.ToLower(strings.TrimSpace(value))
	if code == "" {
		return ""
	}

	allowed := map[string]struct{}{
		"jo": {},
		"sa": {},
		"eg": {},
		"ps": {},
	}
	if _, ok := allowed[code]; ok {
		return code
	}

	return ""
}

func normalizeQualityBatchRequest(req contentQualityBatchRequest) contentQualityBatchRequest {
	req.CountryCode = sanitizeCountryCode(req.CountryCode)
	if req.CountryCode == "" {
		req.CountryCode = "jo"
	}
	req.ContentType = strings.ToLower(strings.TrimSpace(req.ContentType))
	if req.ContentType != "article" && req.ContentType != "post" && req.ContentType != "all" {
		req.ContentType = "all"
	}
	req.Level = strings.ToLower(strings.TrimSpace(req.Level))
	if req.Level != "weak" && req.Level != "review" && req.Level != "ready" {
		req.Level = "weak"
	}
	req.Mode = strings.ToLower(strings.TrimSpace(req.Mode))
	if req.Mode != "analyze_only" && req.Mode != "fix_preview" && req.Mode != "full_review" && req.Mode != "auto_apply" {
		req.Mode = "fix_preview"
	}
	req.ModelStrategy = strings.ToLower(strings.TrimSpace(req.ModelStrategy))
	switch req.ModelStrategy {
	case "economy", "balanced", "quality", "final_review":
		// accepted
	case "multi_stage_quality":
		req.ModelStrategy = "balanced"
	case "premium", "high_quality":
		req.ModelStrategy = "quality"
	case "critical", "final":
		req.ModelStrategy = "final_review"
	default:
		req.ModelStrategy = "balanced"
	}
	if req.Limit <= 0 || req.Limit > 500 {
		req.Limit = 20 // matches the frontend default
	}
	if req.Concurrency <= 0 {
		req.Concurrency = 2
	}
	maxConcurrency := 4
	switch req.ModelStrategy {
	case "economy":
		maxConcurrency = 6
	case "balanced":
		maxConcurrency = 4
	case "quality":
		maxConcurrency = 3
	case "final_review":
		maxConcurrency = 2
	}
	if req.Concurrency > maxConcurrency {
		req.Concurrency = maxConcurrency
	}
	req.Source = strings.ToLower(strings.TrimSpace(req.Source))
	if req.Source == "" {
		req.Source = "adsense_readiness"
	}
	if req.Source != "adsense_readiness" && req.Source != "manual_filter" {
		req.Source = "adsense_readiness"
	}

	req.Preset = strings.ToLower(strings.TrimSpace(req.Preset))
	switch req.Preset {
	case "weak_first", "indexed_weak", "short_file_pages", "custom_filter", "selected_items",
		readinessProblemUnaudited, readinessProblemPolicyBlocked, readinessProblemAdsNotEligible,
		readinessProblemThinContent, readinessProblemNeedsEnrichment, readinessProblemMetaDescription,
		readinessProblemShortTitle:
		// accepted
	case "", "default":
		req.Preset = "weak_first"
	default:
		req.Preset = "weak_first"
	}

	// Smart AdSense presets intentionally override ambiguous manual defaults.
	// The operator only chooses how many items to process and the system chooses
	// the highest-risk pages from the AdSense readiness report.
	if req.Source == "adsense_readiness" {
		switch req.Preset {
		case "weak_first", "indexed_weak", "short_file_pages", readinessProblemPolicyBlocked:
			req.Level = "weak"
		}
	}

	// Explicit selections are bounded and de-duplicated. An issue-specific preset
	// remains intact so the backend can verify every selected row has that issue.
	if len(req.Targets) > 0 {
		seen := make(map[string]struct{}, len(req.Targets))
		normalized := make([]contentQualityBatchTarget, 0, len(req.Targets))
		for _, target := range req.Targets {
			target.ContentType = strings.ToLower(strings.TrimSpace(target.ContentType))
			if (target.ContentType != "article" && target.ContentType != "post") || target.ContentID == 0 {
				continue
			}
			key := target.ContentType + ":" + strconv.FormatUint(uint64(target.ContentID), 10)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			normalized = append(normalized, target)
			if len(normalized) >= 100 {
				break
			}
		}
		req.Targets = normalized
		if len(normalized) > 0 {
			if !isIssueSpecificQualityPreset(req.Preset) {
				req.Preset = "selected_items"
			}
			req.Limit = len(normalized)
		}
	}

	// Auto-apply is deliberately allowlisted to metadata. Any malformed or future
	// request attempting to auto-apply title/body/policy work is downgraded to a
	// human-reviewed preview.
	if req.Mode == "auto_apply" && req.Preset != readinessProblemMetaDescription {
		req.Mode = "fix_preview"
	}

	req.Query = strings.TrimSpace(req.Query)
	return req
}

func isIssueSpecificQualityPreset(preset string) bool {
	switch preset {
	case readinessProblemUnaudited, readinessProblemPolicyBlocked, readinessProblemAdsNotEligible,
		readinessProblemThinContent, readinessProblemNeedsEnrichment, readinessProblemMetaDescription,
		readinessProblemShortTitle:
		return true
	default:
		return false
	}
}
