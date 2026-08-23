package contentaudit

import (
	"encoding/json"
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

func TestGroundedWriterAcceptsSingleFactIndex(t *testing.T) {
	var got groundedDraft
	if err := json.Unmarshal([]byte(`{"title":"عنوان","content_html":"<p>نص</p>","used_fact_indexes":"0"}`), &got); err != nil {
		t.Fatalf("single writer fact index should decode: %v", err)
	}
	if len(got.UsedFactIndexes) != 1 || got.UsedFactIndexes[0] != 0 {
		t.Fatalf("unexpected indexes: %#v", got.UsedFactIndexes)
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
