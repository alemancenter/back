package contentaudit

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGroundedFactExtractionAcceptsScalarStringFields(t *testing.T) {
	raw := []byte(`{
		"purpose":"وصف التحضير",
		"audience":"طلبة الصف الحادي عشر",
		"facts":[{
			"claim":"يتضمن الملف خطة درس للتربية الإسلامية",
			"evidence_ids":"attachment:2761:text",
			"confidence":98
		}],
		"insufficient_source":"false",
		"source_notes":"تم استخراج النص من ملف DOCX"
	}`)

	var got groundedFactExtraction
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("scalar-compatible grounded JSON should decode: %v", err)
	}
	if len(got.Audience) != 1 || got.Audience[0] != "طلبة الصف الحادي عشر" {
		t.Fatalf("unexpected audience: %#v", got.Audience)
	}
	if len(got.SourceNotes) != 1 || got.SourceNotes[0] != "تم استخراج النص من ملف DOCX" {
		t.Fatalf("unexpected source notes: %#v", got.SourceNotes)
	}
	if got.InsufficientSource {
		t.Fatal("string false must decode as false")
	}
	if len(got.Facts) != 1 || len(got.Facts[0].EvidenceIDs) != 1 || got.Facts[0].EvidenceIDs[0] != "attachment:2761:text" {
		t.Fatalf("unexpected facts: %#v", got.Facts)
	}
}

func TestGroundedFactExtractionStillAcceptsCanonicalArrays(t *testing.T) {
	raw := []byte(`{
		"purpose":"وصف التحضير",
		"audience":["المعلمون","الطلبة"],
		"facts":[{
			"claim":"حقيقة موثقة",
			"evidence_ids":["content:title","attachment:2761:text"],
			"confidence":95
		}],
		"insufficient_source":false,
		"source_notes":["ملاحظة 1","ملاحظة 2"]
	}`)

	var got groundedFactExtraction
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("canonical grounded JSON should decode: %v", err)
	}
	if len(got.Audience) != 2 || len(got.SourceNotes) != 2 || len(got.Facts[0].EvidenceIDs) != 2 {
		t.Fatalf("canonical arrays were not preserved: %#v", got)
	}
}

func TestGroundedFactExtractionAcceptsIntegerValuedDecimalConfidence(t *testing.T) {
	raw := []byte(`{
		"purpose":"وصف التحضير",
		"audience":["المعلم"],
		"facts":[{
			"claim":"حقيقة موثقة",
			"evidence_ids":["attachment:2761:text"],
			"confidence":1.0
		}],
		"insufficient_source":false,
		"source_notes":[]
	}`)

	var got groundedFactExtraction
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("integer-valued decimal confidence should decode: %v", err)
	}
	if len(got.Facts) != 1 || got.Facts[0].Confidence != 1 {
		t.Fatalf("unexpected decimal confidence normalization: %#v", got.Facts)
	}
}

func TestGroundedFactExtractionAcceptsDecimalStringConfidence(t *testing.T) {
	var got groundedFactExtraction
	if err := json.Unmarshal([]byte(`{
		"purpose":"وصف التحضير",
		"audience":["المعلم"],
		"facts":[{
			"claim":"حقيقة موثقة",
			"evidence_ids":["attachment:2761:text"],
			"confidence":"98.0"
		}],
		"insufficient_source":false,
		"source_notes":[]
	}`), &got); err != nil {
		t.Fatalf("decimal string confidence should decode: %v", err)
	}
	if got.Facts[0].Confidence != 98 {
		t.Fatalf("expected confidence 98, got %d", got.Facts[0].Confidence)
	}
}

func TestGroundedFactExtractionRejectsFractionalConfidence(t *testing.T) {
	raw := []byte(`{
		"purpose":"وصف التحضير",
		"audience":["المعلم"],
		"facts":[{
			"claim":"حقيقة موثقة",
			"evidence_ids":["attachment:2761:text"],
			"confidence":98.5
		}],
		"insufficient_source":false,
		"source_notes":[]
	}`)

	var got groundedFactExtraction
	err := json.Unmarshal(raw, &got)
	if err == nil {
		t.Fatal("fractional confidence must remain invalid")
	}
	if !strings.Contains(err.Error(), "confidence") || !strings.Contains(err.Error(), "fractional") {
		t.Fatalf("unexpected fractional confidence error: %v", err)
	}
}

