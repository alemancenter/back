package contentaudit

import (
	"strings"
	"testing"

	"github.com/imanjo/fiber-api/internal/models"
)

func TestParseGroundingSummary(t *testing.T) {
	summary := "[GROUNDING_V2] status=grounded score=96 evidence=4 unsupported=0 model=test/model prompt=grounded-content-repair-v2\nموثق"
	meta, ok := parseGroundingSummary(summary)
	if !ok {
		t.Fatal("expected grounding summary to parse")
	}
	if meta.Status != "grounded" || meta.Score != 96 || meta.Evidence != 4 || meta.Unsupported != 0 || meta.Model != "test/model" {
		t.Fatalf("unexpected meta: %+v", meta)
	}
}

func TestParseGroundingSummaryRejectsLegacyPreview(t *testing.T) {
	if _, ok := parseGroundingSummary("تم إنشاء نسخة موسعة ومحسنة"); ok {
		t.Fatal("legacy preview must not be treated as grounded")
	}
}

func TestBuildGroundedSourcePackIncludesAttachmentMetadata(t *testing.T) {
	category := "تحضير"
	content := &groundedLoadedContent{
		Type:              "article",
		ID:                169,
		CountryCode:       "jo",
		Title:             "تحضير التربية الاسلامية للصف اول ثانوي اكاديمي",
		Content:           "<p>وصف مختصر للمادة المرفقة.</p>",
		CurriculumContext: "الأردن | الصف الأول الثانوي | التربية الإسلامية",
		Files: []models.File{{
			ID:           7,
			FileName:     "تحضير-تربية-اسلامية.pdf",
			FileType:     "pdf",
			FileCategory: &category,
			MimeType:     "application/pdf",
			FileSize:     2048,
		}},
	}
	pack := buildGroundedSourcePack(content)
	if len(pack.Evidence) < 4 {
		t.Fatalf("expected title, body, curriculum and attachment metadata evidence; got %d", len(pack.Evidence))
	}
	var found bool
	for _, evidence := range pack.Evidence {
		if evidence.ID == "attachment:7:meta" {
			found = true
			if !strings.Contains(evidence.Text, "تحضير-تربية-اسلامية.pdf") || !strings.Contains(evidence.Text, "application/pdf") {
				t.Fatalf("attachment metadata missing expected fields: %q", evidence.Text)
			}
		}
	}
	if !found {
		t.Fatal("attachment metadata evidence not found")
	}
}

func TestSanitizeGroundedFactsDropsUnknownEvidence(t *testing.T) {
	pack := groundedSourcePack{Evidence: []groundedEvidence{{ID: "content:title", Verified: true}}}
	in := groundedFactExtraction{Facts: []groundedFact{
		{Claim: "حقيقة صحيحة", EvidenceIDs: []string{"content:title"}, Confidence: 90},
		{Claim: "حقيقة بلا مصدر", EvidenceIDs: []string{"missing"}, Confidence: 99},
	}}
	out := sanitizeGroundedFacts(in, pack)
	if len(out.Facts) != 1 {
		t.Fatalf("expected one grounded fact, got %d", len(out.Facts))
	}
	if out.Facts[0].Claim != "حقيقة صحيحة" {
		t.Fatalf("unexpected fact kept: %+v", out.Facts[0])
	}
}

func TestValidUsedFactIndexes(t *testing.T) {
	if !validUsedFactIndexes([]int{0, 2}, 3) {
		t.Fatal("valid indexes should pass")
	}
	if validUsedFactIndexes(nil, 3) {
		t.Fatal("writer must cite at least one fact")
	}
	if validUsedFactIndexes([]int{3}, 3) {
		t.Fatal("out-of-range fact index must fail")
	}
}
