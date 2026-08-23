package contentaudit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"
)

var ErrGroundedAIInvalidOutput = errors.New("grounded repair AI returned unusable structured output")

const groundedAttemptsPerModel = 2

func groundedStageMaxTokens(stage string, attempt int) int {
	base := 4096
	switch stage {
	case "claim_validator":
		base = 2048
	case "grounded_writer":
		base = 4096
	case "fact_extractor":
		base = 4096
	}
	if attempt > 0 {
		base += 1024
	}
	return base
}

func groundedParseModelList(raw string) []string {
	parts := strings.Split(raw, ",")
	models := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			models = append(models, part)
		}
	}
	return compactStrings(models)
}

func groundedModelRoute(envKey, defaults string) []string {
	if configured := groundedParseModelList(os.Getenv(envKey)); len(configured) > 0 {
		return configured
	}
	return groundedParseModelList(defaults)
}

func groundedModelCandidatesForContext(ctx context.Context) []string {
	strategy := aiModelStrategyFromContext(ctx)
	generic := firstNonEmptyLocal(os.Getenv("TOGETHER_AI_MODEL"), "deepseek-ai/DeepSeek-V4-Pro")
	fallbacks := groundedParseModelList(os.Getenv("TOGETHER_AI_FALLBACK_MODELS"))
	if len(fallbacks) == 0 {
		fallbacks = groundedParseModelList("openai/gpt-oss-20b,google/gemma-3n-E4B-it")
	}
	base := append([]string{generic}, fallbacks...)

	quality := groundedModelRoute("AI_MODELS_FIX_QUALITY", "Qwen/Qwen3-235B-A22B-Instruct-2507-tput,openai/gpt-oss-120b,zai-org/GLM-5.1")
	final := groundedModelRoute("AI_MODELS_FIX_FINAL", "Qwen/Qwen3-235B-A22B-Instruct-2507-tput,zai-org/GLM-5.1,openai/gpt-oss-120b")
	balanced := groundedModelRoute("AI_MODELS_FIX_BALANCED", "meta-llama/Llama-3.3-70B-Instruct-Turbo,pearl-ai/gemma-4-31b-it,google/gemma-4-31B-it")
	economy := groundedModelRoute("AI_MODELS_FIX_ECONOMY", "google/gemma-3n-E4B-it,openai/gpt-oss-20b")

	ordered := []string{}
	switch strategy {
	case "final_review":
		ordered = append(ordered, final...)
		ordered = append(ordered, quality...)
	case "economy":
		ordered = append(ordered, economy...)
		ordered = append(ordered, balanced...)
		ordered = append(ordered, quality...)
		ordered = append(ordered, final...)
	case "balanced":
		ordered = append(ordered, balanced...)
		ordered = append(ordered, quality...)
		ordered = append(ordered, final...)
	case "quality", "":
		ordered = append(ordered, quality...)
		ordered = append(ordered, final...)
	default:
		ordered = append(ordered, quality...)
		ordered = append(ordered, final...)
	}
	ordered = append(ordered, base...)
	return compactStrings(ordered)
}

