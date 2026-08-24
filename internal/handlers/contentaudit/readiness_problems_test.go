package contentaudit

import (
	"testing"

	"github.com/imanjo/fiber-api/internal/contentquality"
	auditservice "github.com/imanjo/fiber-api/internal/services/contentaudit"
)

func TestClassifyReadinessProblemsKeepsAuditAndEditorialSignalsSeparate(t *testing.T) {
	diagnostics := contentquality.Diagnostics{WordCount: 75, FilesCount: 0}
	gate := auditservice.ContentQualityGate{Audited: false, Indexable: true, AdsEligible: false}

	problems := classifyReadinessProblems("عنوان قصير", "", diagnostics, true, gate)
	item := unifiedReadinessItem{Problems: problems}

	for _, expected := range []string{
		readinessProblemUnaudited,
		readinessProblemThinContent,
		readinessProblemMetaDescription,
		readinessProblemShortTitle,
	} {
		if !hasReadinessProblem(item, expected) {
			t.Fatalf("expected problem %q in %#v", expected, problems)
		}
	}
	if hasReadinessProblem(item, readinessProblemAdsNotEligible) {
		t.Fatal("unaudited content must be routed to analysis, not a post-audit eligibility repair")
	}
}

func TestClassifyReadinessProblemsSurfacesPolicyBlockFirst(t *testing.T) {
	diagnostics := contentquality.Diagnostics{WordCount: 400, FilesCount: 1}
	gate := auditservice.ContentQualityGate{Audited: true, Indexable: false, AdsEligible: false}

	problems := classifyReadinessProblems("عنوان تعليمي واضح وطويل بما يكفي", string(make([]rune, 90)), diagnostics, true, gate)
	if len(problems) == 0 || problems[0].Code != readinessProblemPolicyBlocked {
		t.Fatalf("expected policy block to be the primary problem, got %#v", problems)
	}
}

func TestReadinessRepairCollectorCountsAffectedItemsOnce(t *testing.T) {
	collector := newReadinessRepairCollector()
	collector.Add(unifiedReadinessItem{Problems: []readinessItemProblem{
		{Code: readinessProblemThinContent, ActionType: "ai_preview"},
		{Code: readinessProblemMetaDescription, ActionType: "ai_preview"},
	}})
	collector.Add(unifiedReadinessItem{Problems: []readinessItemProblem{
		{Code: readinessProblemUnpublished, ActionType: "manual"},
	}})

	result := collector.Build()
	if result.AffectedItems != 2 || result.ActionableItems != 1 || result.ManualItems != 1 {
		t.Fatalf("unexpected repair totals: %#v", result)
	}
	if result.TotalFindings != 3 {
		t.Fatalf("expected three findings, got %d", result.TotalFindings)
	}
}

func TestNormalizeQualityBatchRequestBoundsExplicitTargets(t *testing.T) {
	req := normalizeQualityBatchRequest(contentQualityBatchRequest{
		Preset: "weak_first",
		Targets: []contentQualityBatchTarget{
			{ContentType: "article", ContentID: 12},
			{ContentType: "article", ContentID: 12},
			{ContentType: "post", ContentID: 9},
			{ContentType: "invalid", ContentID: 1},
		},
	})
	if req.Preset != "selected_items" || req.Limit != 2 || len(req.Targets) != 2 {
		t.Fatalf("explicit targets were not normalized: %#v", req)
	}
}

func TestSelectedQualityTargetsMatchTypeAndID(t *testing.T) {
	req := contentQualityBatchRequest{
		Preset: "selected_items",
		Targets: []contentQualityBatchTarget{{ContentType: "article", ContentID: 7}},
	}
	if !shouldIncludeQualityTarget(unifiedReadinessItem{Type: "article", ID: 7}, req) {
		t.Fatal("selected article must be included")
	}
	if shouldIncludeQualityTarget(unifiedReadinessItem{Type: "post", ID: 7}, req) {
		t.Fatal("same numeric ID from a different content type must not be included")
	}
}
