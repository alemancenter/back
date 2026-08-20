package contentaudit

import "testing"

func TestWeakFirstIncludesAllReviewItems(t *testing.T) {
	req := contentQualityBatchRequest{
		Source: "adsense_readiness",
		Preset: "weak_first",
	}

	tests := []struct {
		level string
		score int
		want  bool
	}{
		{"weak", 59, true},
		{"review", 60, true},
		{"review", 75, true},
		{"ready", 80, false},
	}

	for _, tt := range tests {
		item := adsenseReadinessItem{Level: tt.level, Score: tt.score}
		if got := shouldIncludeQualityTarget(item, req); got != tt.want {
			t.Fatalf("level=%s score=%d: got %v want %v", tt.level, tt.score, got, tt.want)
		}
	}
}
