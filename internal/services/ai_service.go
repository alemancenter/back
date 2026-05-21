package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
)

const (
	AIOverallTimeout = 3 * time.Minute
	AIRequestTimeout = 60 * time.Second
	siteBaseURL      = "https://alemancenter.com"

	defaultAIBaseURL = "https://api.together.ai/v1"
	defaultAIModel   = "Qwen/Qwen3.5-9B"

	minSEOArticleWords = 300
	maxSEOArticleWords = 1000
)

var (
	ErrAIGenerationTimeout = errors.New("ai generation timed out")
	ErrAIProviderFailed    = errors.New("ai provider failed")

	defaultAIFallbackModels = []string{}
)

type searchIntent string

const (
	intentInformational searchIntent = "informational"
	intentSchool        searchIntent = "school"
	intentDownload      searchIntent = "download"
	intentGeneral       searchIntent = "general"
)

type SEOArticle struct {
	Title           string         `json:"title"`
	Slug            string         `json:"slug"`
	MetaTitle       string         `json:"meta_title"`
	MetaDescription string         `json:"meta_description"`
	Keywords        []string       `json:"keywords"`
	Content         string         `json:"content"`
	ContentHTML     string         `json:"content_html"`
	FAQ             []FAQItem      `json:"faq"`
	FeaturedSnippet string         `json:"featured_snippet"`
	TitleVariants   []string       `json:"title_variants"`
	InternalLinks   []InternalLink `json:"internal_links"`
	SEOScore        int            `json:"seo_score"`
	SEOIssues       []string       `json:"seo_issues"`
	CoverAltText    string         `json:"cover_alt_text"`
	SchemaType      string         `json:"schema_type"`
	SchemaHTML      string         `json:"schema_html"`
	WordCount       int            `json:"word_count"`
}

type FAQItem struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

type InternalLink struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type AIService interface {
	GenerateSEOArticle(title, contentType string) (*SEOArticle, error)
	GenerateArticleContent(title string) (string, error)
	RunContentIntelligence(ctx context.Context, req ContentIntelligenceRequest) (*ContentIntelligenceResponse, error)
}

type ContentIntelligenceRequest struct {
	Task          string `json:"task"`
	ModelStrategy string `json:"model_strategy,omitempty"`
	ContentType   string `json:"content_type"`
	ContentID     string `json:"content_id,omitempty"`
	CountryCode   string `json:"country_code,omitempty"`
	Title         string `json:"title"`
	Content       string `json:"content"`
	PlainText     string `json:"plain_text"`
	URL           string `json:"url"`
	Language      string `json:"language"`
	JobID         string `json:"job_id,omitempty"`
}

