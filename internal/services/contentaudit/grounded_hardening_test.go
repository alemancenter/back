package contentaudit

import (
	"archive/zip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/imanjo/fiber-api/internal/fileextract"
)

func TestRepairGroundedJSONControlCharsRawNewline(t *testing.T) {
	raw := "{\"source_notes\":\"line one\nline two\",\"facts\":[],\"audience\":[],\"insufficient_source\":true}"
	if json.Valid([]byte(raw)) {
		t.Fatal("fixture must be invalid JSON before repair")
	}
	repaired := repairGroundedJSONControlChars(raw)
	if !json.Valid([]byte(repaired)) {
		t.Fatalf("repaired output is still invalid JSON: %q", repaired)
	}
}

func TestRepairGroundedJSONControlCharsPreservesValidEscapes(t *testing.T) {
	raw := `{"source_notes":"line one\nline two","facts":[],"audience":[],"insufficient_source":true}`
	raw = strings.ReplaceAll(raw, `\"`, `"`)
	if !json.Valid([]byte(raw)) {
		t.Fatal("fixture should be valid JSON")
	}
	if repaired := repairGroundedJSONControlChars(raw); repaired != raw {
		t.Fatalf("valid escaped JSON changed:\nwant: %q\ngot:  %q", raw, repaired)
	}
}

func TestRepairGroundedJSONControlCharsDoesNotHideStructuralErrors(t *testing.T) {
	raw := `{"purpose":"x" "facts":[]}`
	raw = strings.ReplaceAll(raw, `\"`, `"`)
	if json.Valid([]byte(repairGroundedJSONControlChars(raw))) {
		t.Fatal("structural JSON error must not be silently repaired")
	}
}

func TestGroundedModelCandidatesQualityPrefersQualityModels(t *testing.T) {
	t.Setenv("AI_MODELS_FIX_QUALITY", "quality/model-a,quality/model-b")
	t.Setenv("AI_MODELS_FIX_FINAL", "final/model")
	t.Setenv("TOGETHER_AI_MODEL", "generic/model")
	ctx := WithAIModelStrategy(t.Context(), "quality")
	models := groundedModelCandidatesForContext(ctx)
	want := []string{"quality/model-a", "quality/model-b", "final/model", "generic/model"}
	if len(models) < len(want) {
		t.Fatalf("not enough models: %#v", models)
	}
	for i := range want {
		if models[i] != want[i] {
			t.Fatalf("unexpected quality ordering: %#v", models)
		}
	}
}

func TestGroundedAIJSONRetriesTruncatedResponse(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		content := `{"purpose":"خطة درس","audience":["المعلم"],"facts":[{"claim":"حقيقة موثقة","evidence_ids":["attachment:1:text"],"confidence":98}],"insufficient_source":false,"source_notes":[]}`
		content = strings.ReplaceAll(content, `\"`, `"`)
		finish := "stop"
		if call == 1 {
			content = `{"purpose":"truncated"`
			content = strings.ReplaceAll(content, `\"`, `"`)
			finish = "length"
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{
				"finish_reason": finish,
				"message":       map[string]string{"content": content},
			}},
		})
	}))
	defer server.Close()

	t.Setenv("TOGETHER_AI_API_KEY", "test-key")
	t.Setenv("TOGETHER_AI_BASE_URL", server.URL)
	t.Setenv("AI_MODELS_FIX_QUALITY", "test/model")
	t.Setenv("AI_MODELS_FIX_FINAL", "")
	t.Setenv("TOGETHER_AI_MODEL", "")

	ctx := WithAIModelStrategy(t.Context(), "quality")
	var out groundedFactExtraction
	model, err := groundedAIJSONV3(ctx, "fact_extractor", "system", "user", &out)
	if err != nil {
		t.Fatalf("expected retry to succeed: %v", err)
	}
	if model != "test/model" {
		t.Fatalf("unexpected model: %s", model)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected exactly 2 calls, got %d", calls)
	}
	if len(out.Facts) != 1 || out.Facts[0].Claim != "حقيقة موثقة" {
		t.Fatalf("unexpected decoded output: %+v", out)
	}
}

