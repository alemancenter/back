package contentquality

import (
	"fmt"
	"strings"

	"github.com/imanjo/fiber-api/internal/models"
)

const (
	// ApprovedMinScore is an internal ImanJo quality threshold.
	// It is not a Google/AdSense word-count requirement.
	ApprovedMinScore = 90

	DecisionUnaudited = "unaudited"
	RiskUnknown       = "unknown"
)

// Gate is the single policy result consumed by public ad status, SEO indexing,
// and sitemap decisions. It intentionally separates search indexing from ad
// eligibility so editorial issues do not automatically remove useful pages.
type Gate struct {
	Indexable   bool     `json:"indexable"`
	AdsEligible bool     `json:"ads_eligible"`
	Audited     bool     `json:"audited"`
	Decision    string   `json:"decision"`
	Risk        string   `json:"risk"`
	Score       int      `json:"score"`
	Reasons     []string `json:"reasons"`
	DecisionID  *uint    `json:"decision_id,omitempty"`
}

// Unaudited keeps search indexing unchanged during the staged audit rollout,
// but never permits ads before a saved content-audit decision exists.
func Unaudited() Gate {
	return Gate{
		Indexable:   true,
		AdsEligible: false,
		Audited:     false,
		Decision:    DecisionUnaudited,
		Risk:        RiskUnknown,
		Score:       0,
		Reasons: []string{
			"لم يخضع المحتوى لتدقيق الجودة بعد؛ الإعلانات متوقفة حتى اكتمال التدقيق.",
		},
	}
}

// Evaluate converts one persisted AI/content-audit decision into the canonical
// enforcement result. No word-count heuristic is applied here; those signals
// belong to the audit engine that produced the saved decision.
func Evaluate(decision *models.ContentAIDecision) Gate {
	if decision == nil {
		return Unaudited()
	}

	decisionName := strings.ToLower(strings.TrimSpace(decision.Decision))
	risk := strings.ToLower(strings.TrimSpace(decision.AdSenseRisk))
	if decisionName == "" {
		decisionName = "unknown"
	}
	if risk == "" {
		risk = RiskUnknown
	}

	score := clamp(decision.Score)
	decisionID := decision.ID
	gate := Gate{
		Indexable:   true,
		AdsEligible: false,
		Audited:     true,
		Decision:    decisionName,
		Risk:        risk,
		Score:       score,
		Reasons:     make([]string, 0, 6),
		DecisionID:  &decisionID,
	}

	// Search indexing is intentionally conservative. Only an explicit rejection
	// or critical policy risk removes a page from the index. needs_fix and
	// restricted_ads remain searchable while editors improve them.
	if decisionName == models.AIDecisionRejected {
		gate.Indexable = false
		gate.Reasons = append(gate.Reasons, "قرار التدقيق رفض المحتوى؛ الصفحة غير مؤهلة للأرشفة.")
	}
	if risk == "critical" {
		gate.Indexable = false
		gate.Reasons = append(gate.Reasons, "مستوى المخاطرة حرج؛ الأرشفة والإعلانات متوقفتان حتى المعالجة.")
	}

	// Ads are stricter than indexing: only a fully approved, low-risk decision
	// at the audit engine's approval score is eligible.
	gate.AdsEligible = decisionName == models.AIDecisionApproved &&
		risk == "low" &&
		score >= ApprovedMinScore

	if gate.AdsEligible {
		gate.Reasons = append(gate.Reasons, "المحتوى مدقق ومعتمد بمخاطرة منخفضة ومؤهل للإعلانات.")
	} else {
		switch decisionName {
		case models.AIDecisionNeedsFix:
			gate.Reasons = append(gate.Reasons, "المحتوى يحتاج تحسينًا قبل تفعيل الإعلانات.")
		case models.AIDecisionRestrictedAds:
			gate.Reasons = append(gate.Reasons, "قرار التدقيق يقيّد الإعلانات على هذا المحتوى.")
		case models.AIDecisionRejected:
			gate.Reasons = append(gate.Reasons, "المحتوى المرفوض غير مؤهل للإعلانات.")
		case models.AIDecisionApproved:
			if risk != "low" {
				gate.Reasons = append(gate.Reasons, fmt.Sprintf("قرار الاعتماد موجود لكن مستوى المخاطرة %s وليس منخفضًا.", risk))
			}
			if score < ApprovedMinScore {
				gate.Reasons = append(gate.Reasons, fmt.Sprintf("درجة التدقيق %d أقل من حد الاعتماد الداخلي %d.", score, ApprovedMinScore))
			}
		default:
			gate.Reasons = append(gate.Reasons, "حالة قرار التدقيق غير معروفة؛ الإعلانات متوقفة احترازيًا.")
		}
	}

	// Surface the most important saved audit evidence without exposing the full
	// report. Future SEO/sitemap consumers get the same deterministic explanation.
	for _, issue := range decision.Issues {
		severity := strings.ToLower(strings.TrimSpace(issue.Severity))
		if severity != "high" && severity != "critical" {
			continue
		}
		message := strings.TrimSpace(issue.Message)
		if message == "" {
			continue
		}
		gate.Reasons = append(gate.Reasons, message)
		if len(gate.Reasons) >= 6 {
			break
		}
	}

	if len(gate.Reasons) == 0 {
		gate.Reasons = append(gate.Reasons, "التدقيق مكتمل؛ لا توجد ملاحظات إضافية من بوابة الجودة.")
	}
	return gate
}

func clamp(value int) int {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
