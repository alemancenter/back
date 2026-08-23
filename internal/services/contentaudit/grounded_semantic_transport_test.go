package contentaudit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGroundedModelCandidatesQualityUsesProjectDefaultsWhenUnset(t *testing.T) {
	t.Setenv("AI_MODELS_FIX_QUALITY", "")
	t.Setenv("AI_MODELS_FIX_FINAL", "")
	t.Setenv("AI_MODELS_FIX_BALANCED", "")
	t.Setenv("AI_MODELS_FIX_ECONOMY", "")
	t.Setenv("TOGETHER_AI_MODEL", "")
	t.Setenv("TOGETHER_AI_FALLBACK_MODELS", "")

	ctx := WithAIModelStrategy(t.Context(), "quality")
	got := groundedModelCandidatesForContext(ctx)
	wantPrefix := []string{
		"Qwen/Qwen3-235B-A22B-Instruct-2507-tput",
		"openai/gpt-oss-120b",
		"zai-org/GLM-5.1",
	}
	if len(got) < len(wantPrefix) {
		t.Fatalf("not enough quality models: %#v", got)
	}
	for i := range wantPrefix {
		if got[i] != wantPrefix[i] {
			t.Fatalf("unexpected quality model order: %#v", got)
		}
	}
}

func TestGroundedAIJSONRetriesContradictoryFactExtractor(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		content := `{"purpose":"تحضير","audience":["المعلم"],"facts":[{"claim":"يتضمن الملف خطة درس","evidence_ids":["attachment:1:text"],"confidence":95}],"insufficient_source":false,"source_notes":[]}`
		if call == 1 {
			content = `{"purpose":"تحضير","audience":["المعلم"],"facts":[{"claim":"يتضمن الملف خطة درس","evidence_ids":["attachment:1:text"],"confidence":95}],"insufficient_source":true,"source_notes":["conflict"]}`
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{
				"finish_reason": "stop",
				"message":       map[string]string{"content": content},
			}},
		})
	}))
	defer server.Close()

	setGroundedTransportTestEnv(t, server.URL)
	ctx := WithAIModelStrategy(t.Context(), "quality")
	var out groundedFactExtraction
	_, err := groundedAIJSONV3(ctx, "fact_extractor", "system", "user", &out)
	if err != nil {
		t.Fatalf("expected semantic retry to succeed: %v", err)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
	if out.InsufficientSource || len(out.Facts) != 1 {
		t.Fatalf("unexpected fact output: %+v", out)
	}
}

func TestGroundedAIJSONRetriesAllLowConfidenceFacts(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		confidence := 95
		if call == 1 {
			confidence = 1
		}
		content := map[string]interface{}{
			"purpose": "تحضير",
			"audience": []string{"المعلم"},
			"facts": []map[string]interface{}{{
				"claim": "يتضمن الملف خطة درس",
				"evidence_ids": []string{"attachment:1:text"},
				"confidence": confidence,
			}},
			"insufficient_source": false,
			"source_notes": []string{},
		}
		contentBytes, _ := json.Marshal(content)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{
				"finish_reason": "stop",
				"message":       map[string]string{"content": string(contentBytes)},
			}},
		})
	}))
	defer server.Close()

	setGroundedTransportTestEnv(t, server.URL)
	ctx := WithAIModelStrategy(t.Context(), "quality")
	var out groundedFactExtraction
	_, err := groundedAIJSONV3(ctx, "fact_extractor", "system", "user", &out)
	if err != nil {
		t.Fatalf("expected low-confidence semantic retry to succeed: %v", err)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
	if len(out.Facts) != 1 || out.Facts[0].Confidence != 95 {
		t.Fatalf("unexpected fact output: %+v", out)
	}
}

func TestGroundedAIJSONRetriesEmptyValidatorResult(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		content := `{"grounding_score":96,"supported_claims":7,"unsupported_claims":[],"notes":["موثق"]}`
		if call == 1 {
			content = `{"grounding_score":0,"supported_claims":0,"unsupported_claims":[],"notes":[]}`
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{
				"finish_reason": "stop",
				"message":       map[string]string{"content": content},
			}},
		})
	}))
	defer server.Close()

	setGroundedTransportTestEnv(t, server.URL)
	ctx := WithAIModelStrategy(t.Context(), "quality")
	var out groundedValidation
	_, err := groundedAIJSONV3(ctx, "claim_validator", "system", "user", &out)
	if err != nil {
		t.Fatalf("expected validator semantic retry to succeed: %v", err)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
	if out.GroundingScore != 96 || out.SupportedClaims != 7 {
		t.Fatalf("unexpected validator output: %+v", out)
	}
}

func TestValidateGroundedStageOutputRejectsEmptyWriter(t *testing.T) {
	draft := &groundedDraft{Title: "عنوان", ContentHTML: "", UsedFactIndexes: []int{0}}
	if err := validateGroundedStageOutput("grounded_writer", draft); err == nil || !strings.Contains(err.Error(), "empty content") {
		t.Fatalf("expected empty writer content error, got: %v", err)
	}
}

func setGroundedTransportTestEnv(t *testing.T, baseURL string) {
	t.Helper()
	t.Setenv("TOGETHER_AI_API_KEY", "test-key")
	t.Setenv("TOGETHER_AI_BASE_URL", baseURL)
	t.Setenv("AI_MODELS_FIX_QUALITY", "test/model")
	t.Setenv("AI_MODELS_FIX_FINAL", "")
	t.Setenv("AI_MODELS_FIX_BALANCED", "")
	t.Setenv("AI_MODELS_FIX_ECONOMY", "")
	t.Setenv("TOGETHER_AI_MODEL", "")
	t.Setenv("TOGETHER_AI_FALLBACK_MODELS", "")
}
