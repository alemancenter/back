package contentaudit

import (
	"testing"

	auditservice "github.com/imanjo/fiber-api/internal/services/contentaudit"
)

func TestUnauditedPublicQualityPayloadFailsClosedForAds(t *testing.T) {
	payload := qualityGatePublicPayload(auditservice.UnauditedQualityGate())
	if payload.Eligible || payload.AdsEligible {
		t.Fatalf("unaudited content must fail closed for ads: %+v", payload)
	}
	if !payload.Indexable {
		t.Fatal("unaudited content must remain indexable during staged rollout")
	}
	if payload.Audited {
		t.Fatal("unaudited payload must report audited=false")
	}
}
