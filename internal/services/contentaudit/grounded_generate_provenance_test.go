package contentaudit

import (
	"encoding/json"
	"testing"
)

func testGeneratedDraft() groundedDraft {
	return groundedDraft{
		Title:           "دليل تعليمي موثق لمراجعة الدرس والاستفادة من الملف",
		ContentHTML:     "<h2>المحور الأول</h2><p>التربية الإسلامية شرح تعليمي موثق من المصدر.</p><h2>المحور الثاني</h2><p>مراجعة الدرس تساعد الطالب على تنظيم التعلم.</p>",
		MetaDescription: "التربية الإسلامية مورد تعليمي يساعد الطالب على مراجعة الدرس والاستفادة من المادة التعليمية بطريقة واضحة ومنظمة اعتمادًا على المصدر المتاح.",
		Keywords:        []string{"التربية الإسلامية", "مراجعة الدرس", "مورد تعليمي", "الطالب", "التعلم"},
		UsedFactIndexes: []int{0},
		QualityScore:    30,
	}
}

func TestRunQualityLoopWithGroundingRetriesUnsupportedDraft(t *testing.T) {
	writes := 0
	validations := 0
	write := func(feedback string) (groundedDraft, error) {
		writes++
		return testGeneratedDraft(), nil
	}
	validate := func(draft groundedDraft) (groundedValidation, error) {
		validations++
		if validations == 1 {
			return groundedValidation{GroundingScore: 82, SupportedClaims: 2, UnsupportedClaims: []string{"ادعاء غير مدعوم"}}, nil
		}
		return groundedValidation{GroundingScore: 96, SupportedClaims: 4}, nil
	}

	_, _, grounding, ok, err := runQualityLoopWithGrounding(write, validate, "عنوان احتياطي", nil, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected a validated draft")
	}
	if writes < 2 || validations < 2 {
		t.Fatalf("expected retry after unsupported claim, writes=%d validations=%d", writes, validations)
	}
	if grounding.GroundingScore != 96 || len(grounding.UnsupportedClaims) != 0 {
		t.Fatalf("unexpected grounding result: %+v", grounding)
	}
}

func TestRunQualityLoopWithGroundingFailsClosed(t *testing.T) {
	write := func(feedback string) (groundedDraft, error) {
		return testGeneratedDraft(), nil
	}
	validate := func(draft groundedDraft) (groundedValidation, error) {
		return groundedValidation{GroundingScore: 70, UnsupportedClaims: []string{"غير مدعوم"}}, nil
	}

	_, _, _, ok, err := runQualityLoopWithGrounding(write, validate, "عنوان احتياطي", nil, 1)
	if err == nil {
		t.Fatal("expected fail-closed grounding error")
	}
	if ok {
		t.Fatal("unsupported drafts must never be accepted")
	}
}

func TestRunQualityLoopWithGroundingRejectsInvalidFactIndexes(t *testing.T) {
	validations := 0
	write := func(feedback string) (groundedDraft, error) {
		draft := testGeneratedDraft()
		draft.UsedFactIndexes = []int{9}
		return draft, nil
	}
	validate := func(draft groundedDraft) (groundedValidation, error) {
		validations++
		return groundedValidation{GroundingScore: 100}, nil
	}

	_, _, _, ok, err := runQualityLoopWithGrounding(write, validate, "عنوان احتياطي", nil, 1)
	if err == nil || ok {
		t.Fatalf("invalid fact indexes must fail closed, ok=%v err=%v", ok, err)
	}
	if validations != 0 {
		t.Fatalf("validator should not run for structurally invalid fact indexes; got %d calls", validations)
	}
}

func TestGroundedGenerationResultJSONKeepsProvenanceTopLevel(t *testing.T) {
	draft := testGeneratedDraft()
	quality := scoreSEOQuality(draft)
	grounding := groundedValidation{GroundingScore: 97, SupportedClaims: 4}
	result := buildGenerationResult(draft, quality, &grounding, GenerationSourceModeGroundedFile, true, "", quality.issues())

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if payload["source_mode"] != GenerationSourceModeGroundedFile {
		t.Fatalf("source_mode missing or wrong: %#v", payload["source_mode"])
	}
	if payload["grounding_verified"] != true {
		t.Fatalf("expected grounding_verified=true, got %#v", payload["grounding_verified"])
	}
	if payload["content_html"] == nil || payload["draft_quality_score"] == nil {
		t.Fatalf("embedded article/provenance fields were not flattened: %s", string(raw))
	}
}

func TestFallbackGenerationResultDisclosesUnreadableSource(t *testing.T) {
	draft := testGeneratedDraft()
	quality := scoreSEOQuality(draft)
	warning := "تعذر استخراج نص موثوق من الملف"
	result := buildGenerationResult(draft, quality, nil, GenerationSourceModeGeneralKnowledgeFallback, false, warning, quality.issues())

	if result.SourceMode != GenerationSourceModeGeneralKnowledgeFallback || result.SourceFileReadable {
		t.Fatalf("unexpected fallback provenance: %+v", result)
	}
	if result.GroundingScore != nil || result.GroundingVerified {
		t.Fatalf("fallback must never claim grounding: %+v", result)
	}
	if result.SourceWarning == "" || len(result.SEOIssues) == 0 || result.SEOIssues[0] != warning {
		t.Fatalf("fallback warning must be explicit and backwards-compatible: %+v", result)
	}
}
