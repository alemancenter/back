package contentaudit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	coreai "github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/pkg/logger"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var ErrUnsupportedContentType = errors.New("unsupported content type")
var ErrFixAlreadyClosed = errors.New("fix preview is already applied or rejected")
var ErrAIAnalysisInProgress = errors.New("AI analysis is already running for this content")

var fixPreviewLocks sync.Map

const contentIntelligencePromptVersion = "content-intelligence-v1"

func acquireContentAILock(ctx context.Context, key string, ttl time.Duration) (func(), bool) {
	key = database.Redis().Key("content_ai_lock", key)
	ok, err := database.Redis().Cache().SetNX(ctx, key, "1", ttl).Result()
	if err != nil {
		logger.Warn("content AI redis lock unavailable; continuing without distributed lock", zap.String("key", key), zap.Error(err))
		return func() {}, true
	}
	if !ok {
		return func() {}, false
	}
	return func() { _ = database.Redis().Cache().Del(context.Background(), key).Err() }, true
}

type AIAnalyzeRequest struct {
	RunID       *uint  `json:"run_id,omitempty"`
	FindingID   *uint  `json:"finding_id,omitempty"`
	ContentType string `json:"content_type"`
	ContentID   string `json:"content_id"`
	CountryCode string `json:"country_code"`
	Title       string `json:"title"`
	Content     string `json:"content"`
	URL         string `json:"url"`
}

type AIFixRequest struct {
	DecisionID uint64 `json:"decision_id"`
}
type ApplyFixRequest struct {
	FixPreviewID uint64 `json:"fix_preview_id"`
	Note         string `json:"note"`
}
type RejectFixRequest struct {
	FixPreviewID uint64 `json:"fix_preview_id"`
	Note         string `json:"note"`
}

type loadedContent struct {
	Type        string
	ID          uint
	CountryCode string
	Title       string
	Content     string
	URL         string
}