type ContentIntelligenceIssue struct {
	Type     string `json:"type"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Action   string `json:"action"`
	Evidence string `json:"evidence,omitempty"`
}

type ContentIntelligenceSuggestion struct {
	Type     string `json:"type"`
	Priority string `json:"priority"`
	Message  string `json:"message"`
}

type ContentIntelligenceResponse struct {
	Decision         string                          `json:"decision"`
	AdSenseRisk      string                          `json:"adsense_risk"`
	Score            int                             `json:"score"`
	PolicyScore      int                             `json:"policy_score"`
	SEOScore         int                             `json:"seo_score"`
	LanguageScore    int                             `json:"language_score"`
	SafetyLinksScore int                             `json:"safety_links_score"`
	StructureScore   int                             `json:"structure_score"`
	CanAutoFix       bool                            `json:"can_auto_fix"`
	Summary          string                          `json:"summary"`
	Issues           []ContentIntelligenceIssue      `json:"issues"`
	Suggestions      []ContentIntelligenceSuggestion `json:"suggestions"`
	FixedTitle       string                          `json:"fixed_title,omitempty"`
	FixedContent     string                          `json:"fixed_content,omitempty"`
	FixSummary       string                          `json:"fix_summary,omitempty"`
	Provider         string                          `json:"provider"`
	Model            string                          `json:"model"`
	PromptVersion    string                          `json:"prompt_version"`
	Tokens           int                             `json:"tokens"`
	ProcessingTimeMS int64                           `json:"processing_time_ms"`
	ModelStrategy    string                          `json:"model_strategy,omitempty"`
	ModelRole        string                          `json:"model_role,omitempty"`
}

type aiService struct {
	apiKey         string
	baseURL        string
	model          string
	fallbackModels []string
	modelRouter    aiModelRouter
	httpClient     *http.Client
}

func NewAIService(apiKey string) AIService {
	apiKey = firstNonEmpty(
		apiKey,
		os.Getenv("TOGETHER_AI_API_KEY"),
		os.Getenv("TOGETHER_AI_KEY"),
		os.Getenv("TOGETHER_API_KEY"),
	)

	baseURL := strings.TrimRight(firstNonEmpty(os.Getenv("TOGETHER_AI_BASE_URL"), defaultAIBaseURL), "/")
	model := firstNonEmpty(os.Getenv("TOGETHER_AI_MODEL"), defaultAIModel)
	fallbackModels := parseModelList(os.Getenv("TOGETHER_AI_FALLBACK_MODELS"))
	if len(fallbackModels) == 0 {
		fallbackModels = append([]string(nil), defaultAIFallbackModels...)
	}
	fallbackModels = uniqueFallbackModels(model, fallbackModels)
	modelRouter := newAIModelRouter(model, fallbackModels)

	return &aiService{
		apiKey:         strings.TrimSpace(apiKey),
		baseURL:        baseURL,
		model:          model,
		fallbackModels: fallbackModels,
		modelRouter:    modelRouter,
		httpClient: &http.Client{
			Timeout: AIRequestTimeout + 5*time.Second,
		},
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}

	return ""
}

func parseModelList(raw string) []string {
	parts := strings.Split(raw, ",")
	models := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			models = append(models, part)
		}
	}

	return models
}

func uniqueFallbackModels(primary string, fallbackModels []string) []string {
	primary = strings.TrimSpace(primary)
	seen := map[string]bool{primary: true}
	result := make([]string, 0, len(fallbackModels))

	for _, model := range fallbackModels {
		model = strings.TrimSpace(model)
		if model == "" || seen[model] {
			continue
		}

		seen[model] = true
		result = append(result, model)
	}

	return result
}

func (s *aiService) GenerateSEOArticle(title, contentType string) (*SEOArticle, error) {
	title = normalizeInputTitle(title)

	if err := validateInputTitle(title); err != nil {
		return nil, err
	}

	if s.apiKey == "" {
		return nil, errors.New("Together AI API key is missing")
	}

	if contentType != "post" {
		contentType = "article"
	}

	ctx, cancel := context.WithTimeout(context.Background(), AIOverallTimeout)
	defer cancel()

	return s.generateSEOWithFallback(ctx, title, contentType, 0)
}

func (s *aiService) RunContentIntelligence(ctx context.Context, req ContentIntelligenceRequest) (*ContentIntelligenceResponse, error) {
	if s.apiKey == "" {
		return nil, errors.New("Together AI API key is missing")
	}

	req.Task = strings.TrimSpace(req.Task)
	if req.Task == "" {
		req.Task = "audit_content"
	}
	req.ModelStrategy = normalizeAIModelStrategy(req.ModelStrategy)
	if req.ContentType != "post" {
		req.ContentType = "article"
	}

	ctx, cancel := context.WithTimeout(ctx, AIOverallTimeout)
	defer cancel()

	return s.runContentIntelligenceWithFallback(ctx, req, 0)
}

type aiCompletionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func parseAIUsage(bodyBytes []byte) aiCompletionUsage {
	var data struct {
		Usage aiCompletionUsage `json:"usage"`
	}
	if err := json.Unmarshal(bodyBytes, &data); err != nil {
		return aiCompletionUsage{}
	}
	return data.Usage
}

func estimateAITokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	// Conservative cross-language estimate. Arabic text is often tokenized denser
	// than latin text, so keep a minimum of one token per two runes for short text.
	runes := len([]rune(text))
	byChars := (len(text) + 3) / 4
	byRunes := (runes + 1) / 2
	if byRunes > byChars {
		return byRunes
	}
	return byChars
}

func aiModelCostPerMillion(model string) (float64, float64) {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "lfm2-24b"):
		return 0.03, 0.12
	case strings.Contains(m, "gpt-oss-20b"):
		return 0.05, 0.20
	case strings.Contains(m, "gemma-3n"):
		return 0.06, 0.12
	case strings.Contains(m, "qwen3.5-9b") || strings.Contains(m, "qwen3-9b"):
		return 0.10, 0.15
	case strings.Contains(m, "rnj-1"):
		return 0.15, 0.15
	case strings.Contains(m, "gpt-oss-120b"):
		return 0.15, 0.60
	case strings.Contains(m, "gemma-4-31b"):
		return 0.20, 0.50
	case strings.Contains(m, "qwen3-235b"):
		return 0.20, 0.60
	case strings.Contains(m, "minimax"):
		return 0.30, 1.20
	case strings.Contains(m, "kimi-k2.5") || strings.Contains(m, "kimi/kimi-k2.5"):
		return 0.50, 2.80
	case strings.Contains(m, "qwen3.5-397b") || strings.Contains(m, "397b"):
		return 0.60, 3.60
	case strings.Contains(m, "llama-3.3-70b"):
		return 0.88, 0.88
	case strings.Contains(m, "glm-5.1"):
		return 1.40, 4.40
	case strings.Contains(m, "glm-5"):
		return 1.00, 3.20
	case strings.Contains(m, "deepseek"):
		return 2.10, 4.40
	default:
		return 0, 0
	}
}

func estimateAIModelCostUSD(model string, inputTokens, outputTokens int) float64 {
	inputPerMillion, outputPerMillion := aiModelCostPerMillion(model)
	if inputPerMillion == 0 && outputPerMillion == 0 {
		return 0
	}
	return (float64(inputTokens)/1000000.0)*inputPerMillion + (float64(outputTokens)/1000000.0)*outputPerMillion
}

func recordContentAIModelRun(req ContentIntelligenceRequest, modelName, role, status, errMessage string, inputTokens, outputTokens int, durationMS int64) {
	if database.DB() == nil {
		return
	}
	if inputTokens <= 0 {
		inputTokens = estimateAITokens(req.Title + "\n" + req.PlainText + "\n" + req.Content)
	}
	cost := estimateAIModelCostUSD(modelName, inputTokens, outputTokens)
	run := models.ContentAIModelRun{
		JobID:            strings.TrimSpace(req.JobID),
		ContentType:      strings.TrimSpace(req.ContentType),
		ContentID:        strings.TrimSpace(req.ContentID),
		CountryCode:      strings.TrimSpace(req.CountryCode),
		TaskType:         strings.TrimSpace(req.Task),
		ModelStrategy:    strings.TrimSpace(req.ModelStrategy),
		Provider:         "together_ai",
		Model:            modelName,
		Role:             role,
		InputTokens:      inputTokens,
		OutputTokens:     outputTokens,
		EstimatedCostUSD: cost,
		DurationMS:       durationMS,
		Status:           status,
		Error:            truncate(errMessage, 1000),
	}
	if run.TaskType == "" {
		run.TaskType = "audit_content"
	}
	if run.ModelStrategy == "" {
		run.ModelStrategy = "balanced"
	}
	if err := database.DB().Create(&run).Error; err != nil {
		log.Printf("Content AI model run logging failed | model=%s | err=%v", modelName, err)
	}
}

func (s *aiService) runContentIntelligenceWithFallback(ctx context.Context, req ContentIntelligenceRequest, attempt int) (*ContentIntelligenceResponse, error) {
	started := time.Now()
	currentModel, modelRole, err := s.resolveContentIntelligenceModel(req, attempt)
	if err != nil {
		return nil, err
	}
	log.Printf("Content AI | strategy=%s | role=%s | model=%s | attempt=%d | task=%s | title=%q", req.ModelStrategy, modelRole, currentModel, attempt, req.Task, truncate(req.Title, 70))
	systemPrompt, userPrompt := buildContentIntelligencePrompts(req)
	estimatedInputTokens := estimateAITokens(systemPrompt + "\n" + userPrompt)
	payload := map[string]interface{}{
		"model": currentModel,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"max_tokens":  2000,
		"temperature": 0.25,
		"top_p":       0.9,
		"stop":        []string{"<|eot_id|>", "<|im_end|>"},
		"reasoning":   map[string]interface{}{"enabled": false},
		"response_format": map[string]interface{}{
			"type": "json_schema",
			"json_schema": map[string]interface{}{
				"name":   "content_intelligence",
				"strict": false,
				"schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"decision":           map[string]interface{}{"type": "string"},
						"adsense_risk":       map[string]interface{}{"type": "string"},
						"score":              map[string]interface{}{"type": "integer"},
						"policy_score":       map[string]interface{}{"type": "integer"},
						"seo_score":          map[string]interface{}{"type": "integer"},
						"language_score":     map[string]interface{}{"type": "integer"},
						"safety_links_score": map[string]interface{}{"type": "integer"},
						"structure_score":    map[string]interface{}{"type": "integer"},
						"can_auto_fix":       map[string]interface{}{"type": "boolean"},
						"summary":            map[string]interface{}{"type": "string"},
						"issues":             map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object"}},
						"suggestions":        map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object"}},
						"fixed_title":        map[string]interface{}{"type": "string"},
						"fixed_content":      map[string]interface{}{"type": "string"},
						"fix_summary":        map[string]interface{}{"type": "string"},
					},
				},
			},
		},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, MapError(err)
	}
	requestCtx, cancel := context.WithTimeout(ctx, AIRequestTimeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(requestCtx, http.MethodPost, s.baseURL+"/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, MapError(err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		durationMS := time.Since(started).Milliseconds()
		recordContentAIModelRun(req, currentModel, modelRole, "failed", MapError(err).Error(), estimatedInputTokens, 0, durationMS)
		if s.hasContentIntelligenceFallback(req, attempt) {
			return s.runContentIntelligenceWithFallback(ctx, req, attempt+1)
		}
		return nil, fmt.Errorf("%w: %v", ErrAIProviderFailed, MapError(err))
	}
	defer resp.Body.Close()
	responseBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		durationMS := time.Since(started).Milliseconds()
		recordContentAIModelRun(req, currentModel, modelRole, "failed", MapError(err).Error(), estimatedInputTokens, 0, durationMS)
		return nil, MapError(err)
	}
	usage := parseAIUsage(responseBytes)
	inputTokens := usage.PromptTokens
	if inputTokens <= 0 {
		inputTokens = estimatedInputTokens
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := extractAPIError(responseBytes)
		if apiErr == "" {
			apiErr = string(responseBytes)
		}
		durationMS := time.Since(started).Milliseconds()
		recordContentAIModelRun(req, currentModel, modelRole, "failed", fmt.Sprintf("status %d — %s", resp.StatusCode, apiErr), inputTokens, usage.CompletionTokens, durationMS)
		if s.hasContentIntelligenceFallback(req, attempt) {
			return s.runContentIntelligenceWithFallback(ctx, req, attempt+1)
		}
		return nil, fmt.Errorf("%w: status %d — %s", ErrAIProviderFailed, resp.StatusCode, apiErr)
	}
	rawContent, err := parseAIRawContent(responseBytes)
	if err != nil {
		durationMS := time.Since(started).Milliseconds()
		recordContentAIModelRun(req, currentModel, modelRole, "failed", err.Error(), inputTokens, usage.CompletionTokens, durationMS)
		if s.hasContentIntelligenceFallback(req, attempt) {
			return s.runContentIntelligenceWithFallback(ctx, req, attempt+1)
		}
		return nil, err
	}
	var out ContentIntelligenceResponse
	if err := json.Unmarshal([]byte(cleanJSONPayload(rawContent)), &out); err != nil {
		outputTokens := usage.CompletionTokens
		if outputTokens <= 0 {
			outputTokens = estimateAITokens(rawContent)
		}
		durationMS := time.Since(started).Milliseconds()
		recordContentAIModelRun(req, currentModel, modelRole, "failed", err.Error(), inputTokens, outputTokens, durationMS)
		if s.hasContentIntelligenceFallback(req, attempt) {
			return s.runContentIntelligenceWithFallback(ctx, req, attempt+1)
		}
		return nil, err
	}
	out.Provider = "together_ai"
	out.Model = currentModel
	out.ModelStrategy = req.ModelStrategy
	out.ModelRole = modelRole
	out.PromptVersion = "content-intelligence-v1:" + req.ModelStrategy + ":" + modelRole
	out.ProcessingTimeMS = time.Since(started).Milliseconds()
	outputTokens := usage.CompletionTokens
	if outputTokens <= 0 {
		outputTokens = estimateAITokens(rawContent)
	}
	out.Tokens = inputTokens + outputTokens
	recordContentAIModelRun(req, currentModel, modelRole, "success", "", inputTokens, outputTokens, out.ProcessingTimeMS)
	if out.Decision == "" {
		out.Decision = "needs_fix"
	}
	if out.AdSenseRisk == "" {
		out.AdSenseRisk = "medium"
	}
	return &out, nil
}

func buildContentIntelligencePrompts(req ContentIntelligenceRequest) (string, string) {
	kind := "مقال طويل SEO"
	minWords := 300
	if req.ContentType == "post" {
		kind = "بوست تعليمي"
		minWords = 300
	}
	system := "أنت محرر محتوى عربي محترف وخبير SEO وسياسات Google AdSense. استخدم نفس أسلوب منصة الأيمان التعليمية: لغة عربية سليمة، محتوى تعليمي آمن، بنية واضحة، قيمة تعليمية حقيقية، بدون حشو أو مبالغة. أعد Strict JSON فقط ولا تكتب أي نص خارج JSON."
	mode := "حلل المحتوى واتخذ قرار نشر/تصحيح"
	extra := ""
	if req.Task == "fix_content" {
		mode = "أنشئ نسخة مصححة وموسّعة آمنة للمراجعة البشرية دون تغيير الفكرة الأساسية"
		extra = fmt.Sprintf(`
تعليمات صارمة لمهمة fix_content:
- ممنوع إرجاع نفس HTML الأصلي أو نسخة مطابقة منه.
- إذا كان المحتوى قصيراً أو Thin Content، وسّعه إلى %d كلمة على الأقل.
- اكتب fixed_content بصيغة HTML نظيفة تحتوي على فقرات <p> وعناوين فرعية <h2> مناسبة.
- أضف قيمة تعليمية حقيقية: شرح الفكرة، نقاط عملية، أمثلة أو إرشادات، وأسئلة شائعة مختصرة عند الحاجة.
- لا تضف روابط خارجية، ولا أكواد، ولا عبارات تسويقية مبالغ فيها.
- حافظ على أسلوب عربي تعليمي مناسب للطلاب وأولياء الأمور والمعلمين.
- يجب أن يكون fixed_content أطول وأغنى من النص الأصلي بوضوح.`, minWords)
	}
	user := fmt.Sprintf(`المهمة: %s
نوع المحتوى: %s
العنوان: %s
الرابط: %s

النص الصافي:
%s

HTML الأصلي:
%s
%s

أرجع JSON بهذه المفاتيح فقط: decision, adsense_risk, score, policy_score, seo_score, language_score, safety_links_score, structure_score, can_auto_fix, summary, issues, suggestions, fixed_title, fixed_content, fix_summary.
قواعد القرار: أي Thin Content أو مخالفة سياسة مهمة يجب أن تكون needs_fix أو restricted_ads أو rejected وليس approved. إذا كانت المهمة fix_content فأعد fixed_content بصيغة HTML نظيفة مناسبة لـ %s.`, mode, kind, req.Title, req.URL, truncate(req.PlainText, 6000), truncate(req.Content, 6000), extra, kind)
	return system, user
}

func cleanJSONPayload(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	return strings.TrimSpace(raw)
}

// aiModelRouter selects model sequences by task and strategy so large content jobs
// do not depend on a single expensive or overloaded model. Every route falls back
// to the configured default model to keep deployments safe when provider model IDs
// differ between accounts.
type aiModelRouter struct {
	defaultModel string
	fallbacks    []string
	routes       map[string][]string
}

func newAIModelRouter(defaultModel string, fallbackModels []string) aiModelRouter {
	defaultModel = strings.TrimSpace(defaultModel)
	fallbackModels = uniqueFallbackModels(defaultModel, fallbackModels)
	base := append([]string{defaultModel}, fallbackModels...)
	return aiModelRouter{
		defaultModel: defaultModel,
		fallbacks:    fallbackModels,
		routes: map[string][]string{
			"audit_content:economy":      modelRouteFromEnv("AI_MODELS_AUDIT_ECONOMY", append(parseModelList("togethercomputer/LFM2-24B-A2B,openai/gpt-oss-20b"), base...)),
			"audit_content:balanced":     modelRouteFromEnv("AI_MODELS_AUDIT_BALANCED", append(parseModelList("openai/gpt-oss-20b,Qwen/Qwen3.5-9B,google/gemma-3n-E4B-it"), base...)),
			"audit_content:quality":      modelRouteFromEnv("AI_MODELS_AUDIT_QUALITY", append(parseModelList("Qwen/Qwen3-235B-A22B-Instruct-2507-FP8-Throughput,openai/gpt-oss-120b,Qwen/Qwen3.5-9B"), base...)),
			"audit_content:final_review": modelRouteFromEnv("AI_MODELS_AUDIT_FINAL", append(parseModelList("openai/gpt-oss-120b,Kimi/Kimi-K2.5,Qwen/Qwen3-235B-A22B-Instruct-2507-FP8-Throughput,Qwen/Qwen3.5-9B"), base...)),
			"fix_content:economy":        modelRouteFromEnv("AI_MODELS_FIX_ECONOMY", append(parseModelList("Qwen/Qwen3.5-9B,openai/gpt-oss-20b"), base...)),
			"fix_content:balanced":       modelRouteFromEnv("AI_MODELS_FIX_BALANCED", append(parseModelList("Qwen/Qwen3.5-9B,google/gemma-4-31b-it-fp8,openai/gpt-oss-120b"), base...)),
			"fix_content:quality":        modelRouteFromEnv("AI_MODELS_FIX_QUALITY", append(parseModelList("Qwen/Qwen3-235B-A22B-Instruct-2507-FP8-Throughput,openai/gpt-oss-120b,Qwen/Qwen3.5-9B"), base...)),
			"fix_content:final_review":   modelRouteFromEnv("AI_MODELS_FIX_FINAL", append(parseModelList("openai/gpt-oss-120b,Kimi/Kimi-K2.5,Qwen/Qwen3-235B-A22B-Instruct-2507-FP8-Throughput,Qwen/Qwen3.5-9B"), base...)),
		},
	}
}

func modelRouteFromEnv(envKey string, defaults []string) []string {
	if configured := parseModelList(os.Getenv(envKey)); len(configured) > 0 {
		return uniqueModelSequence(configured)
	}
	return uniqueModelSequence(defaults)
}

func uniqueModelSequence(models []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		out = append(out, model)
	}
	return out
}

func normalizeAIModelStrategy(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "economy", "balanced", "quality", "final_review":
		return value
	case "multi_stage_quality":
		return "balanced"
	case "premium", "high_quality":
		return "quality"
	case "critical", "final":
		return "final_review"
	default:
		return "balanced"
	}
}

func (s *aiService) contentIntelligenceModels(req ContentIntelligenceRequest) []string {
	strategy := normalizeAIModelStrategy(req.ModelStrategy)
	task := strings.TrimSpace(req.Task)
	if task == "" {
		task = "audit_content"
	}
	key := task + ":" + strategy
	if models := s.modelRouter.routes[key]; len(models) > 0 {
		return models
	}
	fallback := append([]string{s.modelRouter.defaultModel}, s.modelRouter.fallbacks...)
	return uniqueModelSequence(fallback)
}

func (s *aiService) resolveContentIntelligenceModel(req ContentIntelligenceRequest, attempt int) (string, string, error) {
	models := s.contentIntelligenceModels(req)
	if attempt >= 0 && attempt < len(models) {
		role := "primary"
		if attempt > 0 {
			role = fmt.Sprintf("fallback_%d", attempt)
		}
		return models[attempt], role, nil
	}
	return "", "", errors.New("all AI models unavailable for content intelligence route")
}

func (s *aiService) hasContentIntelligenceFallback(req ContentIntelligenceRequest, attempt int) bool {
	return attempt+1 < len(s.contentIntelligenceModels(req))
}

func (s *aiService) GenerateArticleContent(title string) (string, error) {
	article, err := s.GenerateSEOArticle(title, "article")
	if err != nil {
		return "", err
	}

	if article.ContentHTML != "" {
		return article.ContentHTML, nil
	}

	return article.Content, nil
}

func (s *aiService) generateSEOWithFallback(ctx context.Context, title, contentType string, attempt int) (*SEOArticle, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("%w: %v", ErrAIGenerationTimeout, ctx.Err())
	}

	currentModel, err := s.resolveModel(attempt)
	if err != nil {
		return nil, err
	}

	intent := detectIntent(title)
	isArabic := containsArabic(title)

	log.Printf(
		"AI generation | model=%s | attempt=%d | intent=%s | title=%q",
		currentModel,
		attempt,
		intent,
		truncate(title, 70),
	)

	systemPrompt, userPrompt := buildSEOPrompts(title, isArabic, intent)

	payload := map[string]interface{}{
		"model": currentModel,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"max_tokens":  2200,
		"temperature": 0.46,
		"top_p":       0.9,
		"stop":        []string{"<|eot_id|>", "<|im_end|>"},
		"reasoning":   map[string]interface{}{"enabled": false},
		"response_format": map[string]interface{}{
			"type": "json_object",
		},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, MapError(err)
	}

	requestCtx, cancel := context.WithTimeout(ctx, AIRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(
		requestCtx,
		http.MethodPost,
		s.baseURL+"/chat/completions",
		bytes.NewReader(bodyBytes),
	)
	if err != nil {
		return nil, MapError(err)
	}

	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		if errors.Is(requestCtx.Err(), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return s.trySEOFallbackOrError(
				ctx,
				title,
				contentType,
				attempt,
				fmt.Errorf("%w: model %s exceeded %s", ErrAIGenerationTimeout, currentModel, AIRequestTimeout),
			)
		}

		return s.trySEOFallbackOrError(
			ctx,
			title,
			contentType,
			attempt,
			fmt.Errorf("%w: %v", ErrAIProviderFailed, MapError(err)),
		)
	}
	defer resp.Body.Close()

	responseBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, MapError(err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := extractAPIError(responseBytes)
		if apiErr == "" {
			apiErr = string(responseBytes)
		}

		log.Printf(
			"Together AI error | model=%s | status=%d | error=%s",
			currentModel,
			resp.StatusCode,
			truncate(apiErr, 220),
		)

		return s.trySEOFallbackOrError(
			ctx,
			title,
			contentType,
			attempt,
			fmt.Errorf("%w: status %d — %s", ErrAIProviderFailed, resp.StatusCode, apiErr),
		)
	}

	rawContent, err := parseAIRawContent(responseBytes)
	if err != nil {
		return s.trySEOFallbackOrError(ctx, title, contentType, attempt, err)
	}

	article, err := parseSEOArticle(rawContent)
	if err != nil {
		log.Printf("JSON parse failed | model=%s | err=%v | raw=%s", currentModel, err, truncate(rawContent, 350))
		return s.trySEOFallbackOrError(ctx, title, contentType, attempt, err)
	}

	article = cleanSEOArticle(article, title, isArabic, contentType)

	if err := validateSEOArticle(article); err != nil {
		log.Printf(
			"Validation failed | model=%s | err=%v | words=%d | preview=%s",
			currentModel,
			err,
			article.WordCount,
			truncate(article.Content, 220),
		)

		return s.trySEOFallbackOrError(ctx, title, contentType, attempt, err)
	}

	log.Printf("AI generation OK | model=%s | words=%d | intent=%s", currentModel, article.WordCount, intent)

	return article, nil
}

func (s *aiService) resolveModel(attempt int) (string, error) {
	if attempt == 0 {
		return s.model, nil
	}

	idx := attempt - 1
	if idx >= 0 && idx < len(s.fallbackModels) {
		return s.fallbackModels[idx], nil
	}

	return "", errors.New("all AI models unavailable")
}

func (s *aiService) trySEOFallbackOrError(ctx context.Context, title, contentType string, attempt int, err error) (*SEOArticle, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("%w: %v", ErrAIGenerationTimeout, ctx.Err())
	}

	if attempt < len(s.fallbackModels) {
		log.Printf("Trying fallback model: %s", s.fallbackModels[attempt])
		return s.generateSEOWithFallback(ctx, title, contentType, attempt+1)
	}

	return nil, err
}

func detectIntent(title string) searchIntent {
	l := strings.ToLower(title)

	switch {
	case strings.Contains(l, "شروط") ||
		strings.Contains(l, "كيفية") ||
		strings.Contains(l, "كيف") ||
		strings.Contains(l, "طريقة") ||
		strings.Contains(l, "خطوات") ||
		strings.Contains(l, "أسباب") ||
		strings.Contains(l, "تقييم") ||
		strings.Contains(l, "مزايا") ||
		strings.Contains(l, "how to") ||
		strings.Contains(l, "steps") ||
		strings.Contains(l, "conditions") ||
		strings.Contains(l, "benefits"):
		return intentInformational

	case strings.Contains(l, "تحميل") ||
		strings.Contains(l, "pdf") ||
		strings.Contains(l, "ملف") ||
		strings.Contains(l, "نموذج") ||
		strings.Contains(l, "download") ||
		strings.Contains(l, "template") ||
		strings.Contains(l, "file"):
		return intentDownload

	case strings.Contains(l, "موضوع تعبير") ||
		strings.Contains(l, "تعبير عن") ||
		strings.Contains(l, "مقال عن") ||
		strings.Contains(l, "درس") ||
		strings.Contains(l, "شرح") ||
		strings.Contains(l, "اختبار") ||
		strings.Contains(l, "lesson") ||
		strings.Contains(l, "exam") ||
		strings.Contains(l, "worksheet"):
		return intentSchool

	default:
		return intentGeneral
	}
}

func normalizeInputTitle(title string) string {
	title = strings.TrimSpace(title)
	title = strings.ReplaceAll(title, "，", "،")
	title = strings.ReplaceAll(title, "。", ".")
	title = strings.ReplaceAll(title, "###", "")
	title = strings.ReplaceAll(title, "```", "")
	title = strings.ReplaceAll(title, "<", "")
	title = strings.ReplaceAll(title, ">", "")
	title = regexp.MustCompile(`[ \t]+`).ReplaceAllString(title, " ")
	title = regexp.MustCompile(`\n+`).ReplaceAllString(title, " ")

	return strings.TrimSpace(title)
}

