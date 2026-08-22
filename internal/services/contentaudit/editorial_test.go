package contentaudit

import (
	"testing"

	"github.com/imanjo/fiber-api/internal/models"
)

func TestNormalizeEditorialDecision(t *testing.T) {
	valid := map[string]string{
		" KEEP ": models.EditorialDecisionKeep,
		"Improve": models.EditorialDecisionImprove,
		"NOINDEX": models.EditorialDecisionNoindex,
		"merge_301": models.EditorialDecisionMerge301,
		"unclassified": models.EditorialDecisionUnclassified,
	}
	for input, want := range valid {
		if got := NormalizeEditorialDecision(input); got != want {
			t.Fatalf("NormalizeEditorialDecision(%q) = %q, want %q", input, got, want)
		}
	}
	for _, input := range []string{"", "delete", "301", "approved"} {
		if got := NormalizeEditorialDecision(input); got != "" {
			t.Fatalf("invalid decision %q normalized to %q", input, got)
		}
	}
}
