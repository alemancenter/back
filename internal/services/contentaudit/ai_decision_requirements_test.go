package contentaudit

import (
	"strings"
	"testing"

	"github.com/imanjo/fiber-api/internal/models"
)

func TestCurrentSourceRequirementsRejectStaleMetadataApproval(t *testing.T) {
	report := aiReport{Decision: models.AIDecisionApproved, AdSenseRisk: "low", Score: 98}
	content := &loadedContent{
		Title:           "عنوان تعليمي واضح وطويل بما يكفي",
		Content:         strings.Repeat("محتوى ", 350),
		MetaDescription: "",
	}
	guarded := enforceCurrentSourceRequirements(report, content)
	if guarded.Decision != models.AIDecisionNeedsFix || guarded.Score >= QualityGateApprovedMinScore {
		t.Fatalf("stale metadata approval was not revoked: %#v", guarded)
	}
	if !reportHasIssueAction(guarded.Issues, "repair_meta_description") {
		t.Fatalf("metadata repair issue missing: %#v", guarded.Issues)
	}
}

func TestCurrentSourceRequirementsKeepCompleteApproval(t *testing.T) {
	report := aiReport{Decision: models.AIDecisionApproved, AdSenseRisk: "low", Score: 98}
	content := &loadedContent{
		Title:           "عنوان تعليمي واضح وطويل بما يكفي",
		Content:         strings.Repeat("محتوى ", 350),
		MetaDescription: strings.Repeat("و", 100),
	}
	guarded := enforceCurrentSourceRequirements(report, content)
	if guarded.Decision != models.AIDecisionApproved || guarded.Score != 98 {
		t.Fatalf("complete approval changed unexpectedly: %#v", guarded)
	}
}