func groundedRetryInstruction(stage string) string {
	switch stage {
	case "fact_extractor":
		return "\n\nالمحاولة السابقة كانت ناقصة أو غير صالحة بنيويًا أو دلاليًا. أعد JSON قصيرًا وصحيحًا فقط بلا Markdown. حد أقصى 10 حقائق. confidence عدد صحيح من 0 إلى 100 وليس مقياس 0-1. لا تستنتج رقم الصف من content_id أو evidence_ids أو أرقام داخلية. استخدم اسم الصف الحرفي الموجود في الأدلة مثل «الصف الأول الثانوي» كما هو. إذا أعدت facts موثقة فلا تجعل insufficient_source=true. source_notes بحد أقصى عنصرين ولا تضع فيها تفكيرًا داخليًا أو استنتاجات غير موجودة في الأدلة."
	case "grounded_writer":
		return "\n\nالمحاولة السابقة كانت ناقصة أو غير صالحة دلاليًا. أعد JSON صحيحًا ومختصرًا فقط، بدون Markdown خارج content_html وبدون حشو. يجب أن يكون content_html غير فارغ وأن تحتوي used_fact_indexes على فهرس حقيقة صالح واحد على الأقل."
	case "claim_validator":
		return "\n\nالمحاولة السابقة كانت ناقصة أو غير صالحة دلاليًا. أعد JSON صحيحًا ومختصرًا فقط. لا تُرجع نتيجة فارغة من نوع grounding_score=0 وsupported_claims=0 وunsupported_claims=[] عندما توجد مسودة للمراجعة. احسب النتيجة فعليًا واجعل notes مختصرة."
	default:
		return "\n\nالمحاولة السابقة كانت ناقصة أو JSON غير صالح. أعد JSON صحيحًا ومختصرًا فقط."
	}
}

func decodeGroundedAIJSON(raw string, out interface{}) error {
	raw = cleanGroundedJSON(raw)
	raw = repairGroundedJSONControlChars(raw)
	if strings.TrimSpace(raw) == "" {
		return errors.New("empty JSON content")
	}

	value := reflect.ValueOf(out)
	if value.Kind() != reflect.Ptr || value.IsNil() {
		return errors.New("grounded JSON target must be a non-nil pointer")
	}
	candidate := reflect.New(value.Elem().Type())
	if err := json.Unmarshal([]byte(raw), candidate.Interface()); err != nil {
		return err
	}
	value.Elem().Set(candidate.Elem())
	return nil
}

func validateGroundedStageOutput(stage string, out interface{}) error {
	switch value := out.(type) {
	case *groundedFactExtraction:
		if stage != "fact_extractor" {
			return nil
		}
		if value.InsufficientSource && len(value.Facts) > 0 {
			return errors.New("contradictory insufficient_source=true with extracted facts")
		}
		if !value.InsufficientSource && len(value.Facts) == 0 {
			return errors.New("no facts returned while insufficient_source=false")
		}
		maxConfidence := -1
		for _, fact := range value.Facts {
			if strings.TrimSpace(fact.Claim) == "" {
				return errors.New("fact has empty claim")
			}
			if len(fact.EvidenceIDs) == 0 {
				return errors.New("fact has no evidence_ids")
			}
			if fact.Confidence < 0 || fact.Confidence > 100 {
				return fmt.Errorf("fact confidence out of range: %d", fact.Confidence)
			}
			if fact.Confidence > maxConfidence {
				maxConfidence = fact.Confidence
			}
		}
		if len(value.Facts) > 0 && maxConfidence < 60 {
			return fmt.Errorf("all extracted facts have low confidence: max=%d", maxConfidence)
		}

	case *groundedDraft:
		if stage != "grounded_writer" {
			return nil
		}
		if strings.TrimSpace(normalizePlainText(value.ContentHTML)) == "" {
			return errors.New("writer returned empty content")
		}
		if len(value.UsedFactIndexes) == 0 {
			return errors.New("writer returned no used_fact_indexes")
		}

	case *groundedValidation:
		if stage != "claim_validator" {
			return nil
		}
		if value.GroundingScore < 0 || value.GroundingScore > 100 {
			return fmt.Errorf("validator grounding_score out of range: %d", value.GroundingScore)
		}
		if value.SupportedClaims < 0 {
			return fmt.Errorf("validator supported_claims out of range: %d", value.SupportedClaims)
		}
		if value.GroundingScore == 0 && value.SupportedClaims == 0 && len(value.UnsupportedClaims) == 0 {
			return errors.New("validator returned semantically empty result")
		}
	}
	return nil
}

