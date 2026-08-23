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

func groundedModelCandidatesForContext(ctx context.Context) []string {
	strategy := aiModelStrategyFromContext(ctx)
	quality := os.Getenv("AI_MODELS_FIX_QUALITY")
	final := os.Getenv("AI_MODELS_FIX_FINAL")
	generic := os.Getenv("TOGETHER_AI_MODEL")

	ordered := []string{}
	switch strategy {
	case "final_review":
		ordered = append(ordered, final, quality, generic)
	case "quality":
		ordered = append(ordered, quality, final, generic)
	case "economy", "balanced":
		ordered = append(ordered, generic, quality, final)
	default:
		ordered = append(ordered, quality, final, generic)
	}
	ordered = append(ordered, "deepseek-ai/DeepSeek-V4-Pro")

	models := []string{}
	for _, raw := range ordered {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				models = append(models, part)
			}
		}
	}
	return compactStrings(models)
}

func groundedRetryInstruction(stage string) string {
	switch stage {
	case "fact_extractor":
		return "\n\nالمحاولة السابقة كانت ناقصة أو JSON غير صالح. أعد JSON قصيرًا وصحيحًا فقط. لا تستخدم Markdown. حد أقصى 10 حقائق، كل claim مختصر، وsource_notes بحد أقصى عنصرين."
	case "grounded_writer":
		return "\n\nالمحاولة السابقة كانت ناقصة أو JSON غير صالح. أعد JSON صحيحًا ومختصرًا فقط، بدون Markdown خارج content_html وبدون حشو."
	case "claim_validator":
		return "\n\nالمحاولة السابقة كانت ناقصة أو JSON غير صالح. أعد JSON صحيحًا ومختصرًا فقط، واجعل notes مختصرة."
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
