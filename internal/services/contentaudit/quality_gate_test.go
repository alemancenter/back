package contentaudit

import (
	"testing"

	"github.com/imanjo/fiber-api/internal/models"
)

func TestEvaluateQualityGate(t *testing.T) {
	tests := []struct {
		name        string
		decision    *models.ContentAIDecision
		indexable   bool
		adsEligible bool
		audited     bool
	}{
		{
			name:        "unaudited keeps indexing but blocks ads",
			decision:    nil,
			indexable:   true,
			adsEligible: false,
			audited:     false,
		},
		{
			name: "approved low risk high score allows ads",
			decision: &models.ContentAIDecision{
				ID:          1,
				Decision:    models.AIDecisionApproved,
				AdSenseRisk: "low",
				Score:       95,
			},
			indexable:   true,
			adsEligible: true,
			audited:     true,
		},
		{
			name: "approved below internal score blocks ads defensively",
			decision: &models.ContentAIDecision{
				ID:          2,
				Decision:    models.AIDecisionApproved,
				AdSenseRisk: "low",
				Score:       85,
			},
			indexable:   true,
			adsEligible: false,
			audited:     true,
		},
		{
			name: "needs fix stays indexable but ads are blocked",
			decision: &models.ContentAIDecision{
				ID:          3,
				Decision:    models.AIDecisionNeedsFix,
				AdSenseRisk: "medium",
				Score:       80,
			},
			indexable:   true,
			adsEligible: false,
			audited:     true,
		},
		{
			name: "restricted ads stays indexable but ads are blocked",
			decision: &models.ContentAIDecision{
				ID:          4,
				Decision:    models.AIDecisionRestrictedAds,
				AdSenseRisk: "high",
				Score:       70,
			},
			indexable:   true,
			adsEligible: false,
			audited:     true,
		},
		{
			name: "rejected content is not indexable or ad eligible",
			decision: &models.ContentAIDecision{
				ID:          5,
				Decision:    models.AIDecisionRejected,
				AdSenseRisk: "high",
				Score:       30,
			},
			indexable:   false,
			adsEligible: false,
			audited:     true,
		},
		{
			name: "critical risk always disables indexing and ads",
			decision: &models.ContentAIDecision{
				ID:          6,
				Decision:    models.AIDecisionApproved,
				AdSenseRisk: "critical",
				Score:       99,
			},
			indexable:   false,
			adsEligible: false,
			audited:     true,
		},
		{
			name: "unknown decision fails closed for ads",
			decision: &models.ContentAIDecision{
				ID:          7,
				Decision:    "unexpected_state",
				AdSenseRisk: "low",
				Score:       99,
			},
			indexable:   true,
			adsEligible: false,
			audited:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gate := EvaluateQualityGate(tt.decision)
			if gate.Indexable != tt.indexable {
				t.Fatalf("Indexable = %v, want %v", gate.Indexable, tt.indexable)
			}
			if gate.AdsEligible != tt.adsEligible {
				t.Fatalf("AdsEligible = %v, want %v", gate.AdsEligible, tt.adsEligible)
			}
			if gate.Audited != tt.audited {
				t.Fatalf("Audited = %v, want %v", gate.Audited, tt.audited)
			}
			if len(gate.Reasons) == 0 {
				t.Fatal("Reasons must never be empty")
			}
		})
	}
}

func TestEvaluateQualityGateClampsScoreAndIncludesEvidence(t *testing.T) {
	decision := &models.ContentAIDecision{
		ID:          11,
		Decision:    models.AIDecisionRejected,
		AdSenseRisk: "critical",
		Score:       140,
		Issues: []models.ContentAIIssue{
			{Severity: "critical", Message: "مشكلة حرجة مؤكدة"},
		},
	}

	gate := EvaluateQualityGate(decision)
	if gate.Score != 100 {
		t.Fatalf("Score = %d, want 100", gate.Score)
	}
	if gate.DecisionID == nil || *gate.DecisionID != decision.ID {
		t.Fatalf("DecisionID = %v, want %d", gate.DecisionID, decision.ID)
	}

	foundEvidence := false
	for _, reason := range gate.Reasons {
		if reason == "مشكلة حرجة مؤكدة" {
			foundEvidence = true
			break
		}
	}
	if !foundEvidence {
		t.Fatal("expected critical issue evidence in gate reasons")
	}
}
