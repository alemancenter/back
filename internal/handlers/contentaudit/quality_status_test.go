package contentaudit

import (
	"testing"

	auditservice "github.com/imanjo/fiber-api/internal/services/contentaudit"
)

func TestQualityGatePublicPayloadPreservesGateDecision(t *testing.T) {
	decisionID := uint(42)
	gate := auditservice.ContentQualityGate{
		Indexable:   false,
		AdsEligible: false,
		Audited:     true,
		Decision:    "rejected",
		Risk:        "critical",
		Score:       31,
		Reasons:     []string{"critical quality issue"},
		DecisionID:  &decisionID,
	}

	payload := qualityGatePublicPayload(gate)
	if payload.Eligible {
		t.Fatal("legacy eligible alias must stay false when AdsEligible is false")
	}
	if payload.AdsEligible {
		t.Fatal("AdsEligible must be false")
	}
	if payload.Indexable {
		t.Fatal("Indexable must preserve the gate value false")
	}
	if !payload.Audited {
		t.Fatal("Audited must preserve the gate value true")
	}
	if payload.Decision != gate.Decision || payload.AdSenseRisk != gate.Risk || payload.Score != gate.Score {
		t.Fatalf("payload did not preserve decision metadata: %+v", payload)
	}
	if payload.DecisionID == nil || *payload.DecisionID != decisionID {
		t.Fatalf("DecisionID = %v, want %d", payload.DecisionID, decisionID)
	}
	if len(payload.Reasons) != 1 || payload.Reasons[0] != gate.Reasons[0] {
		t.Fatalf("Reasons = %v, want %v", payload.Reasons, gate.Reasons)
	}
}

func TestQualityGatePublicPayloadKeepsLegacyEligibleAliasInSync(t *testing.T) {
	payload := qualityGatePublicPayload(auditservice.ContentQualityGate{
		Indexable:   true,
		AdsEligible: true,
		Audited:     true,
		Decision:    "approved",
		Risk:        "low",
		Score:       96,
	})

	if !payload.Eligible || !payload.AdsEligible {
		t.Fatalf("eligible aliases diverged: legacy=%v central=%v", payload.Eligible, payload.AdsEligible)
	}
}
