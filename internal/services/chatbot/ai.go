package chatbot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	repo "github.com/alemancenter/fiber-api/internal/repositories/chatbot"
)

const (
	chatbotAIBaseURL       = "https://api.together.ai/v1"
	chatbotAIDefaultModel  = "openai/gpt-oss-20b"
	chatbotAIRequestTimout = 14 * time.Second
)

var chatbotAIDefaultFallbackModels = []string{
	"openai/gpt-oss-20b",
	"meta-llama/Llama-3.3-70B-Instruct-Turbo",
	"Qwen/Qwen3-235B-A22B-Instruct-2507-tput",
	"zai-org/GLM-5.1",
	"google/gemma-3n-E4B-it",
	"openai/gpt-oss-120b",
}

type chatbotAIResult struct {
	Answer      string   `json:"answer"`
	Suggestions []string `json:"suggestions"`
	Model       string   `json:"model"`
	Provider    string   `json:"provider"`
	Tokens      int      `json:"tokens"`
	Used        bool     `json:"used"`
}

type chatbotAIClient struct {
	enabled bool
	apiKey  string
	baseURL string
	models  []string
	http    *http.Client
}

type togetherChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type togetherChatRequest struct {
	Model       string                `json:"model"`
	Messages    []togetherChatMessage `json:"messages"`
	Temperature float64               `json:"temperature"`
	MaxTokens   int                   `json:"max_tokens"`
}

