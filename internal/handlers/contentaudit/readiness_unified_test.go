package contentaudit

import (
	"strings"
	"testing"

	"github.com/imanjo/fiber-api/internal/models"
	auditservice "github.com/imanjo/fiber-api/internal/services/contentaudit"
)

func TestUnifiedReadinessUnauditedStrongTextStillBlocksAds(t *testing.T) {
	gate := auditservice.UnauditedQualityGate()
	text := strings.Repeat("كلمة ", 350)
	item := buildUnifiedReadinessItem("عنوان تعليمي طويل بما يكفي للمراجعة", text, strings.Repeat("و", 90), "", 1, true, "article", 10, "jo", gate, nil)
	if !item.ShouldIndex {
		t.Fatal("published unaudited content should remain indexable during staged rollout")
	}
	if item.ShouldShowAds {
		t.Fatal("diagnostic strength must never make unaudited content ad eligible")
	}
	if item.Level != "review" {
		t.Fatalf("level = %q, want review", item.Level)
	}
}

func TestUnifiedReadinessDiagnosticsCannotOverrideApprovedGate(t *testing.T) {
	decision := &models.ContentAIDecision{ID: 7, Decision: models.AIDecisionApproved, AdSenseRisk: "low", Score: 95}
	gate := auditservice.EvaluateQualityGate(decision)
	item := buildUnifiedReadinessItem("قصير", "نص قصير", "", "", 0, true, "article", 11, "jo", gate, nil)
	if !item.ShouldShowAds {
		t.Fatal("editorial diagnostics must not override an approved central gate")
	}
	if len(item.DiagnosticSignals) == 0 {
		t.Fatal("expected diagnostics to remain visible for reviewers")
	}
	if item.Score != 95 {
		t.Fatalf("score = %d, want canonical gate score 95", item.Score)
	}
}

func TestUnifiedReadinessRejectedGateWinsOverStrongDiagnostics(t *testing.T) {
	decision := &models.ContentAIDecision{ID: 8, Decision: models.AIDecisionRejected, AdSenseRisk: "high", Score: 30}
	gate := auditservice.EvaluateQualityGate(decision)
	text := strings.Repeat("محتوى ", 500)
	item := buildUnifiedReadinessItem("عنوان تعليمي طويل بما يكفي للمراجعة", text, strings.Repeat("و", 90), "", 2, true, "post", 12, "jo", gate, nil)
	if item.ShouldIndex || item.ShouldShowAds {
		t.Fatal("rejected central gate must block indexing and ads")
	}
	if item.Level != "weak" {
		t.Fatalf("level = %q, want weak", item.Level)
	}
}

func TestUnifiedReadinessPublicationStatusIsADeploymentGuard(t *testing.T) {
	decision := &models.ContentAIDecision{ID: 9, Decision: models.AIDecisionApproved, AdSenseRisk: "low", Score: 95}
	gate := auditservice.EvaluateQualityGate(decision)
	item := buildUnifiedReadinessItem("عنوان تعليمي طويل بما يكفي للمراجعة", strings.Repeat("محتوى ", 350), strings.Repeat("و", 90), "", 1, false, "article", 13, "jo", gate, nil)
	if item.ShouldIndex || item.ShouldShowAds {
		t.Fatal("unpublished content must not be surfaced as indexable or ad eligible")
	}
}

func TestUnifiedReadinessPolicyBaselineNeverGrantsAds(t *testing.T) {
	gate := auditservice.UnauditedQualityGate()
	baseline := &models.ContentPolicyReadiness{Score: 100, Signal: models.PolicyReadinessSignalClean, Issues: "لا توجد ملاحظات من الفحص الحتمي."}
	item := buildUnifiedReadinessItem("عنوان تعليمي طويل بما يكفي للمراجعة", strings.Repeat("محتوى ", 350), strings.Repeat("و", 90), "", 1, true, "article", 14, "jo", gate, baseline)
	if item.ShouldShowAds {
		t.Fatal("a perfect deterministic policy-baseline score must never grant AdsEligible on its own — only a real ContentAIDecision can")
	}
	if item.Level == "ready" {
		t.Fatalf("level = %q, want anything but ready — baseline-only content must stay in the review bucket", item.Level)
	}
	if item.Score != 100 {
		t.Fatalf("score = %d, want the baseline's 100 to be surfaced for reviewers", item.Score)
	}
	if item.Audited {
		t.Fatal("baseline enrichment must not flip Audited to true — it is not a real AI review")
	}
}

func TestReadinessGateAppliesCurrentSourceCorruptionGuard(t *testing.T) {
	decision := &models.ContentAIDecision{ID: 10, Decision: models.AIDecisionApproved, AdSenseRisk: "low", Score: 99}
	gate := readinessGate(decision, "عنوان سليم", "نص فيه $1 رمز غير محلول", "", "")
	if gate.Indexable || gate.AdsEligible {
		t.Fatal("current source corruption must override saved approval")
	}
	if gate.Risk != "critical" {
		t.Fatalf("risk = %q, want critical", gate.Risk)
	}
}