func TestGroundedWriterAcceptsSingleFactIndex(t *testing.T) {
	var got groundedDraft
	if err := json.Unmarshal([]byte(`{"title":"عنوان","content_html":"<p>نص</p>","used_fact_indexes":"0"}`), &got); err != nil {
		t.Fatalf("single writer fact index should decode: %v", err)
	}
	if len(got.UsedFactIndexes) != 1 || got.UsedFactIndexes[0] != 0 {
		t.Fatalf("unexpected indexes: %#v", got.UsedFactIndexes)
	}
}

func TestGroundedWriterAcceptsMixedIntegerValuedFactIndexes(t *testing.T) {
	var got groundedDraft
	if err := json.Unmarshal([]byte(`{"title":"عنوان","content_html":"<p>نص</p>","used_fact_indexes":[0.0,"1.0",2]}`), &got); err != nil {
		t.Fatalf("integer-valued mixed writer indexes should decode: %v", err)
	}
	want := []int{0, 1, 2}
	if len(got.UsedFactIndexes) != len(want) {
		t.Fatalf("unexpected indexes: %#v", got.UsedFactIndexes)
	}
	for i := range want {
		if got.UsedFactIndexes[i] != want[i] {
			t.Fatalf("unexpected indexes: %#v", got.UsedFactIndexes)
		}
	}
}

func TestGroundedValidatorAcceptsScalarNotes(t *testing.T) {
	var got groundedValidation
	if err := json.Unmarshal([]byte(`{
		"grounding_score":96,
		"supported_claims":7,
		"unsupported_claims":[],
		"notes":"جميع الادعاءات مدعومة"
	}`), &got); err != nil {
		t.Fatalf("scalar validator notes should decode: %v", err)
	}
	if len(got.Notes) != 1 || got.Notes[0] != "جميع الادعاءات مدعومة" {
		t.Fatalf("unexpected notes: %#v", got.Notes)
	}
	if len(got.UnsupportedClaims) != 0 {
		t.Fatalf("unexpected unsupported claims: %#v", got.UnsupportedClaims)
	}
}

func TestGroundedValidatorAcceptsIntegerValuedDecimalCounts(t *testing.T) {
	var got groundedValidation
	if err := json.Unmarshal([]byte(`{
		"grounding_score":96.0,
		"supported_claims":"7.0",
		"unsupported_claims":[],
		"notes":[]
	}`), &got); err != nil {
		t.Fatalf("integer-valued decimal validator fields should decode: %v", err)
	}
	if got.GroundingScore != 96 || got.SupportedClaims != 7 {
		t.Fatalf("unexpected validator numeric normalization: %+v", got)
	}
}

func TestGroundedValidatorAcceptsSupportedClaimTextArray(t *testing.T) {
	var got groundedValidation
	if err := json.Unmarshal([]byte(`{
		"grounding_score":97,
		"supported_claims":[
			"العنوان يوضح أن الملف تحضير للتربية الإسلامية.",
			"النص المستخرج يذكر الصف الحادي عشر.",
			"يتضمن الملف خطة درس حول سورة آل عمران."
		],
		"unsupported_claims":[],
		"notes":[]
	}`), &got); err != nil {
		t.Fatalf("supported claim text arrays should normalize to a count: %v", err)
	}
	if got.SupportedClaims != 3 {
		t.Fatalf("expected 3 supported claims, got %d", got.SupportedClaims)
	}
}

func TestGroundedValidatorCountsDistinctNonEmptySupportedClaims(t *testing.T) {
	var got groundedValidation
	if err := json.Unmarshal([]byte(`{
		"grounding_score":92,
		"supported_claims":["title","content_html","title","   "],
		"unsupported_claims":[],
		"notes":[]
	}`), &got); err != nil {
		t.Fatalf("supported claim labels should normalize to a distinct count: %v", err)
	}
	if got.SupportedClaims != 2 {
		t.Fatalf("expected 2 distinct supported claims, got %d", got.SupportedClaims)
	}
}

func TestGroundedValidatorRejectsNonNumericScalarSupportedClaimText(t *testing.T) {
	var got groundedValidation
	err := json.Unmarshal([]byte(`{
		"grounding_score":92,
		"supported_claims":"هذا ادعاء مدعوم",
		"unsupported_claims":[],
		"notes":[]
	}`), &got)
	if err == nil {
		t.Fatal("non-numeric scalar supported_claims must remain invalid")
	}
	if !strings.Contains(err.Error(), "supported_claims") || !strings.Contains(err.Error(), "invalid integer value") {
		t.Fatalf("unexpected supported_claims scalar error: %v", err)
	}
}