type aiIssueDTO struct {
	Type     string `json:"type"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Action   string `json:"action"`
	Evidence string `json:"evidence,omitempty"`
}
type aiSuggestionDTO struct {
	Type     string `json:"type"`
	Priority string `json:"priority"`
	Message  string `json:"message"`
}

type aiReport struct {
	Decision         string            `json:"decision"`
	AdSenseRisk      string            `json:"adsense_risk"`
	Score            int               `json:"score"`
	PolicyScore      int               `json:"policy_score"`
	SEOScore         int               `json:"seo_score"`
	LanguageScore    int               `json:"language_score"`
	SafetyLinksScore int               `json:"safety_links_score"`
	StructureScore   int               `json:"structure_score"`
	CanAutoFix       bool              `json:"can_auto_fix"`
	Summary          string            `json:"summary"`
	Issues           []aiIssueDTO      `json:"issues"`
	Suggestions      []aiSuggestionDTO `json:"suggestions"`
	Provider         string            `json:"provider"`
	Model            string            `json:"model"`
	PromptVersion    string            `json:"prompt_version"`
	Tokens           int               `json:"tokens"`
	ProcessingTimeMS int64             `json:"processing_time_ms"`
}

func (s *Service) AnalyzeWithAI(ctx context.Context, req AIAnalyzeRequest, userID *uint) (*models.ContentAIDecision, error) {
	content, err := s.resolveContent(ctx, req)
	if err != nil {
		return nil, err
	}
	content.CountryCode = sanitizeCountryCode(content.CountryCode)
	if content.CountryCode == "" {
		content.CountryCode = "jo"
	}
	contentKey := fmt.Sprintf("analyze:%s:%s:%d", content.Type, content.CountryCode, content.ID)
	unlock, locked := acquireContentAILock(ctx, contentKey, 2*time.Minute)
	if !locked {
		if existing, err := s.repo.LatestAIDecision(ctx, content.Type, fmt.Sprintf("%s:%d", content.CountryCode, content.ID), content.CountryCode); err == nil && existing != nil {
			return existing, nil
		}
		return nil, ErrAIAnalysisInProgress
	}
	defer unlock()
	plain := normalizePlainText(content.Content)
	report := buildDecisionReport(content)

	if s.ai != nil && strings.TrimSpace(plain) != "" {
		if aiResp, err := s.ai.RunContentIntelligence(ctx, coreai.ContentIntelligenceRequest{Task: "audit_content", ContentType: content.Type, Title: content.Title, Content: content.Content, PlainText: plain, URL: firstNonEmptyLocal(req.URL, content.URL), Language: "ar"}); err == nil {
			report = reportFromAI(aiResp, report)
		}
	}
	report = enforcePolicyDecision(report)
	reportBytes, _ := json.Marshal(report)
	decision := &models.ContentAIDecision{RunID: req.RunID, FindingID: req.FindingID, ContentType: content.Type, ContentID: fmt.Sprintf("%s:%d", content.CountryCode, content.ID), CountryCode: content.CountryCode, Title: content.Title, Decision: report.Decision, AdSenseRisk: report.AdSenseRisk, Score: report.Score, PolicyScore: report.PolicyScore, SEOScore: report.SEOScore, LanguageScore: report.LanguageScore, SafetyLinksScore: report.SafetyLinksScore, StructureScore: report.StructureScore, CanAutoFix: report.CanAutoFix, Provider: report.Provider, Model: report.Model, PromptVersion: report.PromptVersion, AITokens: report.Tokens, ProcessingTimeMS: report.ProcessingTimeMS, Summary: report.Summary, ReportJSON: string(reportBytes), CreatedByUserID: userID}
	if decision.Provider == "" {
		decision.Provider = "local_decision_engine"
	}
	if decision.Model == "" {
		decision.Model = "alemancenter-content-audit-v1"
	}
	if decision.PromptVersion == "" {
		decision.PromptVersion = contentIntelligencePromptVersion
	}
	for _, issue := range report.Issues {
		decision.Issues = append(decision.Issues, models.ContentAIIssue{Type: issue.Type, Severity: issue.Severity, Message: issue.Message, Action: issue.Action, Evidence: issue.Evidence})
	}
	for _, sug := range report.Suggestions {
		decision.Suggestions = append(decision.Suggestions, models.ContentAISuggestion{Type: sug.Type, Priority: sug.Priority, Message: sug.Message})
	}
	if err := s.repo.SaveAIDecision(ctx, decision); err != nil {
		return nil, fmt.Errorf("save AI decision failed: %w", err)
	}
	return decision, nil
}

func (s *Service) GetAIDecision(ctx context.Context, id uint64) (*models.ContentAIDecision, error) {
	return s.repo.GetAIDecision(ctx, id)
}
func (s *Service) LatestAIDecision(ctx context.Context, contentType, contentID, countryCode string) (*models.ContentAIDecision, error) {
	cc, normalized, _ := normalizeContentReference(contentID, countryCode)
	return s.repo.LatestAIDecision(ctx, normalizeContentType(contentType), normalized, cc)
}
func (s *Service) GetFixPreview(ctx context.Context, id uint64) (*models.ContentAIFixPreview, error) {
	return s.repo.GetFixPreview(ctx, id)
}

func (s *Service) CreateFixPreview(ctx context.Context, decisionID uint64) (*models.ContentAIFixPreview, error) {
	lockIface, _ := fixPreviewLocks.LoadOrStore(decisionID, &sync.Mutex{})
	lock := lockIface.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	unlockRedis, lockedRedis := acquireContentAILock(ctx, fmt.Sprintf("fix-preview:%d", decisionID), 4*time.Minute)
	if !lockedRedis {
		if existing, err := s.repo.LatestFixPreviewByDecision(ctx, decisionID); err == nil && existing != nil {
			return existing, nil
		}
		return nil, fmt.Errorf("AI fix preview is already running for this decision")
	}
	defer unlockRedis()

	if existing, err := s.repo.LatestFixPreviewByDecision(ctx, decisionID); err == nil && existing != nil {
		if isMeaningfulFix(existing.OriginalContent, existing.FixedContent, existing.ContentType) {
			return existing, nil
		}
	}

	decision, err := s.repo.GetAIDecision(ctx, decisionID)
	if err != nil {
		return nil, err
	}
	cc, normalizedID, numericID := normalizeContentReference(decision.ContentID, decision.CountryCode)
	content, err := s.loadContentByRef(ctx, decision.ContentType, cc, numericID)
	if err != nil {
		return nil, err
	}

	originalPlain := normalizePlainText(content.Content)
	fixedTitle, fixedContent, summary := localFixContent(content.Title, content.Content, decision.Issues, content.Type)

	if s.ai != nil {
		// First try the dedicated content-intelligence fixing task. It should return a real fixed HTML draft.
		if aiResp, err := s.ai.RunContentIntelligence(ctx, coreai.ContentIntelligenceRequest{Task: "fix_content", ContentType: content.Type, Title: content.Title, Content: content.Content, PlainText: originalPlain, URL: content.URL, Language: "ar"}); err == nil {
			candidateTitle := firstNonEmptyLocal(strings.TrimSpace(aiResp.FixedTitle), fixedTitle)
			candidateContent := strings.TrimSpace(aiResp.FixedContent)
			candidateSummary := firstNonEmptyLocal(strings.TrimSpace(aiResp.FixSummary), "تم إنشاء نسخة محسّنة بالذكاء الاصطناعي وفق سياسات AdSense ومعايير SEO.")
			if isMeaningfulFix(content.Content, candidateContent, content.Type) {
				fixedTitle = candidateTitle
				fixedContent = normalizeFixedHTML(candidateContent)
				summary = candidateSummary
			}
		}

		// If the fixing task returns the same weak content, use the existing project article-generation pipeline.
		// This keeps the style consistent with the generator already used for articles/posts.
		if !isMeaningfulFix(content.Content, fixedContent, content.Type) {
			if article, err := s.ai.GenerateSEOArticle(content.Title, content.Type); err == nil && article != nil {
				candidateContent := firstNonEmptyLocal(article.ContentHTML, formatPlainContentToHTML(article.Content))
				if isMeaningfulFix(content.Content, candidateContent, content.Type) {
					fixedTitle = firstNonEmptyLocal(article.Title, content.Title)
					fixedContent = normalizeFixedHTML(candidateContent)
					summary = "تم توليد نسخة موسّعة ومحسّنة باستخدام نفس AI Pipeline الخاص بتوليد المقالات في المشروع، مع تحسين SEO والبنية والقيمة التعليمية."
				}
			}
		}
	}

	// Final guard: never save a preview that is effectively identical to the original weak content.
	if !isMeaningfulFix(content.Content, fixedContent, content.Type) {
		fixedTitle, fixedContent, summary = localExpandedFallback(content.Title, content.Content, content.Type)
	}

	fixedTitle = strings.TrimSpace(fixedTitle)
	if fixedTitle == "" {
		fixedTitle = content.Title
	}
	fixedContent = normalizeFixedHTML(fixedContent)
	// After localExpandedFallback, only reject completely empty output.
	// Word-count gates do not apply here — the fallback is the last resort
	// and always produces content that differs structurally from the original.
	if strings.TrimSpace(normalizePlainText(fixedContent)) == "" {
		return nil, fmt.Errorf("generated fix preview is not meaningful enough for decision %d", decision.ID)
	}

	preview := &models.ContentAIFixPreview{DecisionID: decision.ID, ContentType: normalizeContentType(decision.ContentType), ContentID: normalizedID, CountryCode: cc, OriginalTitle: content.Title, OriginalContent: content.Content, FixedTitle: fixedTitle, FixedContent: fixedContent, FixSummary: summary, Status: models.AIFixStatusPreviewed}
	saveCtx, saveCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer saveCancel()
	if err := s.repo.SaveFixPreview(saveCtx, preview); err != nil {
		return nil, fmt.Errorf("save AI fix preview failed: %w", err)
	}
	return preview, nil
}

func (s *Service) ApplyFix(ctx context.Context, previewID uint64, userID *uint, note string) (*models.ContentAIFixPreview, error) {
	preview, err := s.repo.GetFixPreview(ctx, previewID)
	if err != nil {
		return nil, err
	}
	if preview.Status != models.AIFixStatusPreviewed {
		return nil, ErrFixAlreadyClosed
	}
	_, _, id := normalizeContentReference(preview.ContentID, preview.CountryCode)
	db := database.GetManager().GetByCode(preview.CountryCode).WithContext(ctx)

	var notifType, notifTitle, notifMsg, notifURL string
	var authorID *uint

	switch normalizeContentType(preview.ContentType) {
	case "article":
		var item models.Article
		if err := db.First(&item, id).Error; err != nil {
			return nil, err
		}
		item.Title = preview.FixedTitle
		item.Content = preview.FixedContent
		if err := db.Save(&item).Error; err != nil {
			return nil, err
		}
		authorID = item.AuthorID
		notifType = `App\Notifications\ArticleFixed`
		notifTitle = "تم تطبيق تصحيح AI على مقالة"
		notifMsg = fmt.Sprintf("تم تحديث المقالة بنسخة محسّنة بالذكاء الاصطناعي: %s", item.Title)
		notifURL = fmt.Sprintf("/dashboard/lesson/articles/edit/%d", item.ID)
	case "post":
		var item models.Post
		if err := db.First(&item, id).Error; err != nil {
			return nil, err
		}
		item.Title = preview.FixedTitle
		item.Content = preview.FixedContent
		if err := db.Save(&item).Error; err != nil {
			return nil, err
		}
		authorID = item.AuthorID
		notifType = `App\Notifications\PostFixed`
		notifTitle = "تم تطبيق تصحيح على منشور"
		notifMsg = fmt.Sprintf("تم تحديث المنشور بنسخة محسّنة بالذكاء الاصطناعي: %s", item.Title)
		notifURL = fmt.Sprintf("/dashboard/posts/edit/%d", item.ID)
	default:
		return nil, ErrUnsupportedContentType
	}

	now := time.Now()
	preview.Status = models.AIFixStatusApplied
	preview.AppliedByUserID = userID
	preview.AppliedAt = &now
	if err := s.repo.UpdateFixPreview(ctx, preview); err != nil {
		return nil, err
	}
	_ = s.repo.CreateApprovalLog(ctx, &models.ContentAIApprovalLog{FixPreviewID: preview.ID, DecisionID: preview.DecisionID, Action: models.AIFixStatusApplied, UserID: userID, Note: note})

	if s.notification != nil {
		includeIDs := []uint{}
		if userID != nil {
			includeIDs = append(includeIDs, *userID)
		}
		if authorID != nil && (userID == nil || *authorID != *userID) {
			includeIDs = append(includeIDs, *authorID)
		}
		permissions := []string{"manage content audit", "manage articles", "manage posts"}
		go func() {
			_ = s.notification.NotifyUsersWithPermissions(notifType, notifTitle, notifMsg, notifURL, permissions, includeIDs...)
		}()
	}

	return preview, nil
}

func (s *Service) RejectFix(ctx context.Context, previewID uint64, userID *uint, note string) (*models.ContentAIFixPreview, error) {
	preview, err := s.repo.GetFixPreview(ctx, previewID)
	if err != nil {
		return nil, err
	}
	if preview.Status != models.AIFixStatusPreviewed {
		return nil, ErrFixAlreadyClosed
	}
	now := time.Now()
	preview.Status = models.AIFixStatusRejected
	preview.RejectedByUserID = userID
	preview.RejectedAt = &now
	if err := s.repo.UpdateFixPreview(ctx, preview); err != nil {
		return nil, err
	}
	_ = s.repo.CreateApprovalLog(ctx, &models.ContentAIApprovalLog{FixPreviewID: preview.ID, DecisionID: preview.DecisionID, Action: models.AIFixStatusRejected, UserID: userID, Note: note})
	return preview, nil
}

func (s *Service) resolveContent(ctx context.Context, req AIAnalyzeRequest) (*loadedContent, error) {
	ct := normalizeContentType(req.ContentType)
	if ct != "article" && ct != "post" {
		return nil, ErrUnsupportedContentType
	}
	cc, _, id := normalizeContentReference(req.ContentID, req.CountryCode)
	if id > 0 {
		return s.loadContentByRef(ctx, ct, cc, id)
	}
	return &loadedContent{Type: ct, ID: 0, CountryCode: cc, Title: strings.TrimSpace(req.Title), Content: req.Content, URL: req.URL}, nil
}

func (s *Service) loadContentByRef(ctx context.Context, contentType, countryCode string, id uint) (*loadedContent, error) {
	if id == 0 {
		return nil, strconv.ErrSyntax
	}
	db := database.GetManager().GetByCode(countryCode).WithContext(ctx)
	switch normalizeContentType(contentType) {
	case "article":
		var item models.Article
		if err := db.First(&item, id).Error; err != nil {
			return nil, err
		}
		normalizedCountry := sanitizeCountryCode(countryCode)
		if normalizedCountry == "" {
			normalizedCountry = "jo"
		}
		return &loadedContent{Type: "article", ID: item.ID, CountryCode: normalizedCountry, Title: item.Title, Content: item.Content, URL: fmt.Sprintf("/%s/articles/%d", normalizedCountry, item.ID)}, nil
	case "post":
		var item models.Post
		if err := db.First(&item, id).Error; err != nil {
			return nil, err
		}
		normalizedCountry := sanitizeCountryCode(firstNonEmptyLocal(item.Country, countryCode))
		if normalizedCountry == "" {
			normalizedCountry = "jo"
		}
		return &loadedContent{Type: "post", ID: item.ID, CountryCode: normalizedCountry, Title: item.Title, Content: item.Content, URL: fmt.Sprintf("/%s/posts/%d", normalizedCountry, item.ID)}, nil
	default:
		return nil, ErrUnsupportedContentType
	}
}

func buildDecisionReport(content *loadedContent) aiReport {
	plain := normalizePlainText(content.Content)
	wc := aiWordCount(plain)
	minWords := 300
	policyScore, seoScore, languageScore, linkScore, structureScore := 100, 70, 100, 100, 85
	issues := []aiIssueDTO{}
	suggestions := []aiSuggestionDTO{}
	if wc < minWords {
		seoScore -= 30
		languageScore -= 15
		issues = append(issues, aiIssueDTO{"quality", "high", "المحتوى قصير وقد يُعتبر Thin Content.", "expand_content", fmt.Sprintf("words=%d minimum=%d", wc, minWords)})
	}
	if strings.TrimSpace(content.Title) == "" || len([]rune(content.Title)) < 12 {
		seoScore -= 20
		issues = append(issues, aiIssueDTO{"seo", "medium", "العنوان قصير أو غير واضح لمحركات البحث.", "improve_title", ""})
	}
	if unsafeMarkupPattern.MatchString(content.Content) {
		policyScore -= 45
		linkScore -= 35
		issues = append(issues, aiIssueDTO{"security", "critical", "يوجد كود أو رابط غير آمن داخل المحتوى.", "remove_unsafe_markup", "script/iframe/javascript detected"})
	}
	lower := strings.ToLower(plain)
	for _, term := range []string{"gambling", "casino", "porn", "xxx", "سكس", "قمار", "كازينو"} {
		if strings.Contains(lower, term) {
			policyScore -= 40
			issues = append(issues, aiIssueDTO{"policy", "high", "يوجد مصطلح حساس قد يؤثر على AdSense.", "rewrite_sensitive_terms", term})
		}
	}
	if wc < minWords {
		suggestions = append(suggestions, aiSuggestionDTO{"content", "high", "توسيع المحتوى بإضافة مقدمة، نقاط رئيسية، وأمثلة تعليمية وأسئلة شائعة مرتبطة بالموضوع."})
	}
	if seoScore < 90 {
		suggestions = append(suggestions, aiSuggestionDTO{"seo", "medium", "تحسين العنوان والوصف الداخلي وربط المحتوى بصفحات تعليمية ذات صلة."})
	}
	policyScore, seoScore, languageScore, linkScore, structureScore = clamp(policyScore), clamp(seoScore), clamp(languageScore), clamp(linkScore), clamp(structureScore)
	total := int(float64(policyScore)*0.40 + float64(seoScore)*0.25 + float64(languageScore)*0.20 + float64(linkScore)*0.10 + float64(structureScore)*0.05)
	decision, risk := classifyDecision(total, policyScore, linkScore, issues)
	if len(suggestions) == 0 {
		suggestions = append(suggestions, aiSuggestionDTO{"publish", "low", "المحتوى جاهز مبدئيًا، مع مراجعة بشرية نهائية قبل النشر أو تفعيل الإعلانات."})
	}
	return aiReport{Decision: decision, AdSenseRisk: risk, Score: total, PolicyScore: policyScore, SEOScore: seoScore, LanguageScore: languageScore, SafetyLinksScore: linkScore, StructureScore: structureScore, CanAutoFix: decision != models.AIDecisionRejected, Summary: buildSummary(decision, total, risk), Issues: issues, Suggestions: suggestions, Provider: "local_decision_engine", Model: "alemancenter-content-audit-v1", PromptVersion: contentIntelligencePromptVersion}
}

func reportFromAI(ai *coreai.ContentIntelligenceResponse, fallback aiReport) aiReport {
	out := fallback
	if ai.Decision != "" {
		out.Decision = normalizeDecision(ai.Decision)
	}
	if ai.AdSenseRisk != "" {
		out.AdSenseRisk = normalizeRisk(ai.AdSenseRisk)
	}
	if ai.Score > 0 {
		out.Score = clamp(ai.Score)
	}
	if ai.PolicyScore > 0 {
		out.PolicyScore = clamp(ai.PolicyScore)
	}
	if ai.SEOScore > 0 {
		out.SEOScore = clamp(ai.SEOScore)
	}
	if ai.LanguageScore > 0 {
		out.LanguageScore = clamp(ai.LanguageScore)
	}
	if ai.SafetyLinksScore > 0 {
		out.SafetyLinksScore = clamp(ai.SafetyLinksScore)
	}
	if ai.StructureScore > 0 {
		out.StructureScore = clamp(ai.StructureScore)
	}
	out.CanAutoFix = ai.CanAutoFix || out.Decision != models.AIDecisionRejected
	if ai.Summary != "" {
		out.Summary = ai.Summary
	}
	if len(ai.Issues) > 0 {
		out.Issues = nil
		for _, i := range ai.Issues {
			out.Issues = append(out.Issues, aiIssueDTO{Type: i.Type, Severity: i.Severity, Message: i.Message, Action: i.Action, Evidence: i.Evidence})
		}
	}
	if len(ai.Suggestions) > 0 {
		out.Suggestions = nil
		for _, sg := range ai.Suggestions {
			out.Suggestions = append(out.Suggestions, aiSuggestionDTO{Type: sg.Type, Priority: sg.Priority, Message: sg.Message})
		}
	}
	for i := range out.Issues {
		out.Issues[i].Type = firstNonEmptyLocal(out.Issues[i].Type, "quality")
		out.Issues[i].Severity = normalizeSeverity(out.Issues[i].Severity)
		out.Issues[i].Action = firstNonEmptyLocal(out.Issues[i].Action, "manual_review")
	}
	for i := range out.Suggestions {
		out.Suggestions[i].Type = firstNonEmptyLocal(out.Suggestions[i].Type, "content")
		out.Suggestions[i].Priority = normalizePriority(out.Suggestions[i].Priority)
	}
	out.Provider = firstNonEmptyLocal(ai.Provider, "together_ai")
	out.Model = ai.Model
	out.PromptVersion = firstNonEmptyLocal(ai.PromptVersion, contentIntelligencePromptVersion)
	out.Tokens = ai.Tokens
	out.ProcessingTimeMS = ai.ProcessingTimeMS
	return out
}

func enforcePolicyDecision(r aiReport) aiReport {
	hasHigh := false
	for _, i := range r.Issues {
		if i.Severity == "critical" {
			r.Decision = models.AIDecisionRejected
			r.AdSenseRisk = "critical"
			r.CanAutoFix = false
			return r
		}
		if i.Severity == "high" {
			hasHigh = true
		}
	}
	if hasHigh && r.Decision == models.AIDecisionApproved {
		r.Decision = models.AIDecisionNeedsFix
		r.AdSenseRisk = "medium"
	}
	if r.Score < 90 && r.Decision == models.AIDecisionApproved {
		r.Decision = models.AIDecisionNeedsFix
		r.AdSenseRisk = "medium"
	}
	if r.Summary == "" {
		r.Summary = buildSummary(r.Decision, r.Score, r.AdSenseRisk)
	}
	return r
}

func classifyDecision(total, policy, links int, issues []aiIssueDTO) (string, string) {
	for _, issue := range issues {
		if issue.Severity == "critical" {
			return models.AIDecisionRejected, "critical"
		}
	}
	if policy < 45 || total < 40 {
		return models.AIDecisionRejected, "high"
	}
	if total < 60 || links < 70 {
		return models.AIDecisionRestrictedAds, "high"
	}
	if total < 90 {
		return models.AIDecisionNeedsFix, "medium"
	}
	return models.AIDecisionApproved, "low"
}

func localFixContent(title, content string, issues []models.ContentAIIssue, contentType string) (string, string, string) {
	fixed := html.UnescapeString(content)
	fixed = scriptStylePattern.ReplaceAllString(fixed, "")
	fixed = unsafeMarkupPattern.ReplaceAllString(fixed, "")
	fixed = strings.ReplaceAll(fixed, "javascript:", "")
	fixed = normalizeFixedHTML(fixed)
	fixedTitle := strings.TrimSpace(title)
	if len([]rune(fixedTitle)) < 20 && fixedTitle != "" {
		fixedTitle = "دليل شامل حول " + fixedTitle
	}
	if strings.TrimSpace(fixed) == "" {
		fixed = "<p>يرجى إعادة بناء هذا المحتوى يدويًا لأنه فارغ أو غير صالح للنشر.</p>"
	}
	return fixedTitle, fixed, "تم إنشاء نسخة آمنة أولية: إزالة الأكواد الخطرة، تحسين تقسيم الفقرات، وتجهيز النص للمراجعة البشرية قبل الاعتماد."
}

func isMeaningfulFix(originalHTML, fixedHTML, contentType string) bool {
	fixedHTML = strings.TrimSpace(fixedHTML)
	if fixedHTML == "" {
		return false
	}
	originalPlain := normalizePlainText(originalHTML)
	fixedPlain := normalizePlainText(fixedHTML)
	if fixedPlain == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(originalPlain), strings.TrimSpace(fixedPlain)) {
		return false
	}
	originalWords := aiWordCount(originalPlain)
	fixedWords := aiWordCount(fixedPlain)
	minWords := minFixWords(contentType)
	if fixedWords < minWords {
		return false
	}
	if originalWords > 0 && fixedWords < originalWords+40 {
		return false
	}
	return true
}

func minFixWords(_ string) int {
	return 300
}

func normalizeFixedHTML(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = scriptStylePattern.ReplaceAllString(raw, "")
	raw = unsafeMarkupPattern.ReplaceAllString(raw, "")
	raw = strings.ReplaceAll(raw, "javascript:", "")
	raw = html.UnescapeString(raw)
	if strings.Contains(strings.ToLower(raw), "<p") || strings.Contains(strings.ToLower(raw), "<h2") || strings.Contains(strings.ToLower(raw), "<ul") {
		return strings.TrimSpace(raw)
	}
	return formatPlainContentToHTML(raw)
}

func formatPlainContentToHTML(content string) string {
	content = strings.TrimSpace(htmlTagPattern.ReplaceAllString(content, "\n"))
	content = html.UnescapeString(content)
	content = regexp.MustCompile(`\n{3,}`).ReplaceAllString(content, "\n\n")
	content = regexp.MustCompile(`[ \t]+`).ReplaceAllString(content, " ")
	if content == "" {
		return ""
	}
	blocks := regexp.MustCompile(`\n\s*\n`).Split(content, -1)
	var out []string
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		runes := []rune(block)
		if len(runes) <= 70 && !strings.HasSuffix(block, ".") && !strings.HasSuffix(block, "؟") && !strings.HasSuffix(block, "!") {
			out = append(out, "<h2>"+html.EscapeString(block)+"</h2>")
		} else {
			out = append(out, "<p>"+html.EscapeString(block)+"</p>")
		}
	}
	return strings.Join(out, "\n")
}

func localExpandedFallback(title, content, contentType string) (string, string, string) {
	plain := normalizePlainText(content)
	if plain == "" {
		plain = "يحتاج هذا المحتوى إلى إعادة بناء كاملة ليصبح مناسبًا للنشر التعليمي وسياسات الإعلانات."
	}
	fixedTitle := strings.TrimSpace(title)
	if fixedTitle == "" {
		fixedTitle = "محتوى تعليمي محسّن"
	}
	if len([]rune(fixedTitle)) < 20 {
		fixedTitle = "دليل شامل حول " + fixedTitle
	}
	intro := fmt.Sprintf("يمثل موضوع %s جانبًا مهمًا في المحتوى التعليمي، لأنه يساعد الطالب وولي الأمر والمعلم على فهم الفكرة بصورة أوضح والاستفادة منها عمليًا.", strings.TrimSpace(title))
	body := plain
	value := "ولكي يكون المحتوى أكثر فائدة، يجب أن يوضح الفكرة الأساسية، ويعرض أمثلة قريبة من الواقع التعليمي، ثم يقدّم إرشادات عملية يمكن تطبيقها بسهولة. هذا الأسلوب يزيد من جودة الصفحة ويمنح القارئ قيمة واضحة بدل الاكتفاء بجمل عامة قصيرة."
	seo := "من الناحية التحريرية، يُفضّل تقسيم المحتوى إلى فقرات واضحة، وإضافة عناوين فرعية مناسبة، وربط الموضوع بسياق تعليمي مباشر. كما يساعد استخدام كلمات مفتاحية طبيعية دون حشو على تحسين الظهور في محركات البحث مع الحفاظ على تجربة قراءة جيدة."
	faq := "يمكن أيضًا إضافة أسئلة شائعة في نهاية المحتوى للإجابة عن أهم ما يبحث عنه القارئ، مثل أهمية الموضوع، وطريقة الاستفادة منه، والخطوات العملية المرتبطة به."
	fixed := fmt.Sprintf("<p>%s</p>\n<h2>شرح الفكرة الأساسية</h2>\n<p>%s</p>\n<h2>القيمة التعليمية للمحتوى</h2>\n<p>%s</p>\n<h2>تحسينات مقترحة للنشر</h2>\n<p>%s</p>\n<h2>أسئلة يمكن إضافتها</h2>\n<p>%s</p>", html.EscapeString(intro), html.EscapeString(body), html.EscapeString(value), html.EscapeString(seo), html.EscapeString(faq))
	return fixedTitle, fixed, "تعذر الحصول على نسخة موسّعة كافية من مزود AI، لذلك تم إنشاء مسودة تحسين محلية موسّعة كحل احتياطي للمراجعة البشرية."
}

var entityPattern = regexp.MustCompile(`&[a-zA-Z0-9#]+;`)

func normalizePlainText(raw string) string {
	text := scriptStylePattern.ReplaceAllString(raw, " ")
	text = htmlTagPattern.ReplaceAllString(text, " ")
	text = html.UnescapeString(text)
	text = entityPattern.ReplaceAllString(text, " ")
	text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}
func aiWordCount(text string) int {
	if strings.TrimSpace(text) == "" {
		return 0
	}
	return len(strings.Fields(text))
}
func clamp(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
func buildSummary(decision string, score int, risk string) string {
	return fmt.Sprintf("قرار النظام: %s، النتيجة: %d/100، مستوى خطورة AdSense: %s.", decision, score, risk)
}
func normalizeContentType(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "posts" {
		return "post"
	}
	if v == "articles" {
		return "article"
	}
	return v
}
func normalizeDecision(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case models.AIDecisionApproved, models.AIDecisionNeedsFix, models.AIDecisionRestrictedAds, models.AIDecisionRejected:
		return v
	default:
		return models.AIDecisionNeedsFix
	}
}
func normalizeRisk(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "low", "medium", "high", "critical":
		return v
	default:
		return "medium"
	}
}
func normalizeSeverity(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "low", "medium", "high", "critical":
		return v
	default:
		return "medium"
	}
}
func normalizePriority(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "low", "medium", "high":
		return v
	default:
		return "medium"
	}
}
func firstNonEmptyLocal(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func normalizeContentReference(contentID, countryCode string) (string, string, uint) {
	raw := strings.TrimSpace(contentID)
	cc := sanitizeCountryCode(countryCode)
	if strings.Contains(raw, ":") {
		parts := strings.Split(raw, ":")
		rawID := parts[len(parts)-1]
		prefix := strings.Join(parts[:len(parts)-1], ":")
		if cc == "" {
			cc = sanitizeCountryCode(prefix)
		}
		id64, _ := strconv.ParseUint(rawID, 10, 64)
		if cc == "" {
			cc = "jo"
		}
		return cc, fmt.Sprintf("%s:%d", cc, id64), uint(id64)
	}
	id64, _ := strconv.ParseUint(raw, 10, 64)
	if cc == "" {
		cc = "jo"
	}
	return cc, fmt.Sprintf("%s:%d", cc, id64), uint(id64)
}
func sanitizeCountryCode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, code := range []string{"jo", "sa", "eg", "ps"} {
		if value == code || strings.HasSuffix(value, "_"+code) || strings.HasSuffix(value, "-"+code) || strings.Contains(value, "_"+code+":") {
			return code
		}
	}
	if value == "" {
		return ""
	}
	return "jo"
}
func splitLongArabicContentLocal(content string) string {
	if strings.Contains(strings.ToLower(content), "<p") {
		return content
	}
	sentences := regexp.MustCompile(`([.!؟?])\s+`).ReplaceAllString(content, "$1</p><p>")
	if !strings.HasPrefix(strings.TrimSpace(sentences), "<p>") {
		sentences = "<p>" + sentences + "</p>"
	}
	return sentences
}

var _ = gorm.ErrRecordNotFound