func validateInputTitle(title string) error {
	if title == "" {
		return errors.New("title is required")
	}

	n := len([]rune(title))

	if n < 5 {
		return errors.New("title is too short")
	}

	if n > 200 {
		return errors.New("title is too long")
	}

	if containsSuspiciousPrompt(title) {
		return errors.New("title contains unsafe prompt instructions")
	}

	return nil
}

func containsSuspiciousPrompt(s string) bool {
	lower := strings.ToLower(s)

	blocked := []string{
		"ignore previous",
		"ignore all",
		"system prompt",
		"developer message",
		"act as",
		"اكتب كود",
		"تجاهل التعليمات",
		"انس التعليمات",
		"نفذ الأمر",
		"<script",
		"</script",
		"drop table",
		"delete from",
	}

	for _, b := range blocked {
		if strings.Contains(lower, strings.ToLower(b)) {
			return true
		}
	}

	return false
}

func intentHintAr(i searchIntent) string {
	switch i {
	case intentInformational:
		return "- ركّز على الإجابة العملية عن سؤال القارئ، ووضّح الشروط والخطوات والفوائد بشكل مترابط."
	case intentDownload:
		return "- وضّح فائدة الملف أو النموذج، وكيف يمكن استخدامه تعليمياً، دون وضع روابط تحميل."
	case intentSchool:
		return "- استخدم أسلوباً تعليمياً واضحاً مناسباً للطلاب وأولياء الأمور والمعلمين."
	default:
		return "- اجعل المقال مفيداً وعملياً للقارئ العادي، مع أمثلة أو توضيحات عند الحاجة."
	}
}

