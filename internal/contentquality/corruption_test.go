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

