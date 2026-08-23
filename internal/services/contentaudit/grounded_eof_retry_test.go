package contentaudit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGroundedAIJSONRetriesUnexpectedEOFWithStop(t *testing.T) {
	var calls int32
	var tokenBudgets []int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		var request struct {
			MaxTokens int `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		tokenBudgets = append(tokenBudgets, request.MaxTokens)

		content := `{"purpose":"ناقص"`
		content = strings.ReplaceAll(content, `\"`, `"`)
		if call > 1 {
			content = `{"purpose":"تحضير موثق","audience":["المعلم"],"facts":[{"claim":"المرفق يتضمن خطط دروس للتربية الإسلامية","evidence_ids":["attachment:2761:text"],"confidence":99}],"insufficient_source":false,"source_notes":[]}`
			content = strings.ReplaceAll(content, `\"`, `"`)
		}

		w.Header().Set("Content-Type", "application/json")
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
	model, err := groundedAIJSONV3(ctx, "fact_extractor", "system", "user", &out)
	if err != nil {
		t.Fatalf("expected EOF retry to succeed: %v", err)
	}
	if model != "test/model" {
		t.Fatalf("unexpected model: %s", model)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 attempts, got %d", calls)
	}
	if len(tokenBudgets) != 2 || tokenBudgets[0] != 4096 || tokenBudgets[1] != 5120 {
		t.Fatalf("unexpected token budgets: %#v", tokenBudgets)
	}
	if len(out.Facts) != 1 || out.Facts[0].Confidence != 99 {
		t.Fatalf("unexpected output: %+v", out)
	}
}