func intentHintEn(i searchIntent) string {
	switch i {
	case intentInformational:
		return "- Answer the reader's practical question with clear conditions, steps, and benefits."
	case intentDownload:
		return "- Explain the file or template benefit and how it can be used educationally, without links."
	case intentSchool:
		return "- Use a clear educational style suitable for students, parents, and teachers."
	default:
		return "- Make the article useful and practical for the average reader."
	}
}

func buildSEOPrompts(title string, isArabic bool, intent searchIntent) (string, string) {
	if isArabic {
		system := `أنت محرر محتوى عربي محترف متخصص في كتابة مقالات SEO تعليمية عالية الجودة.

مهمتك:
- كتابة مقال عربي احترافي جاهز للنشر في موقع تعليمي.
- إخراج JSON صحيح فقط مطابق للهيكل المطلوب.
- عدم كتابة أي نص خارج JSON إطلاقاً.

قواعد صارمة يجب الالتزام بها:

1. الإخراج:
- يجب أن يكون الرد JSON فقط.
- لا تكتب أي شرح أو مقدمة أو تعليق خارج JSON.
- يجب أن يطابق الرد هذا الشكل فقط:
{"meta_description":"وصف بين 100 و180 حرفاً","keywords":["كلمة 1","كلمة 2","كلمة 3","كلمة 4","كلمة 5"],"faq":[{"question":"سؤال واضح","answer":"إجابة عملية مختصرة"}],"content":"نص المقال الكامل"}

2. اللغة:
- استخدم اللغة العربية فقط داخل content.
- ممنوع استخدام أي لغة أخرى أو كلمات أجنبية.

3. جودة المحتوى:
- اكتب محتوى حقيقي مفيد وليس حشو أو جمل عامة.
- اكتب content بطول 350 إلى 450 كلمة تقريباً.
- لا تكرر نفس الفكرة بصياغات مختلفة.
- اجعل الأسلوب عملياً وموجهاً للقارئ.

4. البنية:
- لا تكتب النص كفقرة واحدة.
- استخدم فقرات متعددة واضحة مفصولة بسطر فارغ.
- اجعل كل فقرة تشرح فكرة مختلفة.
- أضف 2 إلى 3 عناوين فرعية قصيرة على سطر مستقل.

5. ممنوعات:
- لا تستخدم Markdown (#, ##, *, -).
- لا تستخدم HTML داخل content.
- لا تكتب روابط.
- لا تكتب عبارات مثل: "في هذا المقال" أو "إليك النص".
- لا تبدأ بجمل ضعيفة مثل: "تُعد" أو "يُعد" أو "تعتبر".

6. الهدف:
- المقال يجب أن يكون مناسباً لمحركات البحث (SEO).
- المقال يجب أن يكون مفيداً للطالب أو ولي الأمر أو المعلم.
- يجب أن يجيب على نية البحث بشكل مباشر.

الالتزام بهذه القواعد بدقة.`

		user := fmt.Sprintf(`اكتب مقالاً SEO تعليمياً احترافياً عن: "%s"
%s

الشروط الخاصة بهذا الطلب:
- اجعل content بين 350 و450 كلمة تقريباً.
- اكتب 5 إلى 8 كلمات مفتاحية دقيقة.
- اكتب 2 إلى 4 أسئلة FAQ مفيدة مرتبطة مباشرة بالموضوع.`, title, intentHintAr(intent))

		return strings.TrimSpace(system), strings.TrimSpace(user)
	}

	system := `You are a professional SEO educational content editor.

Rules:
- Output JSON only. No text outside JSON ever.
- Write real, useful content — no filler or repetition.
- No Markdown, no HTML, no links.
- Multiple clear paragraphs separated by blank lines.
- Add 2-3 short subheadings on their own lines.
- Make content roughly 350-450 words.
- Include 5-8 precise keywords and 2-4 useful FAQ items.
- Do not start with "This article" or "In this article".
- Answer the search intent directly and practically.
- Output exactly this JSON shape: {"meta_description":"...","keywords":["..."],"faq":[{"question":"...","answer":"..."}],"content":"..."}.`

	user := fmt.Sprintf(`Write a professional SEO educational article about: "%s"
%s

Request-specific requirements:
- Keep content roughly 350-450 words.
- Include 5-8 precise keywords.
- Include 2-4 FAQ items that directly answer likely reader questions.`, title, intentHintEn(intent))

	return strings.TrimSpace(system), strings.TrimSpace(user)
}

