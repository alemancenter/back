package contentaudit

import "testing"

func TestCustomFilterWithExplicitQueryIgnoresHiddenWeakLevel(t *testing.T) {
	req := contentQualityBatchRequest{
		Source: "adsense_readiness",
		Preset: "custom_filter",
		Level:  "weak",
		Query:  "169",
	}

	item := unifiedReadinessItem{Level: "review", ShouldIndex: true, ShouldShowAds: false}
	if !shouldIncludeQualityTarget(item, req) {
		t.Fatal("custom_filter with an explicit query must include the matched item regardless of hidden level")
	}
}

func TestCustomFilterWithoutQueryKeepsLevelFilter(t *testing.T) {
	req := contentQualityBatchRequest{
		Source: "adsense_readiness",
		Preset: "custom_filter",
		Level:  "weak",
	}

	if shouldIncludeQualityTarget(unifiedReadinessItem{Level: "review"}, req) {
		t.Fatal("custom_filter without a query must keep the requested level filter")
	}
	if !shouldIncludeQualityTarget(unifiedReadinessItem{Level: "weak"}, req) {
		t.Fatal("custom_filter without a query should include matching level")
	}
}
