package services

import (
	"testing"

	"github.com/imanjo/fiber-api/internal/models"
)

func TestSitemapQualityIndexable(t *testing.T) {
	tests := []struct {
		name     string
		decision *models.ContentAIDecision
		want     bool
	}{
		{
			name:     "unaudited content stays in sitemap",
			decision: nil,
			want:     true,
		},
		{
			name: "approved content stays in sitemap",
			decision: &models.ContentAIDecision{
				Decision:    models.AIDecisionApproved,
				AdSenseRisk: "low",
				Score:       95,
			},
			want: true,
		},
		{
			name: "needs fix stays in sitemap during staged remediation",
			decision: &models.ContentAIDecision{
				Decision:    models.AIDecisionNeedsFix,
				AdSenseRisk: "medium",
				Score:       80,
			},
			want: true,
		},
		{
			name: "restricted ads stays in sitemap",
			decision: &models.ContentAIDecision{
				Decision:    models.AIDecisionRestrictedAds,
				AdSenseRisk: "high",
				Score:       65,
			},
			want: true,
		},
		{
			name: "rejected content is removed from sitemap",
			decision: &models.ContentAIDecision{
				Decision:    models.AIDecisionRejected,
				AdSenseRisk: "high",
				Score:       30,
			},
			want: false,
		},
		{
			name: "critical risk is removed from sitemap regardless of decision",
			decision: &models.ContentAIDecision{
				Decision:    models.AIDecisionApproved,
				AdSenseRisk: "critical",
				Score:       99,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sitemapQualityIndexable(tt.decision); got != tt.want {
				t.Fatalf("sitemapQualityIndexable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestQualityDecisionForSitemap(t *testing.T) {
	decisions := map[uint]models.ContentAIDecision{
		7: {
			ID:          21,
			Decision:    models.AIDecisionRejected,
			AdSenseRisk: "high",
		},
	}

	if got := qualityDecisionForSitemap(decisions, 8); got != nil {
		t.Fatalf("missing decision = %#v, want nil (unaudited)", got)
	}

	got := qualityDecisionForSitemap(decisions, 7)
	if got == nil {
		t.Fatal("existing decision = nil, want saved decision")
	}
	if got.ID != 21 || got.Decision != models.AIDecisionRejected {
		t.Fatalf("existing decision = %#v, want rejected decision 21", got)
	}
}