func parseAIRawContent(bodyBytes []byte) (string, error) {
	var data struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(bodyBytes, &data); err != nil {
		return "", MapError(err)
	}

	if len(data.Choices) == 0 {
		return "", errors.New("no content generated")
	}

	content := strings.TrimSpace(data.Choices[0].Message.Content)
	if content == "" {
		return "", errors.New("empty content generated")
	}

	return content, nil
}

func parseSEOArticle(rawContent string) (*SEOArticle, error) {
	rawContent = strings.TrimSpace(rawContent)
	rawContent = regexp.MustCompile(`(?s)<think>.*?</think>`).ReplaceAllString(rawContent, "")
	rawContent = strings.TrimSpace(rawContent)

	for _, fence := range []string{"```json", "```JSON", "```"} {
		rawContent = strings.TrimPrefix(rawContent, fence)
	}
	rawContent = strings.TrimSuffix(rawContent, "```")
	rawContent = strings.TrimSpace(rawContent)

	start := strings.Index(rawContent, "{")
	end := strings.LastIndex(rawContent, "}")

	if start < 0 || end < 0 || end <= start {
		return nil, errors.New("no JSON object found in AI response")
	}

	rawContent = rawContent[start : end+1]
	// Small models sometimes emit an extra " before string values: ""word" → "word"
	rawContent = regexp.MustCompile(`""\s*(\p{L})`).ReplaceAllString(rawContent, `"$1`)
	repaired := repairJSONStrings(rawContent)

	var article SEOArticle
	if err := json.Unmarshal([]byte(repaired), &article); err != nil {
		repaired2 := regexp.MustCompile(`,\s*([}\]])`).ReplaceAllString(repaired, "$1")
		if err2 := json.Unmarshal([]byte(repaired2), &article); err2 != nil {
			return nil, fmt.Errorf("failed to parse SEO JSON: %w", err)
		}
	}

	return &article, nil
}

func repairJSONStrings(s string) string {
	var buf strings.Builder
	buf.Grow(len(s))

	inString := false
	prevBackslash := false

	for _, r := range s {
		if prevBackslash {
			buf.WriteRune(r)
			prevBackslash = false
			continue
		}

		if r == '\\' && inString {
			prevBackslash = true
			buf.WriteRune(r)
			continue
		}

		if r == '"' {
			inString = !inString
			buf.WriteRune(r)
			continue
		}

		if inString {
			switch r {
			case '\n':
				buf.WriteString(`\n`)
			case '\r':
			case '\t':
				buf.WriteString(`\t`)
			default:
				buf.WriteRune(r)
			}
			continue
		}

		buf.WriteRune(r)
	}

	return buf.String()
}

func cleanSEOArticle(article *SEOArticle, originalTitle string, isArabic bool, contentType string) *SEOArticle {
	article.MetaDescription = cleanPlainText(article.MetaDescription, isArabic)
	article.Content = cleanContent(article.Content, isArabic)
	if isArabic {
		article.Content = splitLongArabicContent(article.Content)
	}
	article.Content = improveHeadings(article.Content)

	article.Title = originalTitle
	article.MetaTitle = truncateRunes(cleanPlainText(originalTitle, isArabic), 60)
	article.CoverAltText = cleanPlainText(originalTitle, isArabic)
	article.SchemaType = "Article"
	article.Slug = makeSlug(originalTitle)

	if len([]rune(article.MetaDescription)) < 80 && article.Content != "" {
		plain := strings.TrimSpace(stripHTML(article.Content))
		words := strings.Fields(plain)

		take := 28
		if len(words) < take {
			take = len(words)
		}

		if take > 0 {
			article.MetaDescription = truncateRunes(strings.Join(words[:take], " "), 160)
		}
	}

	if len([]rune(article.MetaDescription)) > 180 {
		article.MetaDescription = truncateRunes(article.MetaDescription, 160)
	}

	seen := make(map[string]bool)
	cleanedKeywords := make([]string, 0, len(article.Keywords))

	for _, kw := range article.Keywords {
		kw = cleanPlainText(kw, isArabic)
		if kw == "" {
			continue
		}

		key := strings.ToLower(kw)
		if !seen[key] {
			seen[key] = true
			cleanedKeywords = append(cleanedKeywords, kw)
		}
	}

	article.Keywords = cleanedKeywords

	if len(article.Keywords) < 3 {
		for _, w := range strings.Fields(originalTitle) {
			w = strings.Trim(w, ".,،:؛!؟()[]{}\"'")
			if len([]rune(w)) >= 3 {
				key := strings.ToLower(w)
				if !seen[key] {
					seen[key] = true
					article.Keywords = append(article.Keywords, w)
				}
			}

			if len(article.Keywords) >= 5 {
				break
			}
		}
	}

	cleanFAQ := make([]FAQItem, 0, len(article.FAQ))
	for _, item := range article.FAQ {
		q := cleanPlainText(item.Question, isArabic)
		a := cleanPlainText(item.Answer, isArabic)

		if q != "" && a != "" {
			cleanFAQ = append(cleanFAQ, FAQItem{
				Question: q,
				Answer:   a,
			})
		}
	}
	article.FAQ = cleanFAQ

	if len(article.FAQ) == 0 && isArabic && article.Content != "" {
		article.FAQ = autoGenerateFAQ(originalTitle, article.Content)
	}

	article.WordCount = countWords(stripHTML(article.Content))
	article.ContentHTML = formatContentToHTML(article.Content)
	article.ContentHTML = enhanceKeywords(article.ContentHTML, article.Keywords)
	article.FeaturedSnippet = generateFeaturedSnippet(article)
	article.TitleVariants = generateTitleVariants(article.Title)
	article.InternalLinks = generateInternalLinks(article.Title, article.Keywords, contentType)
	article.SEOScore, article.SEOIssues = calculateSEOScore(article)
	article.ContentHTML = injectInternalLinks(article.ContentHTML, article.InternalLinks)

	articleSchema := generateArticleSchema(article)
	faqSchema := generateFAQSchema(article.FAQ)

	article.SchemaHTML = articleSchema
	if faqSchema != "" {
		article.SchemaHTML += "\n" + faqSchema
	}

	return article
}

