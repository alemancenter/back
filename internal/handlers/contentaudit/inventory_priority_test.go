package contentaudit

import (
	"testing"

	"github.com/imanjo/fiber-api/internal/contentquality"
	"github.com/imanjo/fiber-api/internal/models"
)

func TestInventoryPriorityExactDuplicateSuggestsMerge(t *testing.T) {
	item := inventoryItem{Published: true, ShouldIndex: true, Audited: true, Similarity: inventorySimilaritySignal{Kind: contentquality.SimilarityKindExact, Similarity: 1}}
	score, reasons, suggested := inventoryPriority(item)
	if score < 100 || len(reasons) == 0 || suggested != models.EditorialDecisionMerge301 {
		t.Fatalf("unexpected exact duplicate priority: score=%d reasons=%v suggested=%s", score, reasons, suggested)
	}
}

func TestInventoryPriorityCorruptionWinsSuggestion(t *testing.T) {
	item := inventoryItem{Published: true, ShouldIndex: false, Corrupted: true, Similarity: inventorySimilaritySignal{Kind: contentquality.SimilarityKindExact, Similarity: 1}}
	_, _, suggested := inventoryPriority(item)
	if suggested != models.EditorialDecisionImprove {
		t.Fatalf("corruption must be repaired before merge planning, got %s", suggested)
	}
}

func TestInventoryPriorityHealthyApprovedSuggestsKeep(t *testing.T) {
	item := inventoryItem{Published: true, ShouldIndex: true, AdsEligible: true, Audited: true, AuditDecision: models.AIDecisionApproved}
	_, _, suggested := inventoryPriority(item)
	if suggested != models.EditorialDecisionKeep {
		t.Fatalf("healthy approved content should suggest keep, got %s", suggested)
	}
}
