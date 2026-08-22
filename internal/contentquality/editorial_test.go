package contentquality

import (
	"testing"

	"github.com/imanjo/fiber-api/internal/models"
)

func TestApplyEditorialDecisionNoindexBlocksSearchAndAds(t *testing.T) {
	gate := Gate{Indexable: true, AdsEligible: true, Reasons: []string{"approved"}}
	got := ApplyEditorialDecision(gate, models.EditorialDecisionNoindex)
	if got.Indexable || got.AdsEligible {
		t.Fatalf("noindex must block indexing and ads: %+v", got)
	}
	if len(got.Reasons) != 2 {
		t.Fatalf("expected editorial reason to be appended once: %+v", got.Reasons)
	}
}

func TestApplyEditorialDecisionWorkflowLabelsDoNotMutateGate(t *testing.T) {
	for _, decision := range []string{
		models.EditorialDecisionKeep,
		models.EditorialDecisionImprove,
		models.EditorialDecisionMerge301,
		models.EditorialDecisionUnclassified,
	} {
		gate := Gate{Indexable: true, AdsEligible: true, Reasons: []string{"approved"}}
		got := ApplyEditorialDecision(gate, decision)
		if !got.Indexable || !got.AdsEligible || len(got.Reasons) != 1 {
			t.Fatalf("%s must remain workflow-only until separately enforced: %+v", decision, got)
		}
	}
}