func validateSEOArticle(article *SEOArticle) error {
	if article == nil {
		return errors.New("article is nil")
	}

	plain := strings.TrimSpace(stripHTML(article.Content))
	if plain == "" {
		return errors.New("content is empty")
	}

	wordCount := countWords(plain)
	if wordCount < minSEOArticleWords {
		return fmt.Errorf("content is too short: %d words", wordCount)
	}

	if wordCount > maxSEOArticleWords {
		return fmt.Errorf("content is too long: %d words", wordCount)
	}

	if containsArabic(article.Title) {
		if !containsArabic(plain) {
			return errors.New("content is not Arabic")
		}

		if arabicRatio(plain) < 0.45 {
			return errors.New("content has low Arabic ratio")
		}
	}

	if hasBadAIIntro(plain) {
		return errors.New("content contains unwanted AI intro")
	}

	if hasExcessiveRepetition(plain) {
		return errors.New("content contains excessive repetition")
	}

	if looksIncomplete(plain) {
		return errors.New("content appears incomplete")
	}

	if len(article.Keywords) < 3 {
		return errors.New("not enough keywords")
	}

	if len([]rune(strings.TrimSpace(article.MetaDescription))) < 60 {
		return errors.New("meta description is too short")
	}

	score, issues := calculateSEOScore(article)
	article.SEOScore = score
	article.SEOIssues = issues

	if score < 50 {
		return fmt.Errorf("SEO score too low: %d issues: %v", score, issues)
	}

	return nil
}

func cleanPlainText(text string, isArabic bool) string {
	text = strings.TrimSpace(text)

	for _, fence := range []string{"```json", "```html", "```text", "```"} {
		text = strings.ReplaceAll(text, fence, "")
	}

	text = strings.ReplaceAll(text, "，", "،")
	text = strings.ReplaceAll(text, "。", ".")
	text = strings.ReplaceAll(text, "؛؛", "؛")
	text = strings.ReplaceAll(text, "،،", "،")

	text = removeMarkdown(text)

	if isArabic {
		text = removeForeignNoise(text)
	}

	text = regexp.MustCompile(`[ \t]+`).ReplaceAllString(text, " ")
	text = regexp.MustCompile(`\n+`).ReplaceAllString(text, " ")

	return strings.TrimSpace(text)
}

func cleanContent(content string, isArabic bool) string {
	content = strings.TrimSpace(content)
	content = regexp.MustCompile(`(?s)<think>.*?</think>`).ReplaceAllString(content, "")

	for _, fence := range []string{"```html", "```text", "```markdown", "```json", "```"} {
		content = strings.ReplaceAll(content, fence, "")
	}

	content = strings.ReplaceAll(content, "，", "،")
	content = strings.ReplaceAll(content, "。", ".")
	content = strings.ReplaceAll(content, "؛؛", "؛")
	content = strings.ReplaceAll(content, "،،", "،")

	content = stripHTML(content)
	content = removeMarkdown(content)

	if isArabic {
		content = strings.ReplaceAll(content, "interaction humanي", "التفاعل الإنساني")
		content = removeForeignNoise(content)
		content = applyArabicLinguisticFilter(content)

		if m := regexp.MustCompile(`[\x{0600}-\x{06FF}]`).FindStringIndex(content); m != nil && m[0] > 0 && m[0] < 80 {
			content = content[m[0]:]
		}
	}

	aiPrefixes := []string{
		"إليك مقال",
		"إليك النص",
		"إليك المقال",
		"هذا المقال عن",
		"هذا نص",
		"في هذا المقال",
		"سأكتب",
		"بالطبع،",
		"بالطبع ،",
		"بالطبع",
		"المقال:",
		"النص:",
		"المحتوى:",
		"محتوى المقال:",
		"Here is the article",
		"Here is the content",
		"Here's the article",
		"Of course,",
		"Of course:",
		"Certainly,",
		"Sure,",
	}

	for pass := 0; pass < 3; pass++ {
		content = strings.TrimSpace(content)
		stripped := false

		for _, prefix := range aiPrefixes {
			if strings.HasPrefix(content, prefix) {
				content = strings.TrimSpace(content[len(prefix):])
				content = strings.TrimLeft(content, ":،, \t\n")
				stripped = true
				break
			}
		}

		if !stripped {
			break
		}
	}

	content = regexp.MustCompile(`[ \t]+`).ReplaceAllString(content, " ")
	content = regexp.MustCompile(`\n{3,}`).ReplaceAllString(content, "\n\n")

	return strings.TrimSpace(content)
}

func splitLongArabicContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}

	if strings.Count(content, "\n") >= 4 {
		return content
	}

	normalized := regexp.MustCompile(`([.؟!])\s+`).ReplaceAllString(content, "$1\n")
	sentences := strings.Split(normalized, "\n")

	var parts []string
	var current []string
	wordCount := 0

	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}

		current = append(current, sentence)
		wordCount += countWords(sentence)

		if wordCount >= 55 {
			parts = append(parts, strings.Join(current, " "))
			current = nil
			wordCount = 0
		}
	}

	if len(current) > 0 {
		parts = append(parts, strings.Join(current, " "))
	}

	return strings.Join(parts, "\n\n")
}

func applyArabicLinguisticFilter(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}

	replacements := map[string]string{
		"عدم المفاهيم":       "عدم فهم المفاهيم",
		"هذا الدقة":          "هذه الدقة",
		"هذا الخطة":          "هذه الخطة",
		"هذا الطريقة":        "هذه الطريقة",
		"هذا الفكرة":         "هذه الفكرة",
		"هذا المرحلة":        "هذه المرحلة",
		"هذا المهارة":        "هذه المهارة",
		"هذا النقطة":         "هذه النقطة",
		"هذا الفقرة":         "هذه الفقرة",
		"هذا الخطوة":         "هذه الخطوة",
		"هذه الهدف":          "هذا الهدف",
		"هذه الدرس":          "هذا الدرس",
		"مكماًلاً":           "مكملاً",
		"لل نجاح":            "للنجاح",
		"interaction humanي": "التفاعل الإنساني",
	}

	for old, fixed := range replacements {
		content = strings.ReplaceAll(content, old, fixed)
	}

	content = regexp.MustCompile(`[A-Za-z]+[\x{0600}-\x{06FF}]+`).ReplaceAllString(content, "")
	content = regexp.MustCompile(`[\x{0600}-\x{06FF}]+[A-Za-z]+`).ReplaceAllString(content, "")
	content = regexp.MustCompile(`لل\s+([\x{0600}-\x{06FF}])`).ReplaceAllString(content, "لل$1")
	content = regexp.MustCompile(`[ \t]+([،؛:.؟!])`).ReplaceAllString(content, "$1")
	content = regexp.MustCompile(`([،؛:.؟!])([^\s\n])`).ReplaceAllString(content, "$1 $2")
	content = regexp.MustCompile(`[،]{2,}`).ReplaceAllString(content, "،")
	content = regexp.MustCompile(`[.]{2,}`).ReplaceAllString(content, ".")
	content = regexp.MustCompile(`[؟]{2,}`).ReplaceAllString(content, "؟")
	content = regexp.MustCompile(`[!]{2,}`).ReplaceAllString(content, "!")
	content = regexp.MustCompile(`[ \t]{2,}`).ReplaceAllString(content, " ")
	content = regexp.MustCompile(`[ \t]+\n`).ReplaceAllString(content, "\n")
	content = regexp.MustCompile(`\n[ \t]+`).ReplaceAllString(content, "\n")

	return strings.TrimSpace(content)
}

