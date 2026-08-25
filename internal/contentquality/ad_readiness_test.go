package contentquality

import (
	"strings"
	"testing"
)

func TestApplyAdReadinessRequirementsOnlyRevokesAds(t *testing.T) {
	base := Gate{Audited: true, Indexable: true, AdsEligible: true, Decision: "approved", Risk: "low", Score: 95, Reasons: []string{"المحتوى مؤهل للإعلانات."}}
	guarded := ApplyAdReadinessRequirements(base, "عنوان قصير", "نص قصير", "")

	if guarded.AdsEligible {
		t.Fatal("incomplete current metadata/content must revoke ad eligibility")
	}
	if !guarded.Indexable {
		t.Fatal("editorial ad requirements must not change indexing")
	}
	if guarded.Decision != base.Decision || guarded.Score != base.Score || guarded.Risk != base.Risk {
		t.Fatalf("guard changed the persisted audit result: %#v", guarded)
	}
}

func TestApplyAdReadinessRequirementsNeverGrantsAds(t *testing.T) {
	base := Unaudited()
	guarded := ApplyAdReadinessRequirements(base, strings.Repeat("عنوان ", 10), strings.Repeat("محتوى ", 350), strings.Repeat("و", 90))
	if guarded.AdsEligible {
		t.Fatal("current-page requirements must never grant eligibility")
	}
}

func TestApplyAdReadinessRequirementsKeepsCompleteApproval(t *testing.T) {
	base := Gate{Audited: true, Indexable: true, AdsEligible: true, Decision: "approved", Risk: "low", Score: 95}
	guarded := ApplyAdReadinessRequirements(base, "عنوان تعليمي واضح وطويل بما يكفي", strings.Repeat("محتوى ", 350), strings.Repeat("و", 90))
	if !guarded.AdsEligible {
		t.Fatalf("complete approved page should remain eligible: %#v", guarded.Reasons)
	}
}
