package contentquality

import "testing"

func TestDetectReplacementArtifacts(t *testing.T) {
	artifacts := DetectReplacementArtifacts(
		TextField{Name: "title", Value: "عنوان سليم"},
		TextField{Name: "content", Value: "يُ$1 $2$3الهدر الزمني و ${1} مثال"},
	)
	if len(artifacts) != 4 {
		t.Fatalf("expected 4 artifacts, got %d: %#v", len(artifacts), artifacts)
	}
	want := map[string]bool{"$1": true, "$2": true, "$3": true, "${1}": true}
	for _, artifact := range artifacts {
		if !want[artifact.Token] {
			t.Fatalf("unexpected token %q", artifact.Token)
		}
		if artifact.Field != "content" {
			t.Fatalf("unexpected field %q", artifact.Field)
		}
		if artifact.Snippet == "" {
			t.Fatal("expected non-empty snippet")
		}
	}
}

func TestDetectReplacementArtifactsDoesNotFlagLargerDollarAmounts(t *testing.T) {
	artifacts := DetectReplacementArtifacts(TextField{Name: "content", Value: "السعر $10 و $250 فقط"})
	if len(artifacts) != 0 {
		t.Fatalf("expected no artifacts for dollar amounts, got %#v", artifacts)
	}
}

func TestApplyReplacementArtifactGuard(t *testing.T) {
	gate := Gate{Indexable: true, AdsEligible: true, Audited: true, Decision: "approved", Risk: "low", Score: 95}
	artifacts := []ReplacementArtifact{{Token: "$1", Field: "content", Offset: 3}}
	guarded := ApplyReplacementArtifactGuard(gate, artifacts)
	if guarded.Indexable {
		t.Fatal("corrupted content must not remain indexable")
	}
	if guarded.AdsEligible {
		t.Fatal("corrupted content must not remain ad eligible")
	}
	if guarded.Risk != "critical" {
		t.Fatalf("expected critical risk, got %q", guarded.Risk)
	}
	if len(guarded.Reasons) == 0 {
		t.Fatal("expected corruption reason")
	}
}