func improveHeadings(content string) string {
	lines := strings.Split(content, "\n")
	result := make([]string, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "الهدف هو") {
			line = "أهداف الخطة العلاجية"
		}
		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

func removeMarkdown(s string) string {
	for _, marker := range []string{"###", "##", "#", "**", "__", "```"} {
		s = strings.ReplaceAll(s, marker, "")
	}

	s = regexp.MustCompile(`(?m)^\s*[\*\-]\s+`).ReplaceAllString(s, "")

	return strings.TrimSpace(s)
}

func removeForeignNoise(s string) string {
	s = regexp.MustCompile(`[\x{4E00}-\x{9FFF}\x{3040}-\x{30FF}\x{AC00}-\x{D7AF}\x{0400}-\x{04FF}]`).ReplaceAllString(s, "")

	lines := strings.Split(s, "\n")
	cleanedLines := make([]string, 0, len(lines))
	latinWord := regexp.MustCompile(`^[A-Za-z]{3,}$`)

	for _, line := range lines {
		words := strings.Fields(line)
		cleanedWords := make([]string, 0, len(words))

		for _, word := range words {
			cleanWord := strings.Trim(word, ".,،:؛!؟()[]{}\"'")
			if latinWord.MatchString(cleanWord) {
				upper := strings.ToUpper(cleanWord)
				if upper != "SEO" && upper != "FAQ" && upper != "PDF" {
					continue
				}
			}

			cleanedWords = append(cleanedWords, word)
		}

		cleanedLines = append(cleanedLines, strings.Join(cleanedWords, " "))
	}

	return strings.Join(cleanedLines, "\n")
}

func autoGenerateFAQ(title, content string) []FAQItem {
	plain := strings.TrimSpace(stripHTML(content))
	var paragraphs []string

	for _, p := range strings.Split(plain, "\n") {
		p = strings.TrimSpace(p)
		if countWords(p) >= 18 {
			paragraphs = append(paragraphs, p)
		}
	}

	if len(paragraphs) == 0 {
		return nil
	}

	faq := []FAQItem{
		{
			Question: "ما المقصود بـ " + title + "؟",
			Answer:   truncateRunes(paragraphs[0], 220),
		},
	}

	if len(paragraphs) >= 2 {
		faq = append(faq, FAQItem{
			Question: "ما أهمية " + title + "؟",
			Answer:   truncateRunes(paragraphs[len(paragraphs)-1], 220),
		})
	}

	return faq
}

func formatContentToHTML(content string) string {
	content = strings.TrimSpace(stripHTML(content))
	if content == "" {
		return ""
	}

	lines := strings.Split(content, "\n")
	var result []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		escaped := htmlEscape(line)
		runeLen := len([]rune(line))

		endsWithPunct := strings.HasSuffix(line, ".") ||
			strings.HasSuffix(line, "،") ||
			strings.HasSuffix(line, "!") ||
			strings.HasSuffix(line, "؟") ||
			strings.HasSuffix(line, "?") ||
			strings.HasSuffix(line, ":")

		if runeLen <= 65 && !endsWithPunct {
			result = append(result, "<h2>"+escaped+"</h2>")
		} else {
			result = append(result, "<p>"+escaped+"</p>")
		}
	}

	return strings.Join(result, "\n")
}

func enhanceKeywords(html string, keywords []string) string {
	for i, kw := range keywords {
		if i >= 2 || len([]rune(kw)) < 2 {
			break
		}

		escapedKw := htmlEscape(kw)
		strong := "<strong>" + escapedKw + "</strong>"

		if strings.Contains(html, escapedKw) && !strings.Contains(html, strong) {
			html = strings.Replace(html, escapedKw, strong, 1)
		}
	}

	return html
}

func generateFeaturedSnippet(article *SEOArticle) string {
	if article == nil {
		return ""
	}

	plain := strings.TrimSpace(stripHTML(article.Content))
	if plain == "" {
		return ""
	}

	sentences := regexp.MustCompile(`[.؟!]\s+`).Split(plain, -1)

	var selected []string
	wordCount := 0

	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}

		selected = append(selected, sentence)
		wordCount += countWords(sentence)

		if wordCount >= 35 {
			break
		}
	}

	snippet := strings.Join(selected, ". ")
	snippet = strings.TrimSpace(snippet)

	if snippet != "" && !strings.HasSuffix(snippet, ".") && !strings.HasSuffix(snippet, "؟") {
		snippet += "."
	}

	return truncateRunes(snippet, 320)
}

func generateTitleVariants(title string) []string {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil
	}

	isArabic := containsArabic(title)
	var variants []string

	if isArabic {
		variants = append(variants,
			title,
			title+" بطريقة مبسطة",
			title+" مع شرح كامل",
			"أفضل "+title,
			title+" خطوة بخطوة",
		)
	} else {
		variants = append(variants,
			title,
			title+" simplified",
			title+" complete guide",
			"Best "+title,
			title+" step by step",
		)
	}

	cleaned := make([]string, 0, len(variants))
	seen := make(map[string]bool, len(variants))

	for _, v := range variants {
		v = cleanPlainText(v, isArabic)
		if v == "" {
			continue
		}

		if len([]rune(v)) > 70 {
			v = truncateRunes(v, 70)
		}

		key := strings.ToLower(v)
		if !seen[key] {
			seen[key] = true
			cleaned = append(cleaned, v)
		}
	}

	return cleaned
}

func generateInternalLinks(title string, keywords []string, contentType string) []InternalLink {
	text := strings.ToLower(title + " " + strings.Join(keywords, " "))
	searchTerm := bestInternalSearchTerm(title, keywords)
	links := []InternalLink{
		{
			Title: "ابحث عن محتوى مشابه",
			URL:   searchURL(searchTerm, ""),
		},
	}

	if contentType == "post" {
		links = append(links,
			InternalLink{Title: "المزيد من المنشورات", URL: "/posts"},
			InternalLink{Title: "الصفحة الرئيسية", URL: "/"},
		)
	} else {
		switch {
		case strings.Contains(text, "اختبار") || strings.Contains(text, "امتحان") || strings.Contains(text, "نماذج"):
			links = append(links, InternalLink{Title: "نماذج واختبارات تعليمية", URL: searchURL(searchTerm, "exam")})
		case strings.Contains(text, "ورقة") || strings.Contains(text, "أوراق") || strings.Contains(text, "عمل"):
			links = append(links, InternalLink{Title: "أوراق عمل مرتبطة", URL: searchURL(searchTerm, "worksheet")})
		case strings.Contains(text, "خطة") || strings.Contains(text, "علاجية") || strings.Contains(text, "دراسية"):
			links = append(links, InternalLink{Title: "خطط دراسية وعلاجية", URL: searchURL(searchTerm, "plan")})
		case strings.Contains(text, "كتاب") || strings.Contains(text, "ملخص"):
			links = append(links, InternalLink{Title: "كتب وملخصات تعليمية", URL: searchURL(searchTerm, "book")})
		}

		links = append(links,
			InternalLink{Title: "تصفح الصفوف الدراسية", URL: "/classes"},
			InternalLink{Title: "الخدمات التعليمية", URL: "/services"},
		)
	}

	links = uniqueInternalLinks(links)
	if len(links) > 3 {
		links = links[:3]
	}

	return links
}

func bestInternalSearchTerm(title string, keywords []string) string {
	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if len([]rune(keyword)) >= 3 {
			return keyword
		}
	}

	title = strings.TrimSpace(title)
	if title == "" {
		return "مواد تعليمية"
	}

	words := strings.Fields(title)
	if len(words) > 5 {
		words = words[:5]
	}

	return strings.Join(words, " ")
}

func searchURL(query, fileType string) string {
	values := url.Values{}
	query = strings.TrimSpace(query)
	if query != "" {
		values.Set("q", query)
	}
	if fileType != "" {
		values.Set("type", fileType)
	}

	encoded := values.Encode()
	if encoded == "" {
		return "/search"
	}

	return "/search?" + encoded
}

func uniqueInternalLinks(links []InternalLink) []InternalLink {
	seen := make(map[string]bool, len(links))
	result := make([]InternalLink, 0, len(links))

	for _, link := range links {
		link.Title = cleanPlainText(link.Title, containsArabic(link.Title))
		link.URL = strings.TrimSpace(link.URL)
		if link.Title == "" || !isSafeInternalURL(link.URL) || seen[link.URL] {
			continue
		}

		seen[link.URL] = true
		result = append(result, link)
	}

	return result
}