type togetherChatResponse struct {
	Choices []struct {
		Message togetherChatMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

type aiJSONAnswer struct {
	Answer      string   `json:"answer"`
	Suggestions []string `json:"suggestions"`
}

func newChatbotAIClient() chatbotAIClient {
	apiKey := firstEnvNonEmpty("CHATBOT_AI_API_KEY", "TOGETHER_AI_API_KEY", "TOGETHER_AI_KEY", "TOGETHER_API_KEY")
	enabledRaw := strings.ToLower(strings.TrimSpace(os.Getenv("CHATBOT_AI_ENABLED")))
	enabled := apiKey != "" && enabledRaw != "false" && enabledRaw != "0" && enabledRaw != "off"
	baseURL := strings.TrimRight(firstStringNonEmpty(os.Getenv("CHATBOT_AI_BASE_URL"), os.Getenv("TOGETHER_AI_BASE_URL"), chatbotAIBaseURL), "/")
	models := parseCSVModels(firstStringNonEmpty(os.Getenv("AI_MODELS_CHATBOT"), os.Getenv("CHATBOT_AI_MODELS")))
	primary := firstStringNonEmpty(os.Getenv("CHATBOT_AI_MODEL"), os.Getenv("TOGETHER_AI_MODEL"), chatbotAIDefaultModel)
	models = uniqueChatbotModels(append([]string{primary}, append(models, chatbotAIDefaultFallbackModels...)...))
	return chatbotAIClient{enabled: enabled, apiKey: strings.TrimSpace(apiKey), baseURL: baseURL, models: models, http: &http.Client{Timeout: chatbotAIRequestTimout + 2*time.Second}}
}

func firstEnvNonEmpty(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func firstStringNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func parseCSVModels(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if model := strings.TrimSpace(part); model != "" {
			out = append(out, model)
		}
	}
	return out
}

func uniqueChatbotModels(models []string) []string {
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

func shouldUseChatbotAI(message, intent, step, source string, links []repo.ContentResult, confidence float64) bool {
	if isSensitiveNoAI(intent) {
		return false
	}
	m := normalizeArabic(strings.ToLower(message))
	if isNoiseOrEmojiOnly(message) || isThanksMessage(message) || isProfanityOrFrustration(message) {
		return false
	}
	if strings.Contains(m, "api") || strings.Contains(m, "jwt") || strings.Contains(m, "token") || strings.Contains(m, "redis") || strings.Contains(m, "database") {
		return false
	}
	switch intent {
	case "contact_support", "site_usage", "site_services", "about_site", "open_classes", "open_search", "download_problem", "download_location", "permission_problem", "file_not_found", "email_verification_problem", "auth_login_problem", "auth_register_problem", "password_reset_problem", "social_login_problem", "request_content", "thanks", "frustration":
		return false
	}
	if source == "knowledge_base" && confidence >= 0.85 {
		return false
	}
	if source == "content_search" && len(links) == 0 {
		return false
	}
	if len(links) > 0 {
		return true
	}
	return false
}

func isSensitiveNoAI(intent string) bool {
	switch intent {
	case "unsupported_phone_feature", "account_lookup_privacy", "privacy_request", "contact_support", "site_usage", "site_services", "about_site", "open_classes", "open_search", "download_problem", "download_location", "permission_problem", "file_not_found", "email_verification_problem", "auth_login_problem", "auth_register_problem", "password_reset_problem", "social_login_problem", "request_content", "thanks", "frustration":
		return true
	default:
		return false
	}
}

func chatbotAIAllowed(countryID database.CountryID, userID *uint, ip string) (bool, string) {
	if strings.TrimSpace(os.Getenv("CHATBOT_AI_DISABLE_LIMITS")) == "true" {
		return true, ""
	}
	guestLimit := envInt("CHATBOT_AI_GUEST_LIMIT_10M", 5)
	userLimit := envInt("CHATBOT_AI_USER_LIMIT_10M", 15)
	globalLimit := envInt("CHATBOT_AI_DAILY_LIMIT", 1000)
	actor := "guest:" + ip
	limit := guestLimit
	if userID != nil && *userID > 0 {
		actor = "user:" + strconv.FormatUint(uint64(*userID), 10)
		limit = userLimit
	}
	actorHash := sha256.Sum256([]byte(actor))
	country := database.CountryCode(countryID)
	redis := database.GetRedis()
	ctx, cancel := context.WithTimeout(context.Background(), 900*time.Millisecond)
	defer cancel()
	windowKey := redis.Key("chatbot_ai", country, "10m", fmt.Sprintf("%x", actorHash))
	globalKey := redis.Key("chatbot_ai", country, time.Now().Format("2006-01-02"), "global")
	actorCount, err := redis.IncrBy(ctx, windowKey, 1)
	if err != nil {
		return false, "ai_rate_limit_unavailable"
	}
	if actorCount == 1 {
		_ = redis.Expire(ctx, windowKey, 10*time.Minute)
	}
	if int(actorCount) > limit {
		return false, "ai_actor_limit"
	}
	globalCount, err := redis.IncrBy(ctx, globalKey, 1)
	if err != nil {
		return false, "ai_rate_limit_unavailable"
	}
	if globalCount == 1 {
		_ = redis.Expire(ctx, globalKey, 24*time.Hour)
	}
	if int(globalCount) > globalLimit {
		return false, "ai_daily_limit"
	}
	return true, ""
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func (c chatbotAIClient) Generate(ctx context.Context, req chatbotAIRequest) chatbotAIResult {
	if !c.enabled || c.apiKey == "" || len(c.models) == 0 {
		return chatbotAIResult{}
	}
	systemPrompt, userPrompt := buildChatbotAIPrompt(req)
	for _, model := range c.models {
		result, err := c.callTogether(ctx, model, systemPrompt, userPrompt)
		if err == nil && strings.TrimSpace(result.Answer) != "" {
			result.Answer = sanitizeAIAnswer(result.Answer, req.Links)
			result.Suggestions = sanitizeAISuggestions(result.Suggestions)
			if result.Answer != "" {
				result.Model = model
				result.Provider = "together_ai"
				result.Used = true
				return result
			}
		}
	}
	return chatbotAIResult{}
}

type chatbotAIRequest struct {
	Message        string
	Intent         string
	Step           string
	LanguageHint   string
	CurrentAnswer  string
	Entities       searchEntities
	Links          []repo.ContentResult
	AllowedActions []ChatbotAction
	PreviousIntent string
	Authenticated  bool
}

func buildChatbotAIPrompt(req chatbotAIRequest) (string, string) {
	linksJSON, _ := json.Marshal(req.Links)
	actionsJSON, _ := json.Marshal(req.AllowedActions)
	entitiesJSON, _ := json.Marshal(req.Entities)
	system := `أنت مساعد دعم ذكي لموقع Alemancenter التعليمي.
القواعد الصارمة:
- أجب بنفس لغة أو لهجة المستخدم قدر الإمكان.
- استخدم فقط المعلومات والروابط الموجودة في السياق المرسل لك.
- ممنوع اختراع روابط، صفحات، أسماء ملفات، أو نتائج غير موجودة في links.
- لا تذكر صفحة دعم، FAQ، دردشة مباشرة، إشعار داخل الموقع، أو live chat. المتاح فقط: صفحة التواصل /contact-us وروابط actions/links المرسلة.
- ممنوع تأكيد وجود أو عدم وجود أي بريد إلكتروني أو حساب مستخدم.
- ممنوع طلب كلمة المرور أو رموز التفعيل أو كشف معلومات داخلية مثل API/JWT/Redis/Database/مسارات السيرفر.
- إذا كانت المشكلة تقنية أو حسابية، أعط خطوات آمنة وواضحة.
- المنصة حاليًا لا تدعم إنشاء الحساب أو التفعيل أو تسجيل الدخول أو استعادة كلمة المرور عبر رقم الهاتف أو SMS أو WhatsApp أو اسم المستخدم.
- لا تذكر رقم الهاتف أو WhatsApp أو SMS أو اسم المستخدم كطريقة تسجيل/تفعيل/استعادة. الطريقة المتاحة حاليًا هي البريد الإلكتروني فقط، مع Google/Facebook إذا كانت الأزرار ظاهرة في صفحة الدخول.
- إذا سأل المستخدم هل يمكن بدون بريد أو عبر الهاتف/واتساب/SMS، قل بوضوح إن هذا غير مدعوم حاليًا وأن البريد الإلكتروني مطلوب.
- إذا توجد نتائج بحث، اذكر أنها نتائج مقترحة من الموقع ولا تدّعي أنها الوحيدة.
- أعد JSON فقط بالشكل: {"answer":"...","suggestions":["...","...","..."]}`
	user := fmt.Sprintf(`سؤال المستخدم: %s
intent: %s
step: %s
previous_intent: %s
authenticated: %v
entities: %s
current_safe_answer: %s
site_links_json_from_database_files_articles_posts: %s
allowed_actions_json: %s

اكتب ردًا مختصرًا ومفيدًا، منظمًا بخطوات عند الحاجة. الاقتراحات يجب أن تكون قصيرة وعملية، ولا تزيد عن 3.`, req.Message, req.Intent, req.Step, req.PreviousIntent, req.Authenticated, string(entitiesJSON), req.CurrentAnswer, string(linksJSON), string(actionsJSON))
	return system, user
}

func (c chatbotAIClient) callTogether(ctx context.Context, model, systemPrompt, userPrompt string) (chatbotAIResult, error) {
	payload := togetherChatRequest{Model: model, Temperature: 0.25, MaxTokens: envInt("CHATBOT_AI_MAX_TOKENS", 520), Messages: []togetherChatMessage{{Role: "system", Content: systemPrompt}, {Role: "user", Content: userPrompt}}}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return chatbotAIResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return chatbotAIResult{}, err
	}
	defer resp.Body.Close()
	var parsed togetherChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return chatbotAIResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if parsed.Error != nil && parsed.Error.Message != "" {
			return chatbotAIResult{}, errors.New(parsed.Error.Message)
		}
		return chatbotAIResult{}, fmt.Errorf("ai status %d", resp.StatusCode)
	}
	if len(parsed.Choices) == 0 {
		return chatbotAIResult{}, fmt.Errorf("empty ai response")
	}
	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	var answer aiJSONAnswer
	if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &answer); err != nil {
		answer.Answer = content
	}
	return chatbotAIResult{Answer: strings.TrimSpace(answer.Answer), Suggestions: answer.Suggestions, Tokens: parsed.Usage.TotalTokens}, nil
}

func sanitizeAIAnswer(answer string, links []repo.ContentResult) string {
	_ = links
	answer = trim(strings.TrimSpace(answer), 1600)
	if strings.HasPrefix(strings.TrimSpace(answer), "{\"answer\"") || strings.Contains(answer, "\"suggestions\"") {
		return ""
	}
	if answer == "" {
		return ""
	}
	forbidden := []string{"api key", "jwt", "frontend-key", "redis key", "database", "secret", "token="}
	lower := strings.ToLower(answer)
	for _, word := range forbidden {
		if strings.Contains(lower, word) {
			return ""
		}
	}
	answer = sanitizeUnsupportedFeatures(answer)
	// Remove raw external links. The UI renders safe links from backend actions/results only.
	answer = regexpHTTP.ReplaceAllString(answer, "")
	return strings.TrimSpace(answer)
}

var regexpHTTP = regexpMustCompile(`https?://[^\s)]+`)

func regexpMustCompile(pattern string) *regexp.Regexp { return regexp.MustCompile(pattern) }

func sanitizeAISuggestions(suggestions []string) []string {
	out := make([]string, 0, 3)
	seen := map[string]bool{}
	for _, suggestion := range suggestions {
		suggestion = trim(strings.Join(strings.Fields(strings.TrimSpace(suggestion)), " "), 80)
		if suggestion == "" || seen[suggestion] {
			continue
		}
		seen[suggestion] = true
		out = append(out, suggestion)
		if len(out) >= 3 {
			break
		}
	}
	return out
}
