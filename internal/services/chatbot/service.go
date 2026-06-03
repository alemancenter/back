package chatbot

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	repo "github.com/alemancenter/fiber-api/internal/repositories/chatbot"
)

type MessageRequest struct {
	Message     string `json:"message"`
	SessionID   uint   `json:"session_id"`
	GuestID     string `json:"guest_id"`
	CountryCode string `json:"country_code"`
	PageURL     string `json:"page_url"`
}

type ChatbotAction struct {
	Label   string `json:"label"`
	Type    string `json:"type"` // link, message
	URL     string `json:"url,omitempty"`
	Message string `json:"message,omitempty"`
	Style   string `json:"style,omitempty"`
}

type MessageResponse struct {
	SessionID   uint                 `json:"session_id"`
	Answer      string               `json:"answer"`
	Intent      string               `json:"intent"`
	Step        string               `json:"step"`
	Confidence  float64              `json:"confidence"`
	SourceType  string               `json:"source_type"`
	Links       []repo.ContentResult `json:"links"`
	Actions     []ChatbotAction      `json:"actions"`
	Suggestions []string             `json:"suggestions"`
	MessageID   uint                 `json:"message_id"`
	AIUsed      bool                 `json:"ai_used"`
	AIModel     string               `json:"ai_model,omitempty"`
}

type Service interface {
	Reply(countryID database.CountryID, userID *uint, ip, userAgent string, req MessageRequest) (*MessageResponse, error)
	Feedback(countryID database.CountryID, messageID uint, rating, comment string) error
	Suggestions() []string
	ListSessions(countryID database.CountryID, limit int) ([]models.ChatSession, error)
	GetSession(countryID database.CountryID, sessionID uint) (*models.ChatSession, error)
	ListKnowledge(countryID database.CountryID, countryCode string, limit int) ([]models.ChatKnowledgeBase, error)
	CreateKnowledge(countryID database.CountryID, item *models.ChatKnowledgeBase) error
	UpdateKnowledge(countryID database.CountryID, item *models.ChatKnowledgeBase) error
	DeleteKnowledge(countryID database.CountryID, id uint) error
}

type service struct{ repo repo.Repository }

func NewService(r repo.Repository) Service { return &service{repo: r} }

type flowDecision struct {
	Intent     string
	Step       string
	Answer     string
	Confidence float64
}