func isSafeInternalURL(rawURL string) bool {
	if rawURL == "" || !strings.HasPrefix(rawURL, "/") || strings.HasPrefix(rawURL, "//") {
		return false
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.IsAbs() {
		return false
	}

	switch parsed.Path {
	case "/search", "/classes", "/services":
		return true
	default:
		return false
	}
}

func injectInternalLinks(html string, links []InternalLink) string {
	if html == "" || len(links) == 0 {
		return html
	}

	if strings.Contains(html, `class="related-links"`) {
		return html
	}

	var box strings.Builder

	box.WriteString(`<div class="related-links">`)
	box.WriteString(`<h2>روابط تعليمية مفيدة</h2>`)
	box.WriteString(`<ul>`)

	for _, link := range links {
		title := htmlEscape(link.Title)
		url := htmlEscape(link.URL)
		box.WriteString(`<li><a href="` + url + `">` + title + `</a></li>`)
	}

	box.WriteString(`</ul>`)
	box.WriteString(`</div>`)

	return html + "\n" + box.String()
}

func calculateSEOScore(article *SEOArticle) (int, []string) {
	if article == nil {
		return 0, []string{"المقال غير موجود"}
	}

	score := 100
	var issues []string

	wordCount := article.WordCount
	if wordCount == 0 {
		wordCount = countWords(stripHTML(article.Content))
	}

	if wordCount < 300 {
		score -= 10
		issues = append(issues, "المقال أقل من 300 كلمة")
	}

	if wordCount > 900 {
		score -= 10
		issues = append(issues, "المقال طويل وقد يحتاج تقسيمًا أفضل")
	}

	if len([]rune(article.MetaDescription)) < 100 {
		score -= 10
		issues = append(issues, "وصف Meta قصير")
	}

	if len([]rune(article.MetaDescription)) > 180 {
		score -= 10
		issues = append(issues, "وصف Meta طويل")
	}

	if len(article.Keywords) < 5 {
		score -= 10
		issues = append(issues, "عدد الكلمات المفتاحية قليل")
	}

	if len(article.FAQ) < 2 {
		score -= 10
		issues = append(issues, "قسم الأسئلة الشائعة غير كافٍ")
	}

	if article.FeaturedSnippet == "" {
		score -= 10
		issues = append(issues, "لا يوجد Featured Snippet")
	}

	if len(article.InternalLinks) == 0 {
		score -= 10
		issues = append(issues, "لا توجد روابط داخلية")
	}

	if score < 0 {
		score = 0
	}

	return score, issues
}

func generateArticleSchema(article *SEOArticle) string {
	type schemaAuthor struct {
		Type string `json:"@type"`
		Name string `json:"name"`
	}

	type schemaPage struct {
		Type string `json:"@type"`
		ID   string `json:"@id"`
	}

	type articleSchema struct {
		Context        string       `json:"@context"`
		Type           string       `json:"@type"`
		Headline       string       `json:"headline"`
		Description    string       `json:"description"`
		Abstract       string       `json:"abstract,omitempty"`
		Author         schemaAuthor `json:"author"`
		MainEntityPage schemaPage   `json:"mainEntityOfPage"`
	}

	pageURL := siteBaseURL + "/" + article.Slug

	schema := articleSchema{
		Context:     "https://schema.org",
		Type:        "Article",
		Headline:    article.Title,
		Description: article.MetaDescription,
		Abstract:    article.FeaturedSnippet,
		Author:      schemaAuthor{Type: "Organization", Name: "Aleman Center"},
		MainEntityPage: schemaPage{
			Type: "WebPage",
			ID:   pageURL,
		},
	}

	data, err := json.Marshal(schema)
	if err != nil {
		return ""
	}

	return `<script type="application/ld+json">` + string(data) + `</script>`
}

func generateFAQSchema(faq []FAQItem) string {
	if len(faq) == 0 {
		return ""
	}

	type answer struct {
		Type string `json:"@type"`
		Text string `json:"text"`
	}

	type question struct {
		Type           string `json:"@type"`
		Name           string `json:"name"`
		AcceptedAnswer answer `json:"acceptedAnswer"`
	}

	type faqSchema struct {
		Context    string     `json:"@context"`
		Type       string     `json:"@type"`
		MainEntity []question `json:"mainEntity"`
	}

	entities := make([]question, 0, len(faq))

	for _, f := range faq {
		entities = append(entities, question{
			Type: "Question",
			Name: f.Question,
			AcceptedAnswer: answer{
				Type: "Answer",
				Text: f.Answer,
			},
		})
	}

	schema := faqSchema{
		Context:    "https://schema.org",
		Type:       "FAQPage",
		MainEntity: entities,
	}

	data, err := json.Marshal(schema)
	if err != nil {
		return ""
	}

	return `<script type="application/ld+json">` + string(data) + `</script>`
}

func extractAPIError(bodyBytes []byte) string {
	var errData map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &errData); err != nil {
		return ""
	}

	errVal, ok := errData["error"]
	if !ok {
		return ""
	}

	switch v := errVal.(type) {
	case string:
		return v
	case map[string]interface{}:
		if msg, ok := v["message"].(string); ok {
			return msg
		}
		if typ, ok := v["type"].(string); ok {
			return typ
		}
	}

	return ""
}

func stripHTML(s string) string {
	return regexp.MustCompile(`<[^>]*>`).ReplaceAllString(s, "")
}

func countWords(s string) int {
	return len(strings.Fields(s))
}

func containsArabic(s string) bool {
	return regexp.MustCompile(`[\x{0600}-\x{06FF}]`).MatchString(s)
}

func arabicRatio(s string) float64 {
	letters := regexp.MustCompile(`\p{L}`).FindAllString(s, -1)
	if len(letters) == 0 {
		return 0
	}

	arabicLetters := regexp.MustCompile(`[\x{0600}-\x{06FF}]`).FindAllString(s, -1)
	return float64(len(arabicLetters)) / float64(len(letters))
}

func hasBadAIIntro(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))

	badStarts := []string{
		"إليك",
		"هذا المقال",
		"في هذا المقال",
		"سأكتب",
		"بالطبع",
		"here is",
		"this article",
		"in this article",
		"of course",
	}

	for _, start := range badStarts {
		if strings.HasPrefix(lower, strings.ToLower(start)) {
			return true
		}
	}

	return false
}

func hasExcessiveRepetition(s string) bool {
	normalized := strings.ToLower(s)
	normalized = regexp.MustCompile(`[^\p{L}\p{N}\s]+`).ReplaceAllString(normalized, "")
	normalized = regexp.MustCompile(`\s+`).ReplaceAllString(normalized, " ")

	words := strings.Fields(normalized)
	if len(words) < 100 {
		return false
	}

	phraseCount := make(map[string]int)

	for i := 0; i+6 <= len(words); i++ {
		phrase := strings.Join(words[i:i+6], " ")
		phraseCount[phrase]++

		if phraseCount[phrase] >= 3 {
			return true
		}
	}

	return false
}

func looksIncomplete(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}

	if countWords(s) < 50 {
		return false
	}

	incompleteSuffixes := []string{
		"إن",
		"أن",
		"لأن",
		"حيث",
		"because",
		"and",
		"or",
		"the",
		"a",
	}

	lower := strings.ToLower(s)
	for _, suffix := range incompleteSuffixes {
		if strings.HasSuffix(lower, strings.ToLower(suffix)) {
			return true
		}
	}

	return false
}

func makeSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))

	replacements := map[string]string{
		"أ": "ا",
		"إ": "ا",
		"آ": "ا",
		"ة": "ه",
		"ى": "ي",
		"ؤ": "و",
		"ئ": "ي",
	}

	for old, rep := range replacements {
		s = strings.ReplaceAll(s, old, rep)
	}

	s = regexp.MustCompile(`[^\p{L}\p{N}]+`).ReplaceAllString(s, "-")
	s = regexp.MustCompile(`-+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")

	if s == "" {
		return "article"
	}

	if runes := []rune(s); len(runes) > 120 {
		s = strings.Trim(string(runes[:120]), "-")
	}

	return s
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")

	return s
}

func truncate(s string, max int) string {
	runes := []rune(s)

	if len(runes) <= max {
		return s
	}

	return string(runes[:max]) + "..."
}

func truncateRunes(s string, max int) string {
	runes := []rune(s)

	if len(runes) <= max {
		return s
	}

	return strings.TrimSpace(string(runes[:max]))
}
