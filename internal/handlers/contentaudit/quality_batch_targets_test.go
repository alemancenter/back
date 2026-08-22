package contentaudit

import "testing"

func TestWeakFirstUsesGateEligibilityNotLegacyScoreBands(t *testing.T) {
	req := contentQualityBatchRequest{
		Source: "adsense_readiness",
		Preset: "weak_first",
	}

	tests := []struct {
		name string
		item unifiedReadinessItem
		want bool
	}{
		{
			name: "non-indexable item included",
			item: unifiedReadinessItem{Level: "weak", Score: 99, ShouldIndex: false, ShouldShowAds: false},
			want: true,
		},
		{
			name: "indexable but not ad eligible included",
			item: unifiedReadinessItem{Level: "review", Score: 95, ShouldIndex: true, ShouldShowAds: false},
			want: true,
		},
		{
			name: "ad eligible item excluded regardless of score",
			item: unifiedReadinessItem{Level: "ready", Score: 20, ShouldIndex: true, ShouldShowAds: true},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldIncludeQualityTarget(tt.item, req); got != tt.want {
				t.Fatalf("shouldIncludeQualityTarget() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIndexedWeakTargetsEditorialWorkWithoutChangingGate(t *testing.T) {
	req := contentQualityBatchRequest{Source: "adsense_readiness", Preset: "indexed_weak"}

	unaudited := unifiedReadinessItem{
		Level:          "review",
		ShouldIndex:    true,
		ShouldShowAds:  false,
		Audited:        false,
		DiagnosticSignals: nil,
	}
	if !shouldIncludeQualityTarget(unaudited, req) {
		t.Fatal("unaudited indexable content should be selectable for editorial review")
	}

	approved := unifiedReadinessItem{
		Level:             "ready",
		ShouldIndex:       true,
		ShouldShowAds:     true,
		Audited:           true,
		DiagnosticSignals: []string{"إشارة تحريرية"},
	}
	if shouldIncludeQualityTarget(approved, req) {
		t.Fatal("ad-eligible content must not enter indexed_weak batch")
	}
}

func TestShortFilePagesIsDiagnosticSelectionOnly(t *testing.T) {
	req := contentQualityBatchRequest{Source: "adsense_readiness", Preset: "short_file_pages"}

	item := unifiedReadinessItem{
		FilesCount:       1,
		WordCount:        100,
		ShouldIndex:      true,
		ShouldShowAds:    false,
		DiagnosticSignals: []string{"إشارة تحريرية"},
	}
	if !shouldIncludeQualityTarget(item, req) {
		t.Fatal("short file page should be selectable for editorial processing")
	}
	if !item.ShouldIndex {
		t.Fatal("batch selection must not mutate central indexing decision")
	}
}