func groundedAIJSONV3(ctx context.Context, stage, systemPrompt, userPrompt string, out interface{}) (string, error) {
	apiKey := firstNonEmptyLocal(os.Getenv("TOGETHER_AI_API_KEY"), os.Getenv("TOGETHER_AI_KEY"), os.Getenv("TOGETHER_API_KEY"))
	if apiKey == "" {
		return "", ErrGroundedAIUnavailable
	}
	baseURL := strings.TrimRight(firstNonEmptyLocal(os.Getenv("TOGETHER_AI_BASE_URL"), "https://api.together.ai/v1"), "/")
	models := groundedModelCandidatesForContext(ctx)
	if len(models) == 0 {
		return "", ErrGroundedAIUnavailable
	}

	attemptErrors := []string{}
	hadSuccessfulHTTP := false

	for _, model := range models {
		for attempt := 0; attempt < groundedAttemptsPerModel; attempt++ {
			prompt := userPrompt
			if attempt > 0 {
				prompt += groundedRetryInstruction(stage)
			}

			payload := map[string]interface{}{
				"model": model,
				"messages": []map[string]string{
					{"role": "system", "content": systemPrompt},
					{"role": "user", "content": prompt},
				},
				"temperature": 0.05,
				"top_p":       0.8,
				"max_tokens":  groundedStageMaxTokens(stage, attempt),
				"response_format": map[string]interface{}{
					"type": "json_object",
				},
			}
			body, err := json.Marshal(payload)
			if err != nil {
				return "", err
			}

			requestCtx, cancel := context.WithTimeout(ctx, 65*time.Second)
			req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
			if err != nil {
				cancel()
				return "", err
			}
			req.Header.Set("Authorization", "Bearer "+apiKey)
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				cancel()
				attemptErrors = append(attemptErrors, fmt.Sprintf("%s attempt=%d transport=%v", model, attempt+1, err))
				break
			}
			responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
			resp.Body.Close()
			cancel()
			if readErr != nil {
				attemptErrors = append(attemptErrors, fmt.Sprintf("%s attempt=%d read=%v", model, attempt+1, readErr))
				break
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				attemptErrors = append(attemptErrors, fmt.Sprintf("%s attempt=%d HTTP=%d", model, attempt+1, resp.StatusCode))
				break
			}
			hadSuccessfulHTTP = true

			var completion struct {
				Choices []struct {
					FinishReason string `json:"finish_reason"`
					Message      struct {
						Content string `json:"content"`
					} `json:"message"`
				} `json:"choices"`
			}
			if err := json.Unmarshal(responseBody, &completion); err != nil || len(completion.Choices) == 0 {
				if err == nil {
					err = errors.New("response has no choices")
				}
				attemptErrors = append(attemptErrors, fmt.Sprintf("%s attempt=%d envelope=%v", model, attempt+1, err))
				continue
			}

			choice := completion.Choices[0]
			if strings.EqualFold(strings.TrimSpace(choice.FinishReason), "length") {
				attemptErrors = append(attemptErrors, fmt.Sprintf("%s attempt=%d finish_reason=length", model, attempt+1))
				continue
			}
			if err := decodeGroundedAIJSON(choice.Message.Content, out); err != nil {
				attemptErrors = append(attemptErrors, fmt.Sprintf("%s attempt=%d invalid_json=%v", model, attempt+1, err))
				continue
			}
			if err := validateGroundedStageOutput(stage, out); err != nil {
				attemptErrors = append(attemptErrors, fmt.Sprintf("%s attempt=%d semantic=%v", model, attempt+1, err))
				continue
			}
			return model, nil
		}
	}

	if len(attemptErrors) > 8 {
		attemptErrors = attemptErrors[len(attemptErrors)-8:]
	}
	details := strings.Join(attemptErrors, " | ")
	if details == "" {
		details = "no usable model response"
	}
	if hadSuccessfulHTTP {
		return "", fmt.Errorf("%w: stage=%s; %s", ErrGroundedAIInvalidOutput, stage, details)
	}
	return "", fmt.Errorf("%w: stage=%s; %s", ErrGroundedAIUnavailable, stage, details)
}