func TestGroundedAIJSONMalformedForeverReturnsInvalidOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		content := `{"purpose":"broken"`
		content = strings.ReplaceAll(content, `\"`, `"`)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{
				"finish_reason": "stop",
				"message":       map[string]string{"content": content},
			}},
		})
	}))
	defer server.Close()

	t.Setenv("TOGETHER_AI_API_KEY", "test-key")
	t.Setenv("TOGETHER_AI_BASE_URL", server.URL)
	t.Setenv("AI_MODELS_FIX_QUALITY", "test/model")
	t.Setenv("AI_MODELS_FIX_FINAL", "")
	t.Setenv("TOGETHER_AI_MODEL", "")

	ctx := WithAIModelStrategy(t.Context(), "quality")
	var out groundedFactExtraction
	_, err := groundedAIJSONV3(ctx, "fact_extractor", "system", "user", &out)
	if err == nil {
		t.Fatal("expected malformed output to fail")
	}
	if !strings.Contains(err.Error(), ErrGroundedAIInvalidOutput.Error()) || !strings.Contains(err.Error(), "stage=fact_extractor") {
		t.Fatalf("expected precise invalid-output diagnostic, got: %v", err)
	}
}

func TestBuildGroundedSourcePackTreatsCurrentBodyAsContextOnly(t *testing.T) {
	content := &groundedLoadedContent{
		Type:        "article",
		ID:          169,
		CountryCode: "jo",
		Title:       "عنوان",
		Content:     "<p>ادعاء قديم غير موثوق</p>",
	}
	pack := buildGroundedSourcePack(content)
	var body *groundedEvidence
	for i := range pack.Evidence {
		if pack.Evidence[i].ID == "content:body" {
			body = &pack.Evidence[i]
			break
		}
	}
	if body == nil {
		t.Fatal("expected current body context evidence")
	}
	if body.Verified {
		t.Fatal("current body must not be trusted as verified grounding evidence")
	}
	if body.Kind != "body_context" {
		t.Fatalf("unexpected body evidence kind: %s", body.Kind)
	}

	facts := sanitizeGroundedFacts(groundedFactExtraction{Facts: []groundedFact{{
		Claim:       "ادعاء من النص القديم فقط",
		EvidenceIDs: []string{"content:body"},
		Confidence:  99,
	}}}, pack)
	if len(facts.Facts) != 0 || !facts.InsufficientSource {
		t.Fatalf("body-only fact must be rejected: %+v", facts)
	}
}

func TestExtractDOCXTextReadsPast64KiBRawXML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large-prefix.docx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	part, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	prefix := strings.Repeat("x", 80*1024)
	text := strings.Repeat("خطة درس التربية الإسلامية والنتاجات التعليمية ودور المعلم ودور المتعلم. ", 400)
	xmlText := `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><!--` + prefix + `--><w:p><w:r><w:t>` + text + `</w:t></w:r></w:p></w:body></w:document>`
	xmlText = strings.ReplaceAll(xmlText, `\"`, `"`)
	if _, err := part.Write([]byte(xmlText)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	extracted, err := fileextract.ExtractDOCXText(path)
	if err != nil {
		t.Fatalf("extract DOCX: %v", err)
	}
	if strings.Contains(extracted, "<w:") {
		t.Fatal("WordprocessingML leaked into extracted text")
	}
	if !strings.Contains(extracted, "خطة درس التربية الإسلامية") {
		t.Fatalf("expected text after >64 KiB raw XML prefix, got %q", extracted[:minLocal(len(extracted), 200)])
	}
	if len([]rune(extracted)) < 10000 {
		t.Fatalf("expected substantial extracted evidence, got %d runes", len([]rune(extracted)))
	}
	if len([]rune(extracted)) > groundingEvidenceLimit {
		t.Fatalf("extractor exceeded evidence limit: %d", len([]rune(extracted)))
	}
}

func minLocal(a, b int) int {
	if a < b {
		return a
	}
	return b
}