type searchEntities struct {
	Grade       string `json:"grade,omitempty"`
	Subject     string `json:"subject,omitempty"`
	Semester    string `json:"semester,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	RawQuery    string `json:"raw_query,omitempty"`
}

type sessionContext struct {
	LastUserMessage string         `json:"last_user_message,omitempty"`
	PageURL         string         `json:"page_url,omitempty"`
	SourceType      string         `json:"source_type,omitempty"`
	Search          searchEntities `json:"search,omitempty"`
}

func (s *service) Reply(countryID database.CountryID, userID *uint, ip, userAgent string, req MessageRequest) (*MessageResponse, error) {
	message := cleanMessage(req.Message)
	if message == "" {
		return &MessageResponse{Answer: "اكتب سؤالك بوضوح وسأساعدك مباشرة.", Intent: "empty", Step: "start", Confidence: 1, SourceType: "validation", Suggestions: s.Suggestions()}, nil
	}
	guestID := strings.TrimSpace(req.GuestID)
	if guestID == "" {
		guestID = "guest:" + ip
	}

	session, err := s.repo.FindOrCreateSession(countryID, userID, guestID)
	if err != nil {
		return nil, err
	}
	ctx := readSessionContext(session.ContextData)

	if isDownloadLocationQuestion(message) {
		intent := "download_location"
		step := "download_location_steps"
		confidence := 0.97
		answer := contextualAnswer(intent, step, message, session.LastIntent)
		metadata, _ := json.Marshal(map[string]interface{}{"page_url": req.PageURL, "step": step, "guard": "download_location"})
		_ = s.repo.CreateMessage(countryID, &models.ChatMessage{SessionID: session.ID, Role: "user", Message: message, Intent: intent, Confidence: confidence, SourceType: "user", Metadata: string(metadata), IPAddress: ip, UserAgent: trim(userAgent, 500)})
		actions := buildActions(intent, step, nil, searchEntities{})
		suggestions := nextSuggestionsForStep(intent, step, searchEntities{})
		assistantMetaJSON, _ := json.Marshal(map[string]interface{}{"step": step, "actions": actions, "guard": "download_location"})
		assistantMsg := models.ChatMessage{SessionID: session.ID, Role: "assistant", Message: answer, Intent: intent, Confidence: confidence, SourceType: "rules", Metadata: string(assistantMetaJSON)}
		if err := s.repo.CreateMessage(countryID, &assistantMsg); err != nil {
			return nil, err
		}
		_ = s.repo.UpdateSessionState(countryID, session.ID, intent, step, map[string]interface{}{"last_user_message": message, "page_url": req.PageURL, "source_type": "rules"})
		return &MessageResponse{SessionID: session.ID, Answer: answer, Intent: intent, Step: step, Confidence: confidence, SourceType: "rules", Actions: actions, Suggestions: suggestions, MessageID: assistantMsg.ID}, nil
	}

	if isCannotFindContentQuestion(message) {
		intent := "search_content"
		step := "content_refine"
		confidence := 0.94
		answer := "حتى أساعدك في إيجاد الملف بدقة، اكتب الطلب بهذه الصيغة:\n\nنوع الملف + المادة + الصف + الفصل\n\nمثال: اختبار نهائي فيزياء الصف التاسع الفصل الثاني.\n\nإذا كان لديك عنوان الملف كاملًا، أرسله كما هو وسأبحث عنه داخل الموقع."
		metadata, _ := json.Marshal(map[string]interface{}{"page_url": req.PageURL, "step": step, "guard": "cannot_find_content"})
		_ = s.repo.CreateMessage(countryID, &models.ChatMessage{SessionID: session.ID, Role: "user", Message: message, Intent: intent, Confidence: confidence, SourceType: "user", Metadata: string(metadata), IPAddress: ip, UserAgent: trim(userAgent, 500)})
		actions := buildActions(intent, step, nil, searchEntities{})
		suggestions := []string{"اكتب اسم الملف كاملًا", "فتح صفحة البحث", "فتح الصفوف"}
		assistantMetaJSON, _ := json.Marshal(map[string]interface{}{"step": step, "actions": actions, "guard": "cannot_find_content"})
		assistantMsg := models.ChatMessage{SessionID: session.ID, Role: "assistant", Message: answer, Intent: intent, Confidence: confidence, SourceType: "rules", Metadata: string(assistantMetaJSON)}
		if err := s.repo.CreateMessage(countryID, &assistantMsg); err != nil {
			return nil, err
		}
		_ = s.repo.UpdateSessionState(countryID, session.ID, intent, step, map[string]interface{}{"last_user_message": message, "page_url": req.PageURL, "source_type": "rules"})
		return &MessageResponse{SessionID: session.ID, Answer: answer, Intent: intent, Step: step, Confidence: confidence, SourceType: "rules", Actions: actions, Suggestions: suggestions, MessageID: assistantMsg.ID}, nil
	}

	if isGenericDownloadProblemQuestion(message) {
		intent := "download_problem"
		step := "download_diagnosis"
		confidence := 0.95
		answer := contextualAnswer(intent, step, message, session.LastIntent)
		metadata, _ := json.Marshal(map[string]interface{}{"page_url": req.PageURL, "step": step, "guard": "generic_download"})
		_ = s.repo.CreateMessage(countryID, &models.ChatMessage{SessionID: session.ID, Role: "user", Message: message, Intent: intent, Confidence: confidence, SourceType: "user", Metadata: string(metadata), IPAddress: ip, UserAgent: trim(userAgent, 500)})
		actions := buildActions(intent, step, nil, searchEntities{})
		suggestions := nextSuggestionsForStep(intent, step, searchEntities{})
		assistantMetaJSON, _ := json.Marshal(map[string]interface{}{"step": step, "actions": actions, "guard": "generic_download"})
		assistantMsg := models.ChatMessage{SessionID: session.ID, Role: "assistant", Message: answer, Intent: intent, Confidence: confidence, SourceType: "rules", Metadata: string(assistantMetaJSON)}
		if err := s.repo.CreateMessage(countryID, &assistantMsg); err != nil {
			return nil, err
		}
		_ = s.repo.UpdateSessionState(countryID, session.ID, intent, step, map[string]interface{}{"last_user_message": message, "page_url": req.PageURL, "source_type": "rules"})
		return &MessageResponse{SessionID: session.ID, Answer: answer, Intent: intent, Step: step, Confidence: confidence, SourceType: "rules", Actions: actions, Suggestions: suggestions, MessageID: assistantMsg.ID}, nil
	}

	if isUnsupportedPhoneOrUsernameQuestion(message) {
		intent := "unsupported_phone_feature"
		step := "email_only"
		confidence := 0.98
		answer := unsupportedFeatureAnswer()
		metadata, _ := json.Marshal(map[string]interface{}{"page_url": req.PageURL, "step": step, "feature_guard": true})
		_ = s.repo.CreateMessage(countryID, &models.ChatMessage{SessionID: session.ID, Role: "user", Message: message, Intent: intent, Confidence: confidence, SourceType: "user", Metadata: string(metadata), IPAddress: ip, UserAgent: trim(userAgent, 500)})

		actions := buildActions(intent, step, nil, searchEntities{})
		suggestions := []string{"لدي مشكلة في البريد", "كتبت البريد خطأ", "أريد التواصل مع الإدارة"}
		assistantMeta := map[string]interface{}{"step": step, "actions": actions, "feature_guard": true}
		assistantMetaJSON, _ := json.Marshal(assistantMeta)
		assistantMsg := models.ChatMessage{SessionID: session.ID, Role: "assistant", Message: answer, Intent: intent, Confidence: confidence, SourceType: "rules", Metadata: string(assistantMetaJSON)}
		if err := s.repo.CreateMessage(countryID, &assistantMsg); err != nil {
			return nil, err
		}

		ctx.LastUserMessage = message
		ctx.PageURL = req.PageURL
		ctx.SourceType = "rules"
		_ = s.repo.UpdateSessionState(countryID, session.ID, intent, step, map[string]interface{}{
			"last_user_message": ctx.LastUserMessage,
			"page_url":          ctx.PageURL,
			"source_type":       ctx.SourceType,
			"feature_guard":     true,
		})

		return &MessageResponse{SessionID: session.ID, Answer: answer, Intent: intent, Step: step, Confidence: confidence, SourceType: "rules", Actions: actions, Suggestions: suggestions, MessageID: assistantMsg.ID}, nil
	}

	detectedIntent, confidence := detectIntent(message)
	flow := resolveFlow(message, detectedIntent, session.LastIntent, session.CurrentStep, userID != nil)
	if flow.Intent != "" && flow.Intent != detectedIntent && flow.Confidence > confidence {
		confidence = flow.Confidence
	}
	intent := flow.Intent
	if intent == "" {
		intent = detectedIntent
	}
	step := flow.Step
	if step == "" {
		step = defaultStep(intent)
	}

	currentEntities := extractSearchEntities(message)
	mergedEntities := mergeSearchEntities(ctx.Search, currentEntities)
	if shouldKeepContentContext(intent, session.LastIntent, currentEntities, ctx.Search) {
		intent = "search_content"
		if step == "answer" || step == "" || step == defaultStep(detectedIntent) {
			step = "content_followup"
		}
		confidence = maxFloat(confidence, 0.86)
	}

	metadata, _ := json.Marshal(map[string]interface{}{"page_url": req.PageURL, "step": step, "entities": currentEntities})
	_ = s.repo.CreateMessage(countryID, &models.ChatMessage{SessionID: session.ID, Role: "user", Message: message, Intent: intent, Confidence: confidence, SourceType: "user", Metadata: string(metadata), IPAddress: ip, UserAgent: trim(userAgent, 500)})

	links := []repo.ContentResult{}
	answer := flow.Answer
	source := "flow"

	if shouldRunContentSearch(intent, message, currentEntities) {
		queries := relaxedSearchQueries(message, mergedEntities)
		for idx, searchQuery := range queries {
			candidateLinks, _ := s.repo.SearchContent(countryID, searchQuery, 8)
			// First attempts are strict. Later attempts are relaxed to avoid false "not found"
			// when the title uses قريب/مرادف مثل: اختبار بدل امتحان أو نموذج2 بدل نموذج 2.
			if idx <= 1 {
				candidateLinks = filterSearchResultsByEntities(mergedEntities, candidateLinks)
			}
			if len(candidateLinks) > 0 {
				links = candidateLinks
				break
			}
		}
		answer = buildSearchAnswer(mergedEntities, links)
		source = "content_search"
		if len(links) > 0 {
			step = "content_results"
		} else {
			step = "content_refine"
		}
	} else if answer == "" {
		knowledge, _ := s.repo.FindKnowledge(countryID, database.CountryCode(countryID), message, intent, 3)
		if len(knowledge) > 0 && !requiresContextualAnswer(message, intent, session.LastIntent) {
			answer = knowledge[0].Answer
			source = "knowledge_base"
		} else {
			answer = contextualAnswer(intent, step, message, session.LastIntent)
			source = "flow"
		}
	}

	actions := buildActions(intent, step, links, mergedEntities)
	suggestions := nextSuggestionsForStep(intent, step, mergedEntities)
	aiResult := chatbotAIResult{}
	aiStatus := "skipped"
	aiReason := "not_required"
	if shouldUseChatbotAI(message, intent, step, source, links, confidence) {
		if allowed, reason := chatbotAIAllowed(countryID, userID, ip); allowed {
			aiClient := newChatbotAIClient()
			aiCtx, cancel := context.WithTimeout(context.Background(), chatbotAIRequestTimout)
			aiResult = aiClient.Generate(aiCtx, chatbotAIRequest{
				Message:        message,
				Intent:         intent,
				Step:           step,
				CurrentAnswer:  answer,
				Entities:       mergedEntities,
				Links:          links,
				AllowedActions: actions,
				PreviousIntent: session.LastIntent,
				Authenticated:  userID != nil,
			})
			cancel()
			if aiResult.Used && aiResult.Answer != "" {
				answer = aiResult.Answer
				source = "ai_guarded_rag"
				confidence = maxFloat(confidence, 0.87)
				if len(aiResult.Suggestions) > 0 {
					suggestions = aiResult.Suggestions
				}
				aiStatus = "success"
				aiReason = ""
			} else {
				aiStatus = "failed"
				aiReason = "empty_or_disabled"
			}
		} else {
			aiStatus = "limited"
			aiReason = reason
		}
	}

	answer = sanitizeUnsupportedFeatures(answer)

	assistantMeta := map[string]interface{}{"step": step, "actions": actions, "entities": mergedEntities, "ai_used": aiResult.Used, "ai_model": aiResult.Model}
	if len(links) > 0 {
		assistantMeta["links"] = links
	}
	assistantMetaJSON, _ := json.Marshal(assistantMeta)
	assistantMsg := models.ChatMessage{SessionID: session.ID, Role: "assistant", Message: answer, Intent: intent, Confidence: confidence, SourceType: source, Metadata: string(assistantMetaJSON)}
	if err := s.repo.CreateMessage(countryID, &assistantMsg); err != nil {
		return nil, err
	}
	if aiStatus != "skipped" {
		_ = s.repo.CreateAIUsage(countryID, &models.ChatAIUsage{
			SessionID:   session.ID,
			MessageID:   assistantMsg.ID,
			Provider:    firstStringNonEmpty(aiResult.Provider, "together_ai"),
			Model:       aiResult.Model,
			Intent:      intent,
			Status:      aiStatus,
			Reason:      aiReason,
			Tokens:      aiResult.Tokens,
			CountryCode: database.CountryCode(countryID),
		})
	}

	ctx.LastUserMessage = message
	ctx.PageURL = req.PageURL
	ctx.SourceType = source
	if isContentIntent(intent) {
		ctx.Search = mergedEntities
	}
	_ = s.repo.UpdateSessionState(countryID, session.ID, intent, step, map[string]interface{}{
		"last_user_message": ctx.LastUserMessage,
		"page_url":          ctx.PageURL,
		"source_type":       ctx.SourceType,
		"search":            ctx.Search,
	})

	return &MessageResponse{SessionID: session.ID, Answer: answer, Intent: intent, Step: step, Confidence: confidence, SourceType: source, Links: links, Actions: actions, Suggestions: suggestions, MessageID: assistantMsg.ID, AIUsed: aiResult.Used, AIModel: aiResult.Model}, nil
}

func (s *service) Feedback(countryID database.CountryID, messageID uint, rating, comment string) error {
	return s.repo.CreateFeedback(countryID, &models.ChatFeedback{MessageID: messageID, Rating: trim(rating, 30), Comment: trim(comment, 1000)})
}

func (s *service) Suggestions() []string {
	return []string{"لا أستطيع تسجيل الدخول", "لا تصلني رسالة التفعيل", "عملت تحميل ولا أعرف أين أجد الملف", "ابحث عن امتحانات حاسوب صف ثامن فصل ثاني", "أريد خطة نمو مهني تربية فنية", "أريد التواصل مع الإدارة"}
}
func (s *service) ListSessions(countryID database.CountryID, limit int) ([]models.ChatSession, error) {
	return s.repo.ListSessions(countryID, limit)
}

func (s *service) GetSession(countryID database.CountryID, sessionID uint) (*models.ChatSession, error) {
	return s.repo.GetSessionWithMessages(countryID, sessionID)
}
func (s *service) ListKnowledge(countryID database.CountryID, countryCode string, limit int) ([]models.ChatKnowledgeBase, error) {
	return s.repo.ListKnowledge(countryID, countryCode, limit)
}
func (s *service) CreateKnowledge(countryID database.CountryID, item *models.ChatKnowledgeBase) error {
	return s.repo.CreateKnowledge(countryID, item)
}
func (s *service) UpdateKnowledge(countryID database.CountryID, item *models.ChatKnowledgeBase) error {
	return s.repo.UpdateKnowledge(countryID, item)
}
func (s *service) DeleteKnowledge(countryID database.CountryID, id uint) error {
	return s.repo.DeleteKnowledge(countryID, id)
}

func cleanMessage(v string) string {
	return trim(strings.Join(strings.Fields(strings.TrimSpace(v)), " "), 1200)
}
func trim(v string, max int) string {
	if max <= 0 || utf8.RuneCountInString(v) <= max {
		return v
	}
	r := []rune(v)
	return string(r[:max])
}

func readSessionContext(raw string) sessionContext {
	ctx := sessionContext{}
	if strings.TrimSpace(raw) == "" {
		return ctx
	}
	_ = json.Unmarshal([]byte(raw), &ctx)
	return ctx
}

func isDownloadLocationQuestion(message string) bool {
	m := normalizeArabic(strings.ToLower(message))
	return containsAny(m,
		"عملت تحميل ولا اجد",
		"عملت تحميل ولا أجد",
		"حملت ولا اجد",
		"حملت ولا أجد",
		"حملت وما لقيت",
		"حملت وما لقيته",
		"تم التحميل ولا اجد",
		"تم التحميل ولا أجد",
		"وين الملف بعد التحميل",
		"وين راح الملف",
		"وين بتنزل",
		"وين بكون",
		"وين الاقيه",
		"وين ألاقيه",
		"فين الاقيه",
		"ما بعرف وين",
		"اختفى الملف",
		"ما لقيت التنزيل",
	)
}

func isCannotFindContentQuestion(message string) bool {
	m := normalizeArabic(strings.ToLower(message))
	if isDownloadLocationQuestion(message) {
		return false
	}
	return containsAny(m,
		"لا استطيع ايجاد الملف",
		"لا أستطيع إيجاد الملف",
		"لا أستطيع ايجاد الملف",
		"لم اجد الملف",
		"لم أجد الملف",
		"ما لقيت الملف",
		"مش لاقي الملف",
		"مش لاقيه",
		"لا اجد الملف المطلوب",
		"لا أجد الملف المطلوب",
		"اين الملف",
		"أين الملف",
		"ابحث عن ملف",
		"أبحث عن ملف",
	)
}

func isGenericDownloadProblemQuestion(message string) bool {
	m := normalizeArabic(strings.ToLower(message))
	if isDownloadLocationQuestion(message) || isCannotFindContentQuestion(message) {
		return false
	}
	return containsAny(m,
		"لا استطيع تحميل ملف",
		"لا أستطيع تحميل ملف",
		"لا استطيع تحميل الملفات",
		"لا أستطيع تحميل الملفات",
		"مش قادر احمل",
		"مش قادر أحمل",
		"ما بقدر احمل",
		"ما بقدر أحمل",
		"التحميل لا يعمل",
		"ما يحمل",
		"ما بحمل",
	)
}

func resolveFlow(message, detectedIntent, lastIntent, currentStep string, authenticated bool) flowDecision {
	m := normalizeArabic(strings.ToLower(message))
	contains := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(m, normalizeArabic(strings.ToLower(w))) {
				return true
			}
		}
		return false
	}

	if contains("ما هي خدمات الموقع", "خدمات الموقع", "شو بتقدموا", "ماذا تقدم المنصة", "الخدمات التعليمية", "بنك الامتحانات", "اشتراك", "اشتراكات") {
		return flowDecision{Intent: "site_services", Step: "site_services", Confidence: 0.95, Answer: contextualAnswer("site_services", "site_services", message, lastIntent)}
	}
	if contains("من نحن", "عن الموقع", "عن المنصة", "ما هو موقع الايمان", "ما هو موقع الأيمان", "موقع الايمان") {
		return flowDecision{Intent: "about_site", Step: "about_site", Confidence: 0.94, Answer: contextualAnswer("about_site", "about_site", message, lastIntent)}
	}
	if contains("سياسة الخصوصية", "شروط الاستخدام", "سياسة الكوكيز", "حقوق الملكية", "إخلاء المسؤولية", "اخلاء المسؤولية", "سياسة التحرير") {
		return flowDecision{Intent: "privacy_request", Step: "legal_pages", Confidence: 0.95, Answer: contextualAnswer("privacy_request", "legal_pages", message, lastIntent)}
	}
	if contains("نموذج التواصل غير مهيأ", "recaptcha", "ريكابتشا", "لا استطيع ارسال رسالة", "لا أستطيع إرسال رسالة", "نموذج التواصل لا يعمل", "اتصل بنا لا يعمل") {
		return flowDecision{Intent: "contact_support", Step: "contact_form_not_ready", Confidence: 0.96, Answer: contextualAnswer("contact_support", "contact_form_not_ready", message, lastIntent)}
	}

	if contains("كيف استخدم الموقع", "كيف أستخدم الموقع", "طريقة استخدام الموقع", "استخدام الموقع", "كيف اتصفح", "كيف أتصفح", "كيف ابحث", "كيف أبحث") {
		return flowDecision{Intent: "site_usage", Step: "site_usage", Confidence: 0.96, Answer: contextualAnswer("site_usage", "site_usage", message, lastIntent)}
	}
	if contains("عرض الصفوف التعليمية", "اعرض الصفوف", "عرض الصفوف", "الصفوف التعليمية", "تصفح الصفوف", "أريد تصفح الصفوف", "اريد تصفح الصفوف") {
		return flowDecision{Intent: "open_classes", Step: "open_classes", Confidence: 0.97, Answer: contextualAnswer("open_classes", "open_classes", message, lastIntent)}
	}

	if isDownloadLocationQuestion(message) {
		return flowDecision{Intent: "download_location", Step: "download_location_steps", Confidence: 0.97, Answer: contextualAnswer("download_location", "download_location_steps", message, lastIntent)}
	}
	if isCannotFindContentQuestion(message) {
		return flowDecision{Intent: "search_content", Step: "content_refine", Confidence: 0.94, Answer: "حتى أساعدك في إيجاد الملف بدقة، اكتب الطلب بهذه الصيغة:\n\nنوع الملف + المادة + الصف + الفصل\n\nمثال: اختبار نهائي فيزياء الصف التاسع الفصل الثاني."}
	}
	if isGenericDownloadProblemQuestion(message) {
		return flowDecision{Intent: "download_problem", Step: "download_diagnosis", Confidence: 0.95, Answer: contextualAnswer("download_problem", "download_diagnosis", message, lastIntent)}
	}

	if isNoiseOrEmojiOnly(message) {
		return flowDecision{Intent: "general_question", Step: "unclear", Confidence: 0.97, Answer: "لم أفهم الطلب بوضوح. اكتب سؤالك بجملة قصيرة مثل: أريد امتحان رياضيات الصف التاسع الفصل الثاني، أو لا أستطيع تحميل ملف."}
	}
	if isThanksMessage(message) {
		return flowDecision{Intent: "thanks", Step: "thanks", Confidence: 0.98, Answer: "العفو، يسعدنا خدمتك. إذا احتجت ملفًا أو واجهت مشكلة في التفعيل أو التحميل، اكتب طلبك مباشرة."}
	}
	if isProfanityOrFrustration(message) {
		return flowDecision{Intent: "frustration", Step: "calm_support", Confidence: 0.96, Answer: "أفهم أن المشكلة مزعجة. اكتب لي باختصار ما الذي يحدث معك: هل المشكلة في التحميل، التفعيل، تسجيل الدخول، أم البحث عن ملف؟"}
	}
	if contains("اتصل بالدعم", "اتصل بدعم", "اتصل بفريق الدعم", "دعم الموقع", "الدعم الفني", "تواصل مع الدعم", "تواصل مع الإدارة", "اتصل بنا") {
		return flowDecision{Intent: "contact_support", Step: "contact_steps", Confidence: 0.96, Answer: contextualAnswer("contact_support", "contact_steps", message, lastIntent)}
	}
	if contains("فتح الصفوف", "افتح الصفوف", "صفحة الصفوف", "افتح صفحة الصفوف") {
		return flowDecision{Intent: "open_classes", Step: "open_classes", Confidence: 0.96, Answer: contextualAnswer("open_classes", "open_classes", message, lastIntent)}
	}
	if contains("فتح البحث", "افتح البحث", "فتح البحث بهذه الكلمات", "ابحث بهذه الكلمات") {
		return flowDecision{Intent: "open_search", Step: "open_search", Confidence: 0.94, Answer: contextualAnswer("open_search", "open_search", message, lastIntent)}
	}
	if contains("طلب إضافة ملف", "اضافة ملف", "إضافة ملف", "اضافة درس", "إضافة درس", "طلب ملف", "ملف غير متوفر") {
		return flowDecision{Intent: "request_content", Step: "request_content", Confidence: 0.95, Answer: contextualAnswer("request_content", "request_content", message, lastIntent)}
	}
	if contains("لا تملك صلاحية", "ما عندي صلاحية", "عدم وجود صلاحية", "غير مصرح", "غير مسموح") {
		return flowDecision{Intent: "permission_problem", Step: "permission_denied", Confidence: 0.96, Answer: contextualAnswer("permission_problem", "permission_denied", message, lastIntent)}
	}
	if contains("رابط التحقق منتهي", "رابط التحقق منتهي الصلاحيه", "رابط التفعيل منتهي", "انتهت صلاحية رابط", "منتهي الصلاحية") {
		return flowDecision{Intent: "email_verification_problem", Step: "expired_verification_link", Confidence: 0.96, Answer: contextualAnswer("email_verification_problem", "expired_verification_link", message, lastIntent)}
	}
	if contains("حملت ملف واحد", "ماطلع الثاني", "وما طلع الثاني", "احمل ملف ثاني", "احمل ملف ثاين", "وثالث", "كل الملفات", "ودي احمل كل الملفات", "احمل كل الملفات") {
		return flowDecision{Intent: "download_problem", Step: "multiple_downloads", Confidence: 0.95, Answer: contextualAnswer("download_problem", "multiple_downloads", message, lastIntent)}
	}
	if contains("ساعه استنى", "ساعة استنى", "استنى فيه يحمل", "لسه يحمل", "يعلق التحميل", "تحميل معلق") {
		return flowDecision{Intent: "download_problem", Step: "download_stuck", Confidence: 0.95, Answer: contextualAnswer("download_problem", "download_stuck", message, lastIntent)}
	}

	if hasContentSearchWords(m) && contains("تفعيل", "غير مفعل", "غير مفعّل", "يفعل البريد", "تفعيل البريد") {
		return flowDecision{Intent: "email_verification_problem", Step: "content_blocked_by_verification", Confidence: 0.96, Answer: "فهمت أن طلبك تعليمي، لكن التحميل أو الوصول للملف متوقف بسبب تفعيل البريد.\n\nلإكمال الوصول للملف اتبع الخطوات التالية:\n\n1. افتح صفحة تسجيل الدخول.\n2. سجّل الدخول بنفس البريد وكلمة المرور.\n3. إذا ظهر تنبيه أن البريد غير مفعّل، اضغط إعادة إرسال التفعيل.\n4. افتح بريدك وافحص Inbox وSpam/Junk.\n5. بعد التفعيل ارجع للبحث عن الملف أو الامتحان المطلوب.\n\nإذا لم تصل الرسالة بعد 5 دقائق، تواصل مع الإدارة واذكر البريد ونوع الملف الذي تريد الوصول إليه."}
	}
	if contains("وين بتنزل", "وين بكون", "وين الاقيه", "وين ألاقيه", "فين الاقيه", "اين اجده", "ما بعرف وين", "مش موجود يعني بعمل تحميل", "بعمل تحميل ولكن") {
		return flowDecision{Intent: "download_location", Step: "download_location_steps", Confidence: 0.95, Answer: contextualAnswer("download_location", "download_location_steps", message, lastIntent)}
	}
	if contains("متصفح فيسبوك", "متصفح الفيسبوك", "داخل فيسبوك", "داخل الفيسبوك", "داخل انستغرام", "داخل الانستغرام", "instagram browser", "facebook browser", "in-app browser") && contains("تحميل", "تنزيل", "ملف", "pdf") {
		return flowDecision{Intent: "download_problem", Step: "in_app_browser_download", Confidence: 0.96, Answer: contextualAnswer("download_problem", "in_app_browser_download", message, lastIntent)}
	}
	if contains("زر التحميل", "زر تحميل", "التحميل لا يضغط", "ما بضغط", "لا يستجيب", "العداد لا يعمل", "العداد واقف", "تحميل واقف", "ما بنزل", "لا يحمل") {
		return flowDecision{Intent: "download_problem", Step: "download_button_issue", Confidence: 0.95, Answer: contextualAnswer("download_problem", "download_button_issue", message, lastIntent)}
	}
	if contains("لا يفتح", "لا يقتح", "موراضي", "مور اضي", "مو راضي", "ما بيفتح", "عدم الوصول", "المرفق غير موجود", "لا يظهر الامتحان", "ما بطلعلي", "ما بطلع لي") && contains("رابط", "تحميل", "مرفق", "امتحان", "ملف") {
		return flowDecision{Intent: "file_not_found", Step: "broken_download_link", Confidence: 0.94, Answer: contextualAnswer("file_not_found", "broken_download_link", message, lastIntent)}
	}
	if contains("البريد خطأ", "بريد خطأ", "الايميل غلط", "الايميل خطأ", "كتبت الايميل", "كتبت البريد", "تعديل البريد", "تصحيح البريد", "wrong email") {
		return flowDecision{Intent: "email_verification_problem", Step: "wrong_email", Confidence: 0.96, Answer: contextualAnswer("email_verification_problem", "wrong_email", message, lastIntent)}
	}
	if contains("لا استطيع الوصول الى بريدي", "لا أستطيع الوصول إلى بريدي", "بريدي قديم", "البريد قديم", "فقدت البريد", "نسيت البريد", "صندوق البريد ممتلئ", "البريد ممتلئ", "inbox full") {
		return flowDecision{Intent: "email_verification_problem", Step: "email_access_problem", Confidence: 0.95, Answer: contextualAnswer("email_verification_problem", "email_access_problem", message, lastIntent)}
	}
	if contains("نتائج البحث غير مناسبة", "نتائج غير مناسبة", "نتائج غلط", "لا تظهر نتائج", "ما في نتائج", "ما لقيت", "مش لاقي", "لم أجد الملف", "لم اجد الملف") {
		return flowDecision{Intent: "search_content", Step: "search_refine", Confidence: 0.92, Answer: contextualAnswer("search_content", "search_refine", message, lastIntent)}
	}
	if containsEmail(message) && contains("افحص", "فحص", "هل متواجد", "هل موجود", "موجود", "مسجل", "تحقق", "لديكم", "عندكم") {
		return flowDecision{Intent: "account_lookup_privacy", Step: "privacy_safe_lookup", Confidence: 0.97, Answer: "لحماية خصوصية المستخدمين، لا أستطيع تأكيد هل بريد إلكتروني معيّن موجود في النظام من داخل الدردشة.\n\nإذا كان البريد يخصك اتبع أحد الحلول الآمنة:\n\n1. جرّب تسجيل الدخول من صفحة تسجيل الدخول.\n2. إذا نسيت كلمة المرور، استخدم استعادة كلمة المرور.\n3. إذا وصلتك رسالة استعادة، فهذا يعني أن البريد مرتبط بحساب.\n4. إذا لم تصل أي رسالة، تواصل مع الإدارة مع ذكر البريد ووقت المحاولة.\n\nلا تشارك كلمة المرور أو روابط التفعيل مع أي شخص."}
	}
	if contains("هل البريد", "البريد الالكتروني متواجد", "البريد الإلكتروني متواجد", "الايميل متواجد", "ايميلي موجود", "حسابي موجود", "هل عندكم حساب") {
		return flowDecision{Intent: "account_lookup_privacy", Step: "privacy_safe_lookup", Confidence: 0.94, Answer: "لا أستطيع كشف وجود بريد إلكتروني أو حساب من داخل الدردشة لحماية خصوصية المستخدمين.\n\nللتأكد بشكل آمن:\n\n1. افتح صفحة تسجيل الدخول وجرب الدخول.\n2. إذا لم تتذكر كلمة المرور، استخدم استعادة كلمة المرور.\n3. إذا وصلك بريد الاستعادة، فالبريد مرتبط بحساب.\n4. إذا لم يصلك شيء، أرسل للإدارة البريد ووقت المحاولة من صفحة التواصل."}
	}
	if contains("خطوات انشاء", "خطوات إنشاء", "طريقة انشاء", "طريقة إنشاء", "كيف انشئ", "كيف أسجل", "تسجيل حساب", "حساب جديد") {
		return flowDecision{Intent: "auth_register_problem", Step: "register_steps", Confidence: 0.95, Answer: contextualAnswer("auth_register_problem", "register_steps", message, lastIntent)}
	}
	if contains("صفحه تسجيل الدخول", "صفحة تسجيل الدخول", "رابط تسجيل الدخول", "افتح تسجيل الدخول") || (contains("الدخول") && contains("صفحه", "صفحة", "رابط", "افتح")) {
		return flowDecision{Intent: "auth_login_problem", Step: "open_login_page", Confidence: 0.95, Answer: "يمكنك فتح صفحة تسجيل الدخول مباشرة من الزر بالأسفل. بعد الدخول، إذا ظهرت رسالة أن البريد غير مفعّل، انتقل إلى إعادة إرسال التفعيل أو تواصل مع الإدارة إذا لم تصل الرسالة."}
	}
	if contains("اعاده ارسال", "إعادة إرسال", "اعادة ارسال", "ارسل التفعيل", "كود التفعيل", "رسالة التفعيل") && contains("صفحه", "صفحة", "رابط", "كيف", "وين", "اين", "أين") {
		return flowDecision{Intent: "email_verification_problem", Step: "resend_verification", Confidence: 0.95, Answer: "إعادة إرسال رسالة التفعيل تتم بعد تسجيل الدخول أو من تنبيه الحساب غير المفعّل.\n\n1. افتح صفحة تسجيل الدخول.\n2. أدخل البريد وكلمة المرور.\n3. إذا ظهر تنبيه أن البريد غير مفعّل، اضغط إعادة إرسال التفعيل.\n4. افحص Inbox وSpam/Junk والرسائل الترويجية.\n5. انتظر من 2 إلى 5 دقائق قبل المحاولة مرة أخرى."}
	}
	if contains("صفحه استعادة", "صفحة استعادة", "رابط الاستعادة", "نسيت كلمة المرور", "اعادة تعيين كلمة المرور") && contains("صفحه", "صفحة", "رابط", "نسيت", "استعادة") {
		return flowDecision{Intent: "password_reset_problem", Step: "open_password_reset", Confidence: 0.94, Answer: "افتح صفحة استعادة كلمة المرور من الزر بالأسفل، ثم اكتب البريد المرتبط بحسابك. افحص البريد الوارد والرسائل غير الهامة، ولا تطلب الرابط أكثر من مرة متتالية حتى لا تتأخر الرسائل."}
	}
	if contains("صفحه التواصل", "صفحة التواصل", "تواصل مع الاداره", "تواصل مع الإدارة", "رابط التواصل") {
		return flowDecision{Intent: "contact_support", Step: "open_contact", Confidence: 0.94, Answer: "يمكنك التواصل مع الإدارة من صفحة التواصل. اكتب البريد المستخدم، رابط الصفحة إن وجد، وصف المشكلة أو رسالة الخطأ حتى تتم مراجعتها بسرعة."}
	}
	if contains("الخطوات", "خطوات", "بشكل صحيح", "ماذا افعل", "شو اعمل", "ما الحل") && lastIntent != "" {
		return flowDecision{Intent: lastIntent, Step: "show_steps", Confidence: 0.9}
	}
	if detectedIntent == "general_question" && lastIntent != "" && contains("صحيح", "تمام", "فحصت", "عملت", "جربت", "مكتوب صحيح", "ما زالت", "لم تصل", "لم يصل", "لا تصل") {
		return flowDecision{Intent: lastIntent, Step: "follow_up", Confidence: 0.88}
	}
	return flowDecision{Intent: detectedIntent, Step: defaultStep(detectedIntent), Confidence: 0.0}
}

func defaultStep(intent string) string {
	switch intent {
	case "unsupported_phone_feature":
		return "email_only"
	case "auth_login_problem":
		return "login_diagnosis"
	case "auth_register_problem":
		return "register_steps"
	case "password_reset_problem":
		return "password_reset_steps"
	case "email_verification_problem":
		return "verification_steps"
	case "download_problem":
		return "download_diagnosis"
	case "download_location":
		return "download_location_steps"
	case "permission_problem":
		return "permission_denied"
	case "file_not_found":
		return "broken_download_link"
	case "search_content", "find_grade", "find_subject", "find_semester", "request_content":
		return "collect_search_details"
	case "account_lookup_privacy":
		return "privacy_safe_lookup"
	case "contact_support":
		return "contact_steps"
	case "site_services":
		return "site_services"
	case "about_site":
		return "about_site"
	case "privacy_request":
		return "legal_pages"
	case "site_usage":
		return "site_usage"
	case "open_classes":
		return "open_classes"
	case "open_search":
		return "open_search"
	case "thanks":
		return "thanks"
	case "frustration":
		return "calm_support"
	default:
		return "answer"
	}
}

func countryLandingPath(countryID database.CountryID) string {
	code := strings.TrimSpace(string(database.CountryCode(countryID)))
	if code == "" {
		code = "jo"
	}
	return "/" + code
}

func siteSearchHelpAnswer() string {
	return "للبحث داخل الموقع استخدم صيغة واضحة:\n\nنوع الملف + المادة + الصف + الفصل\n\nأمثلة:\n- اختبار نهائي فيزياء الصف التاسع الفصل الثاني\n- أوراق عمل رياضيات الصف الخامس الفصل الأول\n- تحليل محتوى لغة عربية الصف العاشر الفصل الثاني\n\nيمكنك أيضًا استخدام الفلاتر الموجودة في البحث: الصف، نوع المحتوى، المادة، والفصل الدراسي."
}

func contextualAnswer(intent, step, message, lastIntent string) string {
	switch intent {
	case "unsupported_phone_feature":
		return unsupportedFeatureAnswer()
	case "account_lookup_privacy":
		return "لحماية خصوصية المستخدمين، لا أستطيع تأكيد هل بريد إلكتروني معيّن موجود في النظام من داخل الدردشة.\n\nإذا كان البريد يخصك:\n\n1. جرّب تسجيل الدخول.\n2. إذا نسيت كلمة المرور، استخدم استعادة كلمة المرور.\n3. إذا وصلك بريد استعادة كلمة المرور فهذا يعني أن البريد مرتبط بحساب.\n4. إذا لم يصلك شيء، تواصل مع الإدارة لمراجعة الحالة يدويًا."
	case "auth_register_problem":
		return "خطوات إنشاء حساب جديد:\n\n1. افتح صفحة إنشاء الحساب.\n2. اكتب الاسم والبريد الإلكتروني وكلمة المرور.\n3. تأكد أن البريد مكتوب بشكل صحيح قبل الإرسال.\n4. اضغط إنشاء حساب.\n5. افتح بريدك الإلكتروني واضغط رابط التفعيل.\n6. بعد التفعيل، ارجع إلى الموقع وسجّل الدخول.\n\nإذا كان البريد مستخدمًا سابقًا، استخدم استعادة كلمة المرور بدل إنشاء حساب جديد."
	case "auth_login_problem":
		if step == "show_steps" || step == "follow_up" {
			return "خطوات حل مشكلة تسجيل الدخول:\n\n1. افتح صفحة تسجيل الدخول.\n2. اكتب البريد وكلمة المرور يدويًا بدون مسافات زائدة.\n3. تأكد من لغة لوحة المفاتيح عند كتابة كلمة المرور.\n4. إذا ظهرت رسالة أن البريد غير مفعّل، انتقل إلى إعادة إرسال التفعيل.\n5. إذا كانت كلمة المرور غير صحيحة، استخدم استعادة كلمة المرور.\n6. إذا بقيت المشكلة، تواصل مع الإدارة مع ذكر البريد ووقت المحاولة."
		}
		return "لحل مشكلة تسجيل الدخول: تأكد من كتابة البريد وكلمة المرور بدون مسافات زائدة، ثم جرّب تسجيل الدخول مرة أخرى. إذا ظهرت رسالة أن البريد غير مفعّل، فعّل البريد أولًا. وإذا نسيت كلمة المرور، استخدم خيار استعادة كلمة المرور."
	case "password_reset_problem":
		return "خطوات استعادة كلمة المرور:\n\n1. افتح صفحة استعادة كلمة المرور.\n2. اكتب البريد المرتبط بحسابك.\n3. افحص البريد الوارد وSpam/Junk.\n4. افتح رابط الاستعادة واضبط كلمة مرور جديدة.\n5. إذا لم تصل الرسالة، تأكد من البريد ثم تواصل مع الإدارة."
	case "email_verification_problem":
		if step == "expired_verification_link" {
			return "إذا ظهرت رسالة أن رابط التحقق منتهي الصلاحية، فهذا يعني أن الرابط القديم لم يعد صالحًا.\n\nالحل الصحيح:\n\n1. افتح صفحة تسجيل الدخول.\n2. سجّل الدخول بالبريد وكلمة المرور.\n3. اضغط إعادة إرسال رسالة التفعيل مرة واحدة فقط.\n4. افتح أحدث رسالة وصلت إلى بريدك، وليس الرسائل القديمة.\n5. اضغط رابط التحقق الجديد خلال وقت قصير.\n\nإذا تكررت المشكلة، تواصل مع الإدارة واذكر البريد ووقت آخر محاولة."
		}
		if step == "wrong_email" {
			return "إذا كان البريد مكتوبًا بشكل خاطئ فلن تصلك رسالة التفعيل.\n\nاتبع الحل المناسب:\n\n1. إذا تستطيع الدخول إلى الحساب، افتح الملف الشخصي أو إعدادات الحساب وحاول تعديل البريد.\n2. إذا لا تستطيع تعديل البريد، استخدم صفحة التواصل.\n3. اكتب للإدارة البريد الخاطئ كما سجلته والبريد الصحيح الذي تريد اعتماده.\n4. لا تنشئ حسابات كثيرة بنفس البيانات حتى لا تتداخل المحاولات.\n\nبعد تصحيح البريد اطلب إعادة إرسال رسالة التفعيل مرة واحدة ثم افحص Inbox وSpam/Junk."
		}
		if step == "email_access_problem" {
			return "إذا كان البريد قديمًا أو لا تستطيع فتحه، فلن تتمكن من تأكيد الحساب بنفسك.\n\nالحل الآمن:\n\n1. جرّب أولًا استعادة الوصول إلى البريد من مزود البريد.\n2. إذا كان صندوق البريد ممتلئًا، احذف بعض الرسائل ثم أعد إرسال التفعيل.\n3. إذا فقدت الوصول للبريد نهائيًا، تواصل مع الإدارة.\n4. أرسل البريد القديم، البريد الجديد، واسم الحساب إن وجد.\n5. قد تطلب الإدارة معلومات تحقق قبل تغيير البريد لحماية الحساب."
		}
		if step == "content_blocked_by_verification" {
			return "فهمت أن طلبك تعليمي، لكن الوصول للملف أو الامتحان متوقف بسبب تفعيل البريد.\n\nاتبع هذه الخطوات بالترتيب:\n\n1. افتح صفحة تسجيل الدخول.\n2. سجّل الدخول بنفس البريد وكلمة المرور.\n3. إذا ظهر تنبيه أن البريد غير مفعّل، اضغط إعادة إرسال التفعيل.\n4. افتح بريدك وافحص Inbox وSpam/Junk والرسائل الترويجية.\n5. بعد التفعيل ارجع إلى صفحة الملف أو ابحث عنه من جديد.\n\nإذا لم تصل رسالة التفعيل خلال 5 دقائق، تواصل مع الإدارة واذكر البريد واسم الملف المطلوب."
		}
		if step == "follow_up" {
			return "تمام، بما أن البريد مكتوب بشكل صحيح، الخطوة التالية هي إعادة إرسال رسالة التفعيل:\n\n1. افتح صفحة تسجيل الدخول.\n2. أدخل البريد وكلمة المرور.\n3. إذا ظهر تنبيه أن البريد غير مفعّل، اضغط إعادة إرسال التفعيل.\n4. افحص Inbox وSpam/Junk والرسائل الترويجية.\n5. انتظر من 2 إلى 5 دقائق.\n6. إذا لم تصل الرسالة، تواصل مع الإدارة واذكر البريد ووقت آخر محاولة."
		}
		return "خطوات حل مشكلة عدم وصول رسالة التفعيل:\n\n1. تأكد أن البريد مكتوب بشكل صحيح.\n2. افحص البريد غير الهام Spam/Junk والرسائل الترويجية.\n3. افتح صفحة تسجيل الدخول وسجّل الدخول.\n4. إذا ظهر خيار إعادة إرسال التفعيل، اضغط عليه مرة واحدة.\n5. انتظر من 2 إلى 5 دقائق.\n6. إذا لم تصل الرسالة، تواصل مع الإدارة مع ذكر البريد ووقت المحاولة."
	case "download_problem":
		if step == "multiple_downloads" {
			return "إذا حمّلت ملفًا واحدًا وتريد تحميل ملف ثانٍ أو ثالث، اتبع التالي:\n\n1. انتظر حتى يكتمل تحميل الملف الأول بالكامل.\n2. افتح صفحة الملف الثاني من داخل الموقع، ولا تستخدم رابط تحميل قديم.\n3. اضغط زر التحميل مرة واحدة فقط.\n4. إذا لم يبدأ التحميل، حدّث الصفحة وجرب من Chrome أو Safari.\n5. لا تضغط أزرار التحميل بسرعة متتالية حتى لا يعتبر النظام الطلبات مكررة."
		}
		if step == "download_stuck" {
			return "إذا بقي الملف فترة طويلة دون أن ينزل، فغالبًا التحميل عالق أو المتصفح منع التنزيل.\n\nجرّب بالترتيب:\n\n1. أوقف التحميل العالق من المتصفح.\n2. حدّث صفحة الملف الأصلية.\n3. اضغط تحميل مرة واحدة وانتظر.\n4. جرّب Chrome أو Safari بدل متصفح فيسبوك/إنستغرام.\n5. إذا بقيت المشكلة، أرسل رابط صفحة الملف للإدارة."
		}
		if step == "in_app_browser_download" {
			return "المشكلة غالبًا من متصفح التطبيق داخل Facebook أو Instagram؛ هذه المتصفحات قد تمنع تحميل الملفات.\n\nالحل:\n\n1. انسخ رابط الصفحة.\n2. افتحه في Chrome أو Safari خارج التطبيق.\n3. سجّل الدخول مرة أخرى إذا طُلب منك ذلك.\n4. اضغط تحميل من صفحة الملف الأصلية.\n5. بعد التحميل افتح Downloads / التنزيلات أو تطبيق الملفات.\n\nإذا بقي الخطأ، أرسل رابط صفحة الملف ونوع جهازك للإدارة."
		}
		if step == "download_button_issue" {
			return "إذا كان زر التحميل لا يستجيب أو العدّاد لا يعمل:\n\n1. حدّث الصفحة مرة واحدة.\n2. تأكد أنك مسجل الدخول وأن البريد مفعّل.\n3. أوقف مانع الإعلانات مؤقتًا إن كان يمنع أزرار الصفحة.\n4. جرّب متصفحًا آخر مثل Chrome أو Safari.\n5. لا تضغط الزر عدة مرات متتالية؛ انتظر انتهاء أي عدّاد ظاهر.\n6. إذا بقيت المشكلة، أرسل رابط الصفحة وصورة للخطأ للإدارة."
		}
		return "لفحص مشكلة تحميل الملفات، جرّب هذه الخطوات بالترتيب:\n\n1. سجّل الدخول إلى حسابك من نفس المتصفح.\n2. تأكد أن البريد الإلكتروني مفعّل؛ بعض الملفات لا تُحمّل قبل التفعيل.\n3. افتح صفحة الملف الأصلية داخل الموقع، ولا تستخدم رابط تحميل منسوخًا أو قديمًا.\n4. اضغط زر التحميل مرة واحدة وانتظر انتهاء العدّاد إن ظهر.\n5. على الهاتف، افتح مجلد Downloads / التنزيلات أو تطبيق الملفات بعد التحميل.\n6. إذا ظهرت رسالة خطأ أو صلاحية، أرسل للإدارة رابط صفحة الملف ونص الرسالة."
	case "download_location":
		return "إذا ضغطت تحميل ولم تعرف أين ذهب الملف، فغالبًا تم حفظه في مجلد التنزيلات بجهازك.\n\nجرّب هذه الخطوات:\n\n1. افتح تطبيق الملفات أو مدير الملفات في الهاتف.\n2. ادخل إلى مجلد Downloads / التنزيلات.\n3. في الكمبيوتر افتح Downloads من File Explorer أو Finder.\n4. ابحث باسم الملف أو حسب آخر ملف تم تحميله.\n5. إذا لم يظهر الملف، أعد التحميل من صفحة الملف الأصلية وتأكد أن المتصفح لم يمنع التنزيل.\n\nعلى الهاتف قد يظهر الملف داخل إشعارات المتصفح أو داخل تطبيق Downloads."
	case "social_login_problem":
		return "إذا تعذر الدخول عبر Google أو Facebook:\n\n1. جرّب فتح صفحة تسجيل الدخول من متصفح حديث مثل Chrome أو Safari.\n2. اسمح للنوافذ المنبثقة وملفات الارتباط، لأن مزود الدخول يحتاج نافذة تحقق خارجية.\n3. إذا كان الحساب مرتبطًا ببريدك، جرّب الدخول بالبريد وكلمة المرور.\n4. إذا أنشأت الحساب عبر Google أو Facebook ولا تعرف كلمة المرور، استخدم استعادة كلمة المرور للبريد نفسه.\n5. إذا بقيت المشكلة، تواصل مع الإدارة واذكر البريد المستخدم وطريقة الدخول التي جربتها."
	case "permission_problem":
		if step == "permission_denied" {
			return "إذا ظهرت رسالة: لا تملك صلاحية، فهذا غالبًا بسبب واحد من هذه الأسباب:\n\n1. لم يتم تسجيل الدخول من نفس المتصفح.\n2. البريد الإلكتروني غير مفعّل بعد.\n3. الملف يحتاج صلاحية أو تم تغيير تصنيفه.\n4. الرابط قديم أو منسوخ من مكان آخر.\n\nالحل: سجّل الدخول، فعّل البريد، ثم افتح صفحة الملف الأصلية واضغط تحميل. إذا بقيت الرسالة، أرسل رابط الملف للإدارة."
		}
		return "إذا ظهرت رسالة عدم وجود صلاحية، تأكد من تسجيل الدخول وتفعيل البريد. إذا بقيت الرسالة، أرسل رابط الملف للإدارة حتى تتم مراجعة الصلاحية أو التصنيف."
	case "file_not_found":
		return "إذا ظهر لك عند الضغط على رابط التحميل: عدم الوصول، المرفق غير موجود، أو أن الرابط لا يفتح، فغالبًا الرابط قديم أو انتهت صلاحيته أو تم نقل الملف.\n\nالحل الصحيح:\n\n1. لا تستخدم رابطًا منسوخًا أو قديمًا.\n2. افتح صفحة الملف الأصلية من داخل الموقع.\n3. اضغط تحميل من جديد.\n4. إذا بقي الخطأ، استخدم البحث باسم الامتحان أو الصف والمادة.\n5. إذا لم يظهر الملف، أرسل رابط الصفحة للإدارة حتى تتم مراجعة المرفق."
	case "search_content", "find_grade", "find_subject", "find_semester":
		if step == "search_refine" {
			return "إذا ظهرت نتائج غير مناسبة أو لم تظهر نتائج، استخدم صيغة بحث أدق:\n\n1. اكتب نوع الملف: امتحان، ملخص، ورقة عمل، خطة.\n2. اكتب المادة بوضوح.\n3. اكتب الصف.\n4. اكتب الفصل الأول أو الفصل الثاني.\n\nمثال قوي: امتحان اللغة العربية الصف الأول الفصل الثاني.\n\nإذا لم يظهر الملف بعد ذلك، أرسل طلب إضافة ملف للإدارة مع نفس التفاصيل."
		}
		return "للبحث بشكل صحيح، اكتب الصف والمادة والفصل ونوع الملف. مثال: رياضيات الصف التاسع الفصل الأول اختبار، أو ملخص علوم الصف السابع. إذا عرفت الدولة أو المنهج، اذكره أيضًا."

	case "thanks":
		return "العفو، يسعدنا خدمتك. إذا احتجت ملفًا أو واجهت مشكلة في التفعيل أو التحميل، اكتب طلبك مباشرة."
	case "frustration":
		return "أفهم أن المشكلة مزعجة. اكتب لي باختصار ما الذي يحدث معك: هل المشكلة في التحميل، التفعيل، تسجيل الدخول، أم البحث عن ملف؟"
	case "open_classes":
		return "لعرض الصفوف التعليمية، افتح صفحة الأردن ثم اختر الصف المطلوب. بعد ذلك اختر المادة المناسبة للوصول إلى الفصول والملفات التعليمية المرتبطة بهذا الصف.\n\nإذا كنت تبحث عن ملف محدد، استخدم البحث بصيغة: نوع الملف + المادة + الصف + الفصل."
	case "open_search":
		return siteSearchHelpAnswer()
	case "site_services":
		return "خدمات الموقع تشمل: محتوى تعليمي للصفوف، بنك امتحانات وملفات، أخبار ومقالات تربوية، بحث وتصفية متقدمة، وخدمات للأعضاء والمعلمين مثل طلب ملفات أو إرسال اقتراحات عبر صفحة التواصل."
	case "about_site":
		return "موقع الأيمان منصة تعليمية تهدف إلى توفير موارد تعليمية موثوقة ومنظمة للطلاب والمعلمين، مع التركيز على المنهاج الأردني وتسهيل الوصول إلى الصفوف والمواد والاختبارات والملفات التعليمية."
	case "site_usage":
		return "طريقة استخدام الموقع باختصار:\n\n1. من الصفحة الرئيسية يمكنك البحث باستخدام: الصف، نوع المحتوى، المادة، والفصل.\n2. من صفحة الأردن اختر الصف أولًا، ثم المادة، ثم الفصل أو الملف المطلوب.\n3. للبحث السريع اكتب: نوع الملف + المادة + الصف + الفصل.\n4. إذا لم تجد الملف، أرسل عنوانه الكامل أو اطلب إضافته من صفحة التواصل.\n\nمثال: اختبار نهائي فيزياء الصف التاسع الفصل الثاني."
	case "privacy_request":
		return "يمكنك مراجعة صفحات سياسة الخصوصية، شروط الاستخدام، سياسة الكوكيز، إخلاء المسؤولية، حقوق الملكية، وسياسة التحرير من روابط أسفل الموقع."
	case "contact_support":
		return "للتواصل مع الإدارة استخدم صفحة اتصل بنا. اكتب: الاسم، البريد الإلكتروني، الموضوع، وصف المشكلة، ورابط الصفحة إن وجد.\n\nإذا ظهر أن نموذج التواصل غير مهيأ أو لم تتمكن من الإرسال، استخدم البريد الموجود في صفحة من نحن: info@alemancenter.com."
	default:
		return ruleAnswer(intent)
	}
}

func requiresContextualAnswer(message, intent, lastIntent string) bool {
	m := normalizeArabic(strings.ToLower(message))

	// هذه الحالات تحتاج ردًا سياقيًا ثابتًا، ولا يجب أخذ أول جواب من قاعدة المعرفة
	// لأن قاعدة المعرفة قد تكون عامة وتسبب ردودًا خاطئة مثل ربط الصلاحية بتفعيل البريد.
	switch intent {
	case "unsupported_phone_feature",
		"account_lookup_privacy",
		"auth_register_problem",
		"email_verification_problem",
		"download_location",
		"permission_problem",
		"file_not_found",
		"contact_support",
		"request_content",
		"open_classes",
		"open_search",
		"site_usage",
		"site_services",
		"about_site",
		"privacy_request",
		"thanks",
		"frustration":
		return true
	}

	if containsAny(m,
		"كتبت البريد خطأ",
		"البريد خطأ",
		"الايميل غلط",
		"رابط التحقق منتهي",
		"رابط التفعيل منتهي",
		"لا تملك صلاحية",
		"عدم وجود صلاحية",
		"عملت تحميل ولا أجد",
		"وين الملف",
		"وين بتنزل",
		"اتصل بالدعم",
		"الدعم الفني",
		"طلب إضافة ملف",
		"إضافة درس",
	) {
		return true
	}

	if isContentIntent(lastIntent) && containsAny(m,
		"الفصل الأول",
		"الفصل الاول",
		"الفصل الثاني",
		"فصل ثاني",
		"نهائي",
		"امتحان نهائي",
		"اختبار نهائي",
		"علوم",
		"رياضيات",
		"لغة عربية",
		"انجليزي",
		"تربية اجتماعية",
	) {
		return true
	}

	return false
}

func buildActions(intent, step string, links []repo.ContentResult, entities searchEntities) []ChatbotAction {
	actions := []ChatbotAction{}
	addLink := func(label, url, style string) {
		actions = append(actions, ChatbotAction{Label: label, Type: "link", URL: url, Style: style})
	}
	addMsg := func(label, msg string) {
		actions = append(actions, ChatbotAction{Label: label, Type: "message", Message: msg})
	}
	searchURL := "/search"
	if q := buildSearchQuery("", entities); q != "" {
		searchURL = "/search?q=" + url.QueryEscape(q)
	}

	switch intent {
	case "site_services":
		addLink("الخدمات", "/services", "primary")
		addLink("اتصل بنا", "/contact-us", "secondary")
	case "about_site":
		addLink("من نحن", "/about-us", "primary")
		addLink("اتصل بنا", "/contact-us", "secondary")
	case "privacy_request":
		addLink("سياسة الخصوصية", "/privacy-policy", "primary")
		addLink("شروط الاستخدام", "/terms", "secondary")
	case "site_usage":
		addLink("فتح الصفوف", "/jo", "primary")
		addLink("فتح البحث", "/search", "secondary")
		addMsg("مشكلة في التحميل", "لا أستطيع تحميل الملفات")
	case "auth_login_problem":
		addLink("فتح صفحة تسجيل الدخول", "/login", "primary")
		addLink("استعادة كلمة المرور", "/forgot-password", "secondary")
		addMsg("لا تصلني رسالة التفعيل", "لا تصلني رسالة التفعيل")
	case "unsupported_phone_feature":
		addLink("فتح صفحة تسجيل الدخول", "/login", "primary")
		addLink("إنشاء حساب ببريد إلكتروني", "/register", "secondary")
		addLink("فتح صفحة التواصل", "/contact-us", "secondary")
	case "auth_register_problem":
		addLink("إنشاء حساب جديد", "/register", "primary")
		addLink("فتح صفحة تسجيل الدخول", "/login", "secondary")
		addLink("استعادة كلمة المرور", "/forgot-password", "secondary")
	case "password_reset_problem":
		addLink("فتح استعادة كلمة المرور", "/forgot-password", "primary")
		addLink("فتح صفحة التواصل", "/contact-us", "secondary")
	case "social_login_problem":
		addLink("فتح صفحة تسجيل الدخول", "/login", "primary")
		addLink("استعادة كلمة المرور", "/forgot-password", "secondary")
		addLink("فتح صفحة التواصل", "/contact-us", "secondary")
	case "email_verification_problem":
		addLink("فتح صفحة تسجيل الدخول", "/login", "primary")
		addLink("فتح صفحة التواصل", "/contact-us", "secondary")
		addMsg("البريد مكتوب صحيح", "البريد مكتوب صحيح")
	case "download_problem", "download_location", "permission_problem", "file_not_found":
		if step == "in_app_browser_download" {
			addMsg("فتحت الصفحة من Chrome", "فتحت الصفحة من Chrome وما زال التحميل لا يعمل")
		}
		if step == "download_button_issue" {
			addMsg("العداد لا يعمل", "العداد لا يعمل وزر التحميل لا يستجيب")
		}
		if intent == "download_location" {
			addMsg("تم التحميل لكن لا أجده", "عملت تحميل للملف ولكن لا أعرف أين أجد الملف")
		}
		addLink("فتح صفحة تسجيل الدخول", "/login", "secondary")
		addLink("فتح صفحة البحث", searchURL, "primary")
		addLink("فتح صفحة التواصل", "/contact-us", "secondary")
	case "search_content", "find_grade", "find_subject", "find_semester":
		addLink("فتح البحث بهذه الكلمات", searchURL, "primary")
		addLink("فتح الصفوف", "/jo", "secondary")
		if entities.Semester == "" && (entities.Grade != "" || entities.Subject != "") {
			addMsg("الفصل الأول", buildSearchQuery("الفصل الأول", entities))
			addMsg("الفصل الثاني", buildSearchQuery("الفصل الثاني", entities))
		}
	case "contact_support", "report_content", "request_content", "site_error", "account_lookup_privacy":
		if intent == "account_lookup_privacy" {
			addLink("تسجيل الدخول", "/login", "primary")
			addLink("استعادة كلمة المرور", "/forgot-password", "secondary")
		}
		addLink("فتح صفحة التواصل", "/contact-us", "primary")
	}
	return actions
}

func detectIntent(message string) (string, float64) {
	m := normalizeArabic(strings.ToLower(message))
	contains := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(m, normalizeArabic(strings.ToLower(w))) {
				return true
			}
		}
		return false
	}

	if isUnsupportedPhoneOrUsernameQuestion(message) {
		return "unsupported_phone_feature", 0.98
	}

	switch {
	case contains("ما هي خدمات الموقع", "خدمات الموقع", "شو بتقدموا", "ماذا تقدم المنصة", "الخدمات التعليمية", "بنك الامتحانات", "اشتراك", "اشتراكات"):
		return "site_services", 0.95
	case contains("من نحن", "عن الموقع", "عن المنصة", "ما هو موقع الايمان", "ما هو موقع الأيمان", "موقع الايمان"):
		return "about_site", 0.94
	case contains("سياسة الخصوصية", "شروط الاستخدام", "سياسة الكوكيز", "حقوق الملكية", "إخلاء المسؤولية", "اخلاء المسؤولية", "سياسة التحرير"):
		return "privacy_request", 0.95
	case contains("نموذج التواصل غير مهيأ", "recaptcha", "ريكابتشا", "لا استطيع ارسال رسالة", "لا أستطيع إرسال رسالة", "نموذج التواصل لا يعمل", "اتصل بنا لا يعمل"):
		return "contact_support", 0.96
	case isDownloadLocationQuestion(message):
		return "download_location", 0.97
	case isCannotFindContentQuestion(message):
		return "search_content", 0.94
	case isGenericDownloadProblemQuestion(message):
		return "download_problem", 0.95

	case contains("كيف استخدم الموقع", "كيف أستخدم الموقع", "طريقة استخدام الموقع", "استخدام الموقع", "كيف اتصفح", "كيف أتصفح", "كيف ابحث", "كيف أبحث"):
		return "site_usage", 0.96
	case contains("عرض الصفوف التعليمية", "اعرض الصفوف", "عرض الصفوف", "الصفوف التعليمية", "تصفح الصفوف", "أريد تصفح الصفوف", "اريد تصفح الصفوف"):
		return "open_classes", 0.97

	case isNoiseOrEmojiOnly(message):
		return "general_question", 0.97
	case isThanksMessage(message):
		return "thanks", 0.98
	case isProfanityOrFrustration(message):
		return "frustration", 0.96
	case contains("اتصل بالدعم", "اتصل بدعم", "اتصل بفريق الدعم", "دعم الموقع", "الدعم الفني", "تواصل مع الدعم", "تواصل مع الإدارة", "اتصل بنا"):
		return "contact_support", 0.96
	case contains("فتح الصفوف", "افتح الصفوف", "صفحة الصفوف", "افتح صفحة الصفوف"):
		return "open_classes", 0.96
	case contains("فتح البحث", "افتح البحث", "فتح البحث بهذه الكلمات", "ابحث بهذه الكلمات"):
		return "open_search", 0.94
	case contains("طلب إضافة ملف", "اضافة ملف", "إضافة ملف", "اضافة درس", "إضافة درس", "طلب ملف", "ملف غير متوفر"):
		return "request_content", 0.95
	case contains("لا تملك صلاحية", "ما عندي صلاحية", "عدم وجود صلاحية", "غير مصرح", "غير مسموح"):
		return "permission_problem", 0.96
	case contains("عملت تحميل ولا اجد", "عملت تحميل ولا أجد", "لا اجد الملف", "لا أجد الملف", "تم التحميل ولا", "حملت ولا لقيت", "حملت وما لقيت", "وين الملف بعد التحميل"):
		return "download_location", 0.95
	case containsEmail(message) && contains("افحص", "فحص", "هل متواجد", "هل موجود", "موجود", "مسجل", "تحقق", "لديكم", "عندكم"):
		return "account_lookup_privacy", 0.97
	case contains("هل البريد", "البريد الالكتروني متواجد", "البريد الإلكتروني متواجد", "الايميل متواجد", "حسابي موجود", "هل عندكم حساب"):
		return "account_lookup_privacy", 0.94
	case contains("خطوات انشاء", "خطوات إنشاء", "طريقة انشاء", "طريقة إنشاء", "كيف انشئ", "كيف أسجل", "تسجيل حساب", "حساب جديد", "register", "signup"):
		return "auth_register_problem", 0.93
	case contains("وين بتنزل", "وين بكون", "وين الاقيه", "وين ألاقيه", "فين الاقيه", "اين اجده", "ما بعرف وين", "مش موجود يعني بعمل تحميل", "بعمل تحميل ولكن", "وين راح الملف", "اختفى الملف", "ما لقيت التنزيل"):
		return "download_location", 0.94
	case contains("متصفح فيسبوك", "متصفح الفيسبوك", "داخل فيسبوك", "داخل الفيسبوك", "داخل انستغرام", "داخل الانستغرام", "facebook browser", "instagram browser") && contains("تحميل", "تنزيل", "ملف", "pdf"):
		return "download_problem", 0.94
	case contains("زر التحميل", "زر تحميل", "التحميل لا يضغط", "ما بضغط", "لا يستجيب", "العداد لا يعمل", "العداد واقف", "تحميل واقف", "ما بنزل", "لا يحمل", "مش قادر احمل", "مش قادر أحمل"):
		return "download_problem", 0.94
	case contains("لا يفتح", "لا يقتح", "موراضي", "مور اضي", "مو راضي", "ما بيفتح", "عدم الوصول", "المرفق غير موجود", "ما بطلعلي", "ما بطلع لي") && contains("رابط", "تحميل", "مرفق", "ملف", "امتحان"):
		return "file_not_found", 0.93
	case contains("البريد خطأ", "بريد خطأ", "الايميل غلط", "الايميل خطأ", "كتبت الايميل", "كتبت البريد", "تعديل البريد", "تصحيح البريد", "wrong email"):
		return "email_verification_problem", 0.94
	case contains("لا استطيع الوصول الى بريدي", "لا أستطيع الوصول إلى بريدي", "بريدي قديم", "البريد قديم", "فقدت البريد", "نسيت البريد", "صندوق البريد ممتلئ", "البريد ممتلئ", "inbox full"):
		return "email_verification_problem", 0.93
	case hasContentSearchWords(m) && contains("تفعيل", "غير مفعل", "غير مفعّل", "يفعل البريد"):
		return "email_verification_problem", 0.94
	case contains("لا يصلني", "لم يصلني", "ما وصل", "لم تصل", "لا تصل", "كود", "رمز", "رسالة", "تفعيل", "غير مفعل", "البريد غير مفعل", "spam", "junk", "email verification", "verify", "verification"):
		return "email_verification_problem", 0.92
	case contains("نسيت", "استعادة", "اعادة تعيين", "إعادة تعيين", "reset password", "forgot password") && contains("كلمة", "مرور", "password"):
		return "password_reset_problem", 0.92
	case contains("بريد مستخدم", "البريد مستخدم", "email already", "حساب موجود", "انشاء حساب", "إنشاء حساب", "تسجيل جديد"):
		return "auth_register_problem", 0.88
	case contains("جوجل", "google", "gmail") && contains("دخول", "تسجيل", "login"):
		return "social_login_problem", 0.86
	case contains("فيسبوك", "facebook", "fb") && contains("دخول", "تسجيل", "login"):
		return "social_login_problem", 0.86
	case contains("لا تملك صلاحية", "غير مصرح", "forbidden", "403", "صلاحية"):
		return "permission_problem", 0.9
	case contains("غير موجود", "404", "تم حذف", "رابط خاطئ", "file not found"):
		return "file_not_found", 0.88
	case contains("نتائج البحث غير مناسبة", "نتائج غير مناسبة", "نتائج غلط", "لا تظهر نتائج", "ما في نتائج", "ما لقيت", "مش لاقي", "لم أجد الملف", "لم اجد الملف"):
		return "search_content", 0.88
	case contains("دولة", "منهج", "الأردن", "الاردن", "السعودية", "مصر", "فلسطين", "country", "curriculum"):
		return "country_or_curriculum", 0.82
	case contains("تحميل", "download", "تنزيل", "ملف", "رابط", "pdf", "تنزيلات", "download failed", "download error"):
		return "download_problem", 0.88
	case contains("دخول", "login", "تسجيل الدخول", "كلمة المرور", "password"):
		return "auth_login_problem", 0.9
	case hasContentSearchWords(m):
		return "search_content", 0.87
	case contains("صف", "الصف", "grade"):
		return "find_grade", 0.82
	case contains("مادة", "subject"):
		return "find_subject", 0.82
	case contains("فصل", "الفصل", "semester", "ترم"):
		return "find_semester", 0.82
	case contains("الصفحة لا تفتح", "بطيء", "لا يعمل", "خطأ", "500", "502", "مشكلة في الموقع", "الجوال", "الهاتف"):
		return "site_error", 0.78
	case contains("إعلان", "اعلان", "ads", "نافذة", "يغطي"):
		return "site_error", 0.75
	case contains("تعديل حساب", "بيانات", "الملف الشخصي", "profile", "تغيير الاسم", "تغيير البريد"):
		return "profile_problem", 0.78
	case contains("حذف حساب", "حذف بيانات", "خصوصية", "privacy", "delete account"):
		return "privacy_request", 0.78
	case contains("إبلاغ", "ابلاغ", "بلاغ", "ملف خاطئ", "تصنيف خاطئ", "report"):
		return "report_content", 0.8
	case contains("طلب ملف", "إضافة ملف", "اضافة ملف", "محتوى ناقص", "أريد درس", "اريد درس"):
		return "request_content", 0.8
	case contains("تواصل", "إدارة", "ادارة", "مشكلة", "support", "contact", "ما زالت المشكلة"):
		return "contact_support", 0.75
	default:
		return "general_question", 0.55
	}
}

func normalizeArabic(v string) string {
	replacer := strings.NewReplacer("أ", "ا", "إ", "ا", "آ", "ا", "ى", "ي", "ة", "ه", "ؤ", "و", "ئ", "ي", "ـ", "")
	return replacer.Replace(v)
}

func containsEmail(v string) bool {
	re := regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	return re.MatchString(v)
}

func hasContentSearchWords(m string) bool {
	words := []string{"بحث", "ابحث", "اريد", "أريد", "ابعث", "ابعثولي", "تبعتولي", "بدي", "عايز", "ارسلوا", "نموذج", "نماذج", "امتحان", "امتحانات", "اختبار", "اختبارات", "نهائي", "ملخص", "ملخصات", "ورقه عمل", "ورقة عمل", "اوراق عمل", "أوراق عمل", "درس", "شرح", "خطة", "خطه", "تحضير", "نمو مهني", "جدول مواصفات", "حلول", "اجابات", "رياضيات", "علوم", "عربي", "اللغه العربيه", "لغة عربية", "انجليزي", "دين", "اسلاميه", "حاسوب", "مهارات رقمية", "ثقافه ماليه", "تربيه فنيه", "تاسع", "ثامن", "عاشر", "اول", "الأول", "الاول", "الصف", "صف", "اجتماعيات", "اجتماعي", "وطني", "نهاية", "فيزياء", "كيمياء", "احياء", "جغرافيا", "تاريخ", "موسيقى", "فلسفه"}
	for _, w := range words {
		if strings.Contains(m, normalizeArabic(w)) {
			return true
		}
	}
	return false
}

func shouldSearchContent(intent, message string) bool {
	return isContentIntent(intent) || hasContentSearchWords(normalizeArabic(strings.ToLower(message)))
}

func relaxedSearchQueries(message string, e searchEntities) []string {
	queries := []string{}

	add := func(q string) {
		q = strings.TrimSpace(q)
		if q == "" {
			return
		}
		for _, existing := range queries {
			if normalizeArabic(existing) == normalizeArabic(q) {
				return
			}
		}
		queries = append(queries, q)
	}

	// Full structured search first.
	add(buildSearchQuery(message, e))

	// Raw message as typed by the visitor.
	add(message)

	// Core entity combinations. This helps when the stored title has "اختبار" instead of "امتحان"
	// or "نموذج 2" instead of "نموذج2".
	if e.Subject != "" && e.Grade != "" && e.Semester != "" {
		add(strings.Join([]string{e.Subject, e.Grade, e.Semester}, " "))
		add(strings.Join([]string{"اختبار", e.Subject, e.Grade, e.Semester}, " "))
		add(strings.Join([]string{"امتحان", e.Subject, e.Grade, e.Semester}, " "))
		add(strings.Join([]string{"نهائي", e.Subject, e.Grade, e.Semester}, " "))
		add(strings.Join([]string{"اختبار نهائي", e.Subject, e.Grade, e.Semester}, " "))
	}

	if e.Subject != "" && e.Grade != "" {
		add(strings.Join([]string{e.Subject, e.Grade}, " "))
		add(strings.Join([]string{"اختبار", e.Subject, e.Grade}, " "))
		add(strings.Join([]string{"امتحان", e.Subject, e.Grade}, " "))
	}

	// Handle "نموذج2" vs "نموذج 2".
	m := normalizeArabic(message)
	if strings.Contains(m, "نموذج2") || strings.Contains(m, "نموذج 2") {
		add(strings.ReplaceAll(message, "نموذج2", "نموذج 2"))
		add(strings.ReplaceAll(message, "نموذج 2", "نموذج2"))
	}
	if strings.Contains(m, "نهائي") && e.Subject != "" && e.Grade != "" && e.Semester != "" {
		add(strings.Join([]string{"اختبار نهائي", e.Subject, e.Grade, e.Semester, "نموذج 2"}, " "))
		add(strings.Join([]string{"امتحان نهائي", e.Subject, e.Grade, e.Semester, "نموذج2"}, " "))
	}

	return queries
}

func shouldRunContentSearch(intent, message string, entities searchEntities) bool {
	if intent == "site_usage" || intent == "site_services" || intent == "about_site" || intent == "privacy_request" || intent == "open_classes" || intent == "open_search" {
		return false
	}
	if isContentIntent(intent) {
		return true
	}
	if intent == "request_content" {
		return true
	}
	if intent == "general_question" && hasContentSearchWords(normalizeArabic(strings.ToLower(message))) {
		return true
	}
	if intent != "general_question" {
		return false
	}
	return entities.Grade != "" || entities.Subject != "" || entities.Semester != "" || entities.ContentType != ""
}
func isContentIntent(intent string) bool {
	return intent == "search_content" || intent == "find_grade" || intent == "find_subject" || intent == "find_semester" || intent == "request_content"
}

func shouldKeepContentContext(intent, lastIntent string, current, previous searchEntities) bool {
	if isContentIntent(intent) {
		return true
	}
	if !isContentIntent(lastIntent) {
		return false
	}
	return current.ContentType != "" || current.Semester != "" || current.Subject != "" || current.Grade != ""
}

func extractSearchEntities(message string) searchEntities {
	m := normalizeArabic(strings.ToLower(message))
	entities := searchEntities{RawQuery: message}
	isNonSchoolHistoricalPhrase := strings.Contains(m, "التاسع عشر") || strings.Contains(m, "القرن التاسع") || strings.Contains(m, "الميلادي")

	grades := map[string]string{
		"الصف الاول": "الصف الأول", "اول ابتدائي": "الصف الأول", "الصف الثاني": "الصف الثاني", "الصف الثالث": "الصف الثالث", "الصف الرابع": "الصف الرابع", "الصف الخامس": "الصف الخامس", "الصف السادس": "الصف السادس", "الصف السابع": "الصف السابع", "صف سابع": "الصف السابع", "الصف الثامن": "الصف الثامن", "صف ثامن": "الصف الثامن", "ثامن": "الصف الثامن", "الصف التاسع": "الصف التاسع", "صف تاسع": "الصف التاسع", "تاسع": "الصف التاسع", "التاسع": "الصف التاسع", "الصف العاشر": "الصف العاشر", "عاشر": "الصف العاشر", "الصف الحادي عشر": "الصف الحادي عشر", "الحادي عشر": "الصف الحادي عشر", "الصف الثاني عشر": "الصف الثاني عشر", "الثاني عشر": "الصف الثاني عشر", "توجيهي": "الصف الثاني عشر",
		// صيغ مختصرة بدون "الصف" — لا نضيف الأول/الثاني لتعارضهما مع الفصل
		"ثالث": "الصف الثالث", "الثالث": "الصف الثالث",
		"رابع": "الصف الرابع", "الرابع": "الصف الرابع",
		"خامس": "الصف الخامس", "الخامس": "الصف الخامس",
		"سادس": "الصف السادس", "السادس": "الصف السادس",
		"سابع": "الصف السابع", "السابع": "الصف السابع",
	}
	if !isNonSchoolHistoricalPhrase {
		for k, v := range grades {
			if strings.Contains(m, normalizeArabic(k)) {
				entities.Grade = v
				break
			}
		}
	}

	subjects := map[string]string{
		"رياضيات": "رياضيات", "الرياضيات": "رياضيات", "عربي": "اللغة العربية", "لغه عربيه": "اللغة العربية", "اللغه العربيه": "اللغة العربية", "لغة عربية": "اللغة العربية", "اللغة العربية": "اللغة العربية", "انجليزي": "اللغة الإنجليزية", "انكليزي": "اللغة الإنجليزية", "انجليزيه": "اللغة الإنجليزية", "اللغة الإنجليزية": "اللغة الإنجليزية", "علوم": "علوم", "العلوم": "علوم", "فيزياء": "فيزياء", "الفيزياء": "فيزياء", "فزيا": "فيزياء", "فيزيا": "فيزياء", "كيمياء": "كيمياء", "احياء": "أحياء", "تاريخ": "تاريخ", "جغرافيا": "جغرافيا", "دين": "التربية الإسلامية", "تربيه اسلاميه": "التربية الإسلامية", "تربية اسلامية": "التربية الإسلامية", "اسلاميه": "التربية الإسلامية", "حاسوب": "حاسوب", "كمبيوتر": "حاسوب", "ثقافه ماليه": "الثقافة المالية", "ثقافة مالية": "الثقافة المالية", "تربيه فنيه": "التربية الفنية", "تربية فنية": "التربية الفنية", "فنية": "التربية الفنية",
		// الدراسات الاجتماعية — المادة الأكثر بحثاً وكانت غائبة عن الخريطة
		"اجتماعيات": "الاجتماعيات", "الاجتماعيات": "الاجتماعيات", "اجتماعي": "الاجتماعيات", "الاجتماعي": "الاجتماعيات", "اجتماع": "الاجتماعيات", "تربيه اجتماعيه": "الاجتماعيات", "تربية اجتماعية": "الاجتماعيات",
		// التربية الوطنية والمدنية
		"وطنيه": "التربية الوطنية", "وطني": "التربية الوطنية", "التربية الوطنية": "التربية الوطنية", "تربيه وطنيه": "التربية الوطنية", "مدنيه": "التربية المدنية", "مدني": "التربية المدنية",
		// التربية الرياضية
		"رياضيه": "التربية الرياضية", "التربية الرياضية": "التربية الرياضية", "تربيه رياضيه": "التربية الرياضية", "ثقافه بدنيه": "التربية الرياضية",
		// التربية الموسيقية
		"موسيقى": "التربية الموسيقية", "الموسيقى": "التربية الموسيقية",
		// الفلسفة والإحصاء والاقتصاد المنزلي
		"فلسفه": "الفلسفة", "فلسفة": "الفلسفة", "احصاء": "الإحصاء", "اقتصاد منزلي": "الاقتصاد المنزلي",
	}
	for k, v := range subjects {
		if strings.Contains(m, normalizeArabic(k)) {
			entities.Subject = v
			break
		}
	}

	if strings.Contains(m, "الفصل الاول") || strings.Contains(m, "فصل اول") || strings.Contains(m, "الترم الاول") {
		entities.Semester = "الفصل الأول"
	} else if strings.Contains(m, "الفصل الثاني") || strings.Contains(m, "فصل ثاني") || strings.Contains(m, "الترم الثاني") {
		entities.Semester = "الفصل الثاني"
	}

	switch {
	case strings.Contains(m, "امتحان") || strings.Contains(m, "امتحانات") || strings.Contains(m, "اختبار") || strings.Contains(m, "اختبارات") || strings.Contains(m, "نموذج") || strings.Contains(m, "نماذج") || strings.Contains(m, "نهائي") || strings.Contains(m, "نهايه"):
		entities.ContentType = "امتحانات"
	case strings.Contains(m, "ملخص") || strings.Contains(m, "ملخصات") || strings.Contains(m, "تلخيص"):
		entities.ContentType = "ملخصات"
	case strings.Contains(m, "ورقه عمل") || strings.Contains(m, "اوراق عمل") || strings.Contains(m, "worksheet"):
		entities.ContentType = "أوراق عمل"
	case strings.Contains(m, "خطه") || strings.Contains(m, "خطة") || strings.Contains(m, "نمو مهني"):
		entities.ContentType = "خطط وملفات"
	case strings.Contains(m, "درس") || strings.Contains(m, "شرح"):
		entities.ContentType = "دروس وشروحات"
	case strings.Contains(m, "ملف") || strings.Contains(m, "ملفات"):
		entities.ContentType = "ملفات"
	}

	return entities
}

func mergeSearchEntities(previous, current searchEntities) searchEntities {
	merged := previous
	if current.Grade != "" {
		merged.Grade = current.Grade
	}
	if current.Subject != "" {
		merged.Subject = current.Subject
	}
	if current.Semester != "" {
		merged.Semester = current.Semester
	}
	if current.ContentType != "" {
		merged.ContentType = current.ContentType
	}
	if current.RawQuery != "" {
		merged.RawQuery = current.RawQuery
	}
	return merged
}

func buildSearchQuery(fallback string, e searchEntities) string {
	parts := []string{}
	if e.ContentType != "" {
		parts = append(parts, e.ContentType)
	}
	if e.Subject != "" {
		parts = append(parts, e.Subject)
	}
	if e.Grade != "" {
		parts = append(parts, e.Grade)
	}
	if e.Semester != "" {
		parts = append(parts, e.Semester)
	}
	if len(parts) == 0 {
		return strings.TrimSpace(fallback)
	}
	if strings.TrimSpace(fallback) != "" && !containsAnyNormalized(fallback, parts...) {
		parts = append(parts, fallback)
	}
	return strings.Join(parts, " ")
}

func containsAnyNormalized(v string, words ...string) bool {
	m := normalizeArabic(strings.ToLower(v))
	for _, w := range words {
		if strings.Contains(m, normalizeArabic(strings.ToLower(w))) {
			return true
		}
	}
	return false
}

func normalizeForMatch(v string) string {
	return normalizeArabic(strings.ToLower(v))
}

func entityMatchTerms(kind, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	switch kind {
	case "grade":
		switch value {
		case "الصف الأول":
			return []string{"الصف الأول", "صف أول", "اول ابتدائي"}
		case "الصف الثاني":
			return []string{"الصف الثاني", "صف ثاني"}
		case "الصف الثالث":
			return []string{"الصف الثالث", "صف ثالث"}
		case "الصف الرابع":
			return []string{"الصف الرابع", "صف رابع"}
		case "الصف الخامس":
			return []string{"الصف الخامس", "صف خامس"}
		case "الصف السادس":
			return []string{"الصف السادس", "صف سادس"}
		case "الصف السابع":
			return []string{"الصف السابع", "صف سابع", "سابع"}
		case "الصف الثامن":
			return []string{"الصف الثامن", "صف ثامن", "ثامن"}
		case "الصف التاسع":
			return []string{"الصف التاسع", "صف تاسع", "تاسع"}
		case "الصف العاشر":
			return []string{"الصف العاشر", "صف عاشر", "عاشر"}
		}
	case "subject":
		switch value {
		case "اللغة العربية":
			return []string{"اللغة العربية", "لغه عربيه", "لغة عربية", "عربي"}
		case "اللغة الإنجليزية":
			return []string{"اللغة الإنجليزية", "لغة انجليزية", "انجليزي", "انكليزي", "english"}
		case "الاجتماعيات":
			return []string{"الاجتماعيات", "اجتماعيات", "تربية اجتماعية", "تربيه اجتماعيه", "الدراسات الاجتماعية"}
		case "التربية الإسلامية":
			return []string{"التربية الإسلامية", "تربية اسلامية", "دين", "اسلامية"}
		case "رياضيات":
			return []string{"رياضيات", "الرياضيات"}
		case "علوم":
			return []string{"علوم", "العلوم"}
		}
	case "semester":
		switch value {
		case "الفصل الأول":
			return []string{"الفصل الأول", "فصل أول", "الفصل الاول", "فصل الاول"}
		case "الفصل الثاني":
			return []string{"الفصل الثاني", "فصل ثاني", "الفصل الثانى", "فصل ناني", "الفصل الثاني"}
		}
	case "content":
		switch value {
		case "امتحانات":
			return []string{"امتحان", "اختبار", "اختبارات", "امتحانات", "نهائي", "نهنئي"}
		case "خطط":
			return []string{"خطة", "خطط", "تحضير"}
		case "تقرير":
			return []string{"تقرير", "تقارير", "أداء", "اداء"}
		}
	}
	return []string{value}
}

func textHasAnyTerm(text string, terms []string) bool {
	m := normalizeForMatch(text)
	for _, term := range terms {
		if strings.Contains(m, normalizeForMatch(term)) {
			return true
		}
	}
	return false
}

func filterSearchResultsByEntities(e searchEntities, links []repo.ContentResult) []repo.ContentResult {
	if len(links) == 0 {
		return links
	}
	filtered := make([]repo.ContentResult, 0, len(links))
	for _, link := range links {
		haystack := strings.Join([]string{link.Title, link.Description, link.URL, link.Type}, " ")
		if e.Grade != "" && !textHasAnyTerm(haystack, entityMatchTerms("grade", e.Grade)) {
			continue
		}
		if e.Subject != "" && !textHasAnyTerm(haystack, entityMatchTerms("subject", e.Subject)) {
			continue
		}
		if e.Semester != "" && !textHasAnyTerm(haystack, entityMatchTerms("semester", e.Semester)) {
			continue
		}
		if e.ContentType != "" && !textHasAnyTerm(haystack, entityMatchTerms("content", e.ContentType)) {
			continue
		}
		filtered = append(filtered, link)
	}
	return filtered
}

func isNoiseOrEmojiOnly(message string) bool {
	m := strings.TrimSpace(message)
	if m == "" {
		return true
	}
	letterCount := 0
	for _, r := range m {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= 'ء' && r <= 'ي') || (r >= 'أ' && r <= 'ئ') {
			letterCount++
		}
	}
	if letterCount == 0 {
		return true
	}
	return utf8.RuneCountInString(m) <= 4 && letterCount <= 2
}

func isThanksMessage(message string) bool {
	m := normalizeForMatch(message)
	return containsAny(m, "شكرا", "شكراً", "مشكور", "مشكورين", "جزاكم الله", "الله يعطيكم", "يعطيكم العافيه", "يعطيكم العافية")
}

func isProfanityOrFrustration(message string) bool {
	m := normalizeForMatch(message)
	return containsAny(m, "خرا", "زفت", "قرف", "غبي", "سيء جدا", "مش راضي")
}

func buildSearchAnswer(e searchEntities, links []repo.ContentResult) string {
	query := buildSearchQuery("", e)
	if query == "" {
		query = "طلبك"
	}
	if len(links) > 0 {
		return "بحثت عن: " + query + "\n\nوجدت نتائج من قاعدة بيانات الموقع داخل الملفات والمقالات والمنشورات. افتح النتيجة الأقرب لك، وإذا لم تكن مطابقة تمامًا اكتب العنوان الكامل أو أعد البحث بصيغة: نوع الملف + المادة + الصف + الفصل."
	}
	missing := []string{}
	if e.Grade == "" {
		missing = append(missing, "الصف")
	}
	if e.Subject == "" {
		missing = append(missing, "المادة")
	}
	if e.Semester == "" {
		missing = append(missing, "الفصل")
	}
	if e.ContentType == "" {
		missing = append(missing, "نوع الملف")
	}
	if len(missing) > 0 {
		return "لم أجد نتائج واضحة حتى الآن. لتحسين البحث، أرسل التفاصيل بهذه الصيغة:\n\nنوع الملف + المادة + الصف + الفصل\n\nمثال: امتحانات اللغة العربية الصف التاسع الفصل الأول.\n\nالبيانات الناقصة غالبًا: " + strings.Join(missing, "، ") + "."
	}
	return "لم تظهر نتيجة مطابقة في البحث السريع داخل الدردشة. هذا لا يعني بالضرورة أن الملف غير موجود. جرّب فتح صفحة البحث بهذه الكلمات أو اكتب العنوان الكامل كما يظهر في الموقع. إذا كان الملف موجودًا ولم يظهر هنا، أرسل رابط الصفحة أو العنوان للإدارة ليتم تحسين الفهرسة."
}

func isUnsupportedPhoneOrUsernameQuestion(message string) bool {
	msg := normalizeArabic(strings.ToLower(message))
	if containsAny(msg,
		"بدون بريد",
		"بدون الايميل",
		"بدون الإيميل",
		"بدون ايميل",
		"بدون إيميل",
		"بدون email",
		"اسم المستخدم",
		"username",
		"user name",
	) {
		return true
	}

	mentionsPhoneChannel := containsAny(msg,
		"رقم الهاتف",
		"رقم تلفون",
		"رقم الموبايل",
		"برقم الهاتف",
		"بالهاتف",
		"عبر الهاتف",
		"تفعيل الهاتف",
		"كود على الهاتف",
		"sms",
		"رسالة نصية",
		"واتساب",
		"الواتساب",
		"whatsapp",
	)
	if !mentionsPhoneChannel {
		return false
	}

	// لا نمنع عبارات مثل: "تحميل من الهاتف" أو "الموقع لا يعمل على الهاتف".
	// المنع فقط عندما يكون الحديث عن إنشاء حساب/تفعيل/دخول/استعادة عبر الهاتف أو واتساب/SMS.
	return containsAny(msg,
		"تسجيل",
		"دخول",
		"انشاء حساب",
		"إنشاء حساب",
		"حساب",
		"تفعيل",
		"رمز",
		"كود",
		"استعاده",
		"استعادة",
		"كلمه المرور",
		"كلمة المرور",
		"بدون بريد",
	)
}

func unsupportedFeatureAnswer() string {
	return "حاليًا لا تدعم المنصة إنشاء الحساب أو تفعيل الحساب أو تسجيل الدخول عبر رقم الهاتف أو WhatsApp أو SMS أو اسم المستخدم.\n\nالطريقة المتاحة الآن هي البريد الإلكتروني فقط:\n\n1. استخدم بريدًا صحيحًا يمكنك الوصول إليه.\n2. افتح رسالة التفعيل التي تصلك على البريد.\n3. اضغط على رابط تأكيد البريد الإلكتروني داخل الرسالة.\n\nإذا كان البريد مكتوبًا خطأ أو لا تستطيع الوصول إليه، تواصل مع الإدارة واذكر البريد الخاطئ والبريد الصحيح المطلوب اعتماده."
}

func sanitizeUnsupportedFeatures(answer string) string {
	if strings.TrimSpace(answer) == "" {
		return answer
	}

	normalized := normalizeArabic(answer)
	blocked := containsAny(normalized,
		"تسجيل الدخول باستخدام رقم الهاتف",
		"إنشاء الحساب عبر رقم الهاتف",
		"انشاء الحساب عبر رقم الهاتف",
		"تفعيل الحساب عبر رقم الهاتف",
		"إعادة تعيينها عبر رقم الهاتف",
		"استعادة كلمة المرور عبر رقم الهاتف",
		"الهاتف المرتبط بحسابك",
		"عبر رقم الهاتف",
		"عبر الهاتف أو",
		"رسالة نصية",
		"sms",
		"واتساب",
		"الواتساب",
		"whatsapp",
		"اسم المستخدم",
		"username",
		"وسائل التواصل الاجتماعي",
		"حسابك على وسائل التواصل",
		"إعادة تعيينها عبر رقم الهاتف",
	)

	if blocked {
		return unsupportedFeatureAnswer()
	}

	return answer
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, normalizeArabic(needle)) {
			return true
		}
	}
	return false
}

func ruleAnswer(intent string) string {
	switch intent {
	case "unsupported_phone_feature":
		return unsupportedFeatureAnswer()
	case "auth_login_problem":
		return "لحل مشكلة تسجيل الدخول: تأكد من كتابة البريد وكلمة المرور بدون مسافات زائدة، ثم جرّب تسجيل الدخول مرة أخرى. إذا ظهرت رسالة أن البريد غير مفعّل، فعّل البريد أولًا. وإذا نسيت كلمة المرور، استخدم خيار استعادة كلمة المرور."
	case "password_reset_problem":
		return "إذا نسيت كلمة المرور، افتح صفحة تسجيل الدخول واضغط على خيار استعادة كلمة المرور، ثم اكتب بريدك المسجل. إذا لم تصل الرسالة، افحص البريد غير الهام وتأكد أن البريد مكتوب بشكل صحيح."
	case "auth_register_problem":
		return contextualAnswer("auth_register_problem", "register_steps", "", "")
	case "email_verification_problem":
		return "إذا لم تصلك رسالة أو كود التفعيل: افحص البريد غير الهام أولًا، وتأكد أن البريد في الحساب مكتوب بشكل صحيح. بعد ذلك استخدم زر إعادة إرسال التفعيل من صفحة الحساب. إذا بقيت المشكلة، أرسل للإدارة البريد المستخدم ووقت المحاولة."
	case "social_login_problem":
		return contextualAnswer("social_login_problem", "social_login_steps", "", "")
	case "download_problem":
		return contextualAnswer("download_problem", "download_diagnosis", "", "")
	case "download_location":
		return contextualAnswer("download_location", "download_location_steps", "", "")
	case "permission_problem":
		return "إذا ظهرت رسالة عدم وجود صلاحية، تأكد من تسجيل الدخول وتفعيل البريد. إذا بقيت الرسالة، أرسل رابط الملف للإدارة حتى تتم مراجعة الصلاحية أو التصنيف."
	case "file_not_found":
		return "إذا ظهر لك عند الضغط على رابط التحميل: عدم الوصول، المرفق غير موجود، أو أن الرابط لا يفتح، فغالبًا الرابط قديم أو انتهت صلاحيته أو تم نقل الملف.\n\nالحل الصحيح:\n\n1. لا تستخدم رابطًا منسوخًا أو قديمًا.\n2. افتح صفحة الملف الأصلية من داخل الموقع.\n3. اضغط تحميل من جديد.\n4. إذا بقي الخطأ، استخدم البحث باسم الامتحان أو الصف والمادة.\n5. إذا لم يظهر الملف، أرسل رابط الصفحة للإدارة حتى تتم مراجعة المرفق."
	case "country_or_curriculum":
		return "تأكد من اختيار الدولة أو المنهج الصحيح قبل البحث، لأن الصفوف والمواد والملفات قد تختلف بين الدول. إذا وجدت ملفًا في دولة أو تصنيف خاطئ، أرسل الرابط للإدارة."
	case "site_error":
		return "جرّب تحديث الصفحة أو فتحها من متصفح آخر. إذا تكررت المشكلة، أرسل للإدارة رابط الصفحة ورسالة الخطأ ونوع الجهاز أو المتصفح المستخدم."
	case "profile_problem":
		return "لتعديل بيانات الحساب، افتح صفحة الحساب أو الملف الشخصي إن كانت متاحة. إذا لم يظهر خيار التعديل، تواصل مع الإدارة واذكر البريد والبيانات المطلوب تعديلها."
	case "privacy_request":
		return "يمكنك مراجعة صفحات سياسة الخصوصية، شروط الاستخدام، سياسة الكوكيز، إخلاء المسؤولية، حقوق الملكية، وسياسة التحرير من روابط أسفل الموقع."
	case "report_content":
		return "للإبلاغ عن ملف أو محتوى خاطئ، أرسل رابط الصفحة ووضح نوع المشكلة: ملف لا يفتح، تصنيف خاطئ، محتوى غير مناسب، أو ملف لا يخص الصف أو المادة."
	case "request_content":
		return "لطلب إضافة ملف أو درس غير موجود، استخدم صفحة اتصل بنا واكتب بوضوح:\n\n1. الدولة أو المنهج.\n2. الصف.\n3. المادة.\n4. الفصل الدراسي.\n5. نوع الملف المطلوب مثل اختبار، ورقة عمل، خطة، تحليل محتوى.\n6. العنوان الكامل إن كان متوفرًا.\n\nكلما كان الطلب أدق، كانت مراجعته أسرع."
	case "contact_support":
		return "إذا بقيت المشكلة بعد تجربة الحلول، استخدم صفحة التواصل واكتب البريد المستخدم ورابط الصفحة ورسالة الخطأ الظاهرة حتى تستطيع الإدارة مراجعتها بسرعة."
	case "find_grade", "find_subject", "find_semester", "search_content":
		return "اكتب اسم الصف والمادة والفصل أو نوع الملف الذي تريده، مثال: رياضيات الصف التاسع الفصل الأول اختبار، وسأبحث لك داخل محتوى الموقع."
	default:
		return "لم أفهم نوع المشكلة بدقة. اكتب المشكلة بجملة قصيرة مثل: لا أستطيع تحميل ملف، لم تصلني رسالة التفعيل، نسيت كلمة المرور، أو أريد ملف رياضيات للصف التاسع."
	}
}

func nextSuggestionsForStep(intent, step string, entities searchEntities) []string {
	switch intent {
	case "unsupported_phone_feature":
		return []string{"لدي مشكلة في البريد", "كتبت البريد خطأ", "أريد التواصل مع الإدارة"}
	case "download_problem":
		if step == "in_app_browser_download" {
			return []string{"فتحت الصفحة من Chrome", "التحميل لا يزال لا يعمل", "أريد التواصل مع الإدارة"}
		}
		if step == "download_button_issue" {
			return []string{"العداد لا يعمل", "زر التحميل لا يستجيب", "أريد التواصل مع الإدارة"}
		}
		return []string{"سجلت الدخول لكن التحميل لا يعمل", "تظهر رسالة لا تملك صلاحية", "عملت تحميل ولا أجد الملف"}
	case "download_location":
		return []string{"الملف لا يظهر في التنزيلات", "أريد إعادة تحميل الملف", "أريد التواصل مع الإدارة"}
	case "permission_problem", "file_not_found":
		return []string{"أريد الإبلاغ عن ملف خاطئ", "لم أجد الملف الذي أبحث عنه", "أريد التواصل مع الإدارة"}
	case "auth_login_problem":
		return []string{"نسيت كلمة المرور", "البريد غير مفعل", "لا تصلني رسالة التفعيل"}
	case "social_login_problem":
		return []string{"أريد الدخول بالبريد وكلمة المرور", "نسيت كلمة المرور", "ما زالت المشكلة موجودة"}
	case "auth_register_problem":
		return []string{"يظهر أن البريد مستخدم سابقًا", "لا تصلني رسالة التفعيل", "أريد فتح صفحة تسجيل الدخول"}
	case "password_reset_problem":
		return []string{"لم تصلني رسالة استعادة كلمة المرور", "البريد مكتوب صحيح", "أريد التواصل مع الإدارة"}
	case "email_verification_problem":
		if step == "wrong_email" {
			return []string{"أريد تعديل البريد", "لا أستطيع الدخول للحساب", "أريد التواصل مع الإدارة"}
		}
		if step == "email_access_problem" {
			return []string{"لا أستطيع فتح بريدي", "صندوق البريد ممتلئ", "أريد تغيير البريد"}
		}
		return []string{"البريد مكتوب صحيح", "فحصت البريد غير الهام", "ما زالت المشكلة موجودة"}
	case "account_lookup_privacy":
		return []string{"أريد استعادة كلمة المرور", "أريد فتح صفحة تسجيل الدخول", "أريد التواصل مع الإدارة"}
	case "search_content", "find_grade", "find_subject", "find_semester":
		base := buildSearchQuery("", entities)
		if base == "" {
			return []string{"امتحانات الصف التاسع لغة عربية", "ملخصات الصف العاشر", "أوراق عمل رياضيات"}
		}
		s := []string{}
		if entities.ContentType == "" {
			s = append(s, "امتحانات "+base)
		}
		if entities.Semester == "" {
			s = append(s, base+" الفصل الأول")
		}
		if entities.ContentType != "ملخصات" {
			s = append(s, "ملخصات "+base)
		}
		if len(s) == 0 {
			s = append(s, "أوراق عمل "+base, "اختبارات "+base, "دروس "+base)
		}
		if len(s) > 3 {
			return s[:3]
		}
		return s
	case "country_or_curriculum":
		return []string{"الملف لا يناسب المنهج", "كيف أجد ملفات صف معين", "لم أجد الملف الذي أبحث عنه"}
	case "site_error":
		return []string{"الصفحة لا تفتح", "الموقع بطيء عندي", "الموقع لا يعمل بشكل جيد على الهاتف"}
	case "contact_support", "report_content", "request_content":
		return []string{"أريد الإبلاغ عن ملف خاطئ", "أريد طلب إضافة ملف أو درس", "ما زالت المشكلة موجودة"}
	default:
		return []string{}
	}
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
