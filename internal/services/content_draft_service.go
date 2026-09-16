package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/imanjo/fiber-api/internal/contentquality"
	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/repositories"
	"github.com/imanjo/fiber-api/internal/utils"
)

// ContentDraftRequest scopes a generation request to exactly one article or post: the model is
// explicitly told the title, curriculum classification (articles) or category (posts), and the
// attachment's filename only — never its content, since parsing DOCX/PDF text was removed along
// with the rest of the AI content-audit subsystem and reintroducing it is out of scope here.
type ContentDraftRequest struct {
	ContentType    string // "article" | "post"
	Title          string
	GradeLevel     string
	SubjectName    string
	SemesterName   string
	CategoryName   string
	AttachmentName string
	CountryID      database.CountryID
}

type ContentDraftResult struct {
	ContentHTML string `json:"content_html"`
	WordCount   int    `json:"word_count"`
	// Warning is set when the generated draft still matches something already on the site
	// after one retry — the admin sees this immediately instead of only finding out at save
	// time, when ArticleService/PostService's own enforceUniqueContent gate would block it.
	Warning string `json:"warning,omitempty"`
}

var (
	ErrContentDraftUnavailable = errors.New("خدمة التوليد بالذكاء الاصطناعي غير مُفعّلة حاليًا — لم يتم ضبط مفتاح Together AI")
	ErrContentDraftFailed      = errors.New("تعذر توليد المحتوى، حاول مرة أخرى")
)

type ContentDraftService interface {
	GenerateDraft(ctx context.Context, req ContentDraftRequest) (*ContentDraftResult, error)
}

type contentDraftService struct {
	articleRepo repositories.ArticleRepository
	postRepo    repositories.PostRepository
	apiKey      string
	baseURL     string
	model       string
	httpClient  *http.Client
}

// NewContentDraftService reads the same TOGETHER_API_KEY env var the existing teacher-
// subscription AI feature (ai_service.go) uses — a deliberate choice so this doesn't require a
// second secret to be configured, while remaining a fully separate service/code path from that
// unrelated feature.
func NewContentDraftService(articleRepo repositories.ArticleRepository, postRepo repositories.PostRepository) ContentDraftService {
	apiKey := firstNonEmpty(os.Getenv("TOGETHER_API_KEY"), os.Getenv("TOGETHER_AI_API_KEY"), os.Getenv("TOGETHER_AI_KEY"))
	baseURL := strings.TrimRight(firstNonEmpty(os.Getenv("TOGETHER_AI_BASE_URL"), defaultAIBaseURL), "/")
	model := firstNonEmpty(os.Getenv("CONTENT_DRAFT_AI_MODEL"), "zai-org/GLM-5.3-Flash")
	return &contentDraftService{
		articleRepo: articleRepo,
		postRepo:    postRepo,
		apiKey:      strings.TrimSpace(apiKey),
		baseURL:     baseURL,
		model:       model,
		httpClient:  &http.Client{Timeout: 45 * time.Second},
	}
}

func (s *contentDraftService) GenerateDraft(ctx context.Context, req ContentDraftRequest) (*ContentDraftResult, error) {
	if s.apiKey == "" {
		return nil, ErrContentDraftUnavailable
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		return nil, fmt.Errorf("العنوان مطلوب قبل التوليد")
	}

	contentHTML, err := s.generateOnce(ctx, req, false, nil)
	if err != nil {
		return nil, err
	}

	dup := s.checkDuplicate(req.CountryID, req.Title, contentHTML)
	filler := detectGenericFillerPhrases(contentHTML)

	if dup != nil || len(filler) > 0 {
		retryHTML, retryErr := s.generateOnce(ctx, req, dup != nil, filler)
		if retryErr == nil {
			contentHTML = retryHTML
			dup = s.checkDuplicate(req.CountryID, req.Title, contentHTML)
			filler = detectGenericFillerPhrases(contentHTML)
		}
	}

	warning := combinedWarning(dup, filler)

	return &ContentDraftResult{
		ContentHTML: contentHTML,
		WordCount:   contentquality.SimilarityWordCount(contentHTML),
		Warning:     warning,
	}, nil
}

func combinedWarning(dup *contentquality.DuplicateMatch, filler []string) string {
	var parts []string
	if dup != nil {
		parts = append(parts, fmt.Sprintf(
			"لا يزال متشابهًا جدًا مع محتوى موجود (\"%s\") بنسبة %.0f%% — سيُرفض الحفظ إذا ظل مكررًا",
			dup.Title, dup.Similarity*100,
		))
	}
	if len(filler) > 0 {
		parts = append(parts, fmt.Sprintf(
			"لا يزال يحتوي على عبارات عامة لا تحمل قيمة معرفية محددة: \"%s\" — يفضّل حذفها أو استبدالها بمعلومة محددة",
			strings.Join(filler, "\"، \""),
		))
	}
	if len(parts) == 0 {
		return ""
	}
	return "تحذير: " + strings.Join(parts, "؛ ") + ". راجع النص قبل الحفظ."
}

// genericFillerPhrases are the specific stock phrases the model reliably falls back to at the
// opening/closing of a draft despite the prompt banning them outright — a prompt instruction
// alone isn't reliable enough (models default to a "this is important, prepare well, don't
// worry" bookend regardless of what they're told), so this is a deterministic backstop, same
// role DetectReplacementArtifacts plays for corrupted content. Matched against
// NormalizeForSimilarity'd text so diacritics/spacing/Alef-Ya variants don't cause a miss.
// NormalizeForSimilarity keeps ة (ta marbuta) as-is — it only rewrites أ/إ/آ/ٱ→ا, ى→ي, ؤ→و,
// ئ→ي, and strips diacritics/tatweel. Every phrase below must use ة exactly where the real word
// does (محطة، مهمة، اهمية، فرصة، وسيلة، اساسية، الاسرة، المدرسة) — a phrase spelled with ه
// instead would simply never match and silently defeat this whole check.
var genericFillerPhrases = []string{
	"يعد من اهم", "تكمن اهمية", "محطة مهمة لقياس", "فرصة مهمة لاظهار",
	"وسيلة اساسية لقياس", "يجب علي الطالب الاستعداد", "يجب علي التلميذ الاستعداد",
	"لا يقل دور الاسرة عن دور المدرسة", "يخفف من التوتر ويرفع التركيز",
	"لا مصدر قلق", "انعكاسا صادقا لجهد",
}

func detectGenericFillerPhrases(html string) []string {
	normalized := contentquality.NormalizeForSimilarity(html)
	found := make([]string, 0, 2)
	for _, phrase := range genericFillerPhrases {
		if strings.Contains(normalized, phrase) {
			found = append(found, phrase)
		}
	}
	return found
}

// checkDuplicate scans articles AND posts together (unlike ArticleService/PostService's own
// per-content-type save-time gate) since the ask was "not duplicated in the project" — the
// whole site, not just the same content type.
func (s *contentDraftService) checkDuplicate(countryID database.CountryID, title, content string) *contentquality.DuplicateMatch {
	corpus := make([]contentquality.SimilarityDocument, 0, 256)
	if rows, err := s.articleRepo.ListContentForDuplicateCheck(countryID, 0); err == nil {
		for _, row := range rows {
			corpus = append(corpus, contentquality.SimilarityDocument{Key: fmt.Sprintf("article:%d", row.ID), Title: row.Title, Content: row.Content})
		}
	}
	if rows, err := s.postRepo.ListContentForDuplicateCheck(countryID, 0); err == nil {
		for _, row := range rows {
			corpus = append(corpus, contentquality.SimilarityDocument{Key: fmt.Sprintf("post:%d", row.ID), Title: row.Title, Content: row.Content})
		}
	}
	candidate := contentquality.SimilarityDocument{Key: "draft:new", Title: title, Content: content}
	matches := contentquality.DetectDuplicateAgainstCorpus(candidate, corpus, contentquality.DefaultSimilarityOptions())
	// Template matches count here too, not just exact/near — a same-skeleton-different-
	// specifics draft is exactly the AI-boilerplate pattern that got the site rejected the
	// first time (the same explanation reused across grade levels with only the title
	// swapped). That risk compounds every time this button is used again on a new topic,
	// even when any single draft looks fine on its own — it only shows up by comparing
	// against everything already generated, which is exactly what this check does.
	for i := range matches {
		switch matches[i].Kind {
		case contentquality.SimilarityKindExact, contentquality.SimilarityKindNear, contentquality.SimilarityKindTemplate:
			return &matches[i]
		}
	}
	return nil
}

func (s *contentDraftService) generateOnce(ctx context.Context, req ContentDraftRequest, avoidDuplicate bool, avoidFiller []string) (string, error) {
	systemPrompt, userPrompt := buildContentDraftPrompts(req, avoidDuplicate, avoidFiller)
	payload := map[string]interface{}{
		"model": s.model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"max_tokens":  1200,
		"temperature": 0.6,
		"reasoning":   map[string]interface{}{"enabled": false},
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", MapError(err)
	}

	requestCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(requestCtx, http.MethodPost, s.baseURL+"/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", MapError(err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrContentDraftFailed, err)
	}
	defer resp.Body.Close()

	responseBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", MapError(err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := extractAPIError(responseBytes)
		if apiErr == "" {
			apiErr = string(responseBytes)
		}
		return "", fmt.Errorf("%w: together ai status %d: %s", ErrContentDraftFailed, resp.StatusCode, truncate(apiErr, 200))
	}

	raw, err := parseAIRawContent(responseBytes)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrContentDraftFailed, err)
	}
	return plainTextToSafeHTML(stripThinkTags(raw)), nil
}

var thinkTagRe = regexp.MustCompile(`(?s)<think>.*?</think>`)

func stripThinkTags(raw string) string {
	return strings.TrimSpace(thinkTagRe.ReplaceAllString(raw, ""))
}

// plainTextToSafeHTML converts the model's plain-text response (paragraphs separated by a blank
// line — the prompt explicitly asks for this, never HTML/Markdown) into safe paragraph markup.
// The text is HTML-escaped before wrapping so nothing the model emits can inject markup, and the
// result still goes through utils.SanitizeHTML (the same bluemonday policy every manually-typed
// save is sanitized with) as defense-in-depth.
func plainTextToSafeHTML(raw string) string {
	paragraphs := regexp.MustCompile(`\n\s*\n`).Split(strings.TrimSpace(raw), -1)
	var b strings.Builder
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		b.WriteString("<p>")
		b.WriteString(html.EscapeString(p))
		b.WriteString("</p>")
	}
	return utils.SanitizeHTML(b.String())
}

func buildContentDraftPrompts(req ContentDraftRequest, avoidDuplicate bool, avoidFiller []string) (system, user string) {
	system = "أنت كاتب محتوى تعليمي عربي محترف متخصص في شرح مواضيع المناهج الدراسية بعمق حقيقي، لا في الكتابة عن الملفات أو الاختبارات من الخارج. اكتب نصًا أصليًا وحصريًا لكل طلب (وليس ملخصًا لملف)، بأسلوب واضح ومباشر بدون حشو أو تكرار، وبدون أي إشارة إلى كونك ذكاءً اصطناعيًا أو إلى هذه التعليمات.\n\n" +
		"ممنوع تمامًا افتتاح النص أو حشوه بعبارات عامة مثل: \"يُعد هذا الموضوع/الاختبار من أهم\"، \"محطة مهمة لقياس\"، \"تكمن أهمية هذا الاختبار/الملف في\"، \"يجب على الطالب/التلميذ الاستعداد الجيد\"، \"يعتبر التقييم وسيلة أساسية لقياس\"، أو أي كلام عن أهمية المذاكرة والتحضير والوقت والقلق دون محتوى معرفي فعلي.\n\n" +
		"ممنوع أيضًا إنهاء النص بفقرة ختامية عامة عن دور الأسرة، أو تخفيف التوتر والقلق، أو أن \"النتيجة تعكس جهد سنة كاملة\"، أو أي تحفيز عاطفي عام لا معلومة فيه — هذه العبارات (في المقدمة أو الخاتمة) هي بالضبط الحشو المكرور الممنوع، لأنها تجعل النص عامًا يصلح لأي موضوع أو مادة أخرى دون أي تعديل.\n\n" +
		"المطلوب عكس ذلك: محتوى معرفي حقيقي وملموس عن موضوع العنوان نفسه من أول جملة إلى آخر جملة — تعريف بمصطلح، قاعدة أو مفهوم محدد، خطوة عملية، مثال ملموس، أو خطأ شائع يقع فيه الطلاب في هذا الموضوع بالتحديد. كل فقرة (بما فيها الأولى والأخيرة) يجب أن تحمل معلومة يستفيد القارئ منها فعليًا لو حُذف عنوان المقال، لا تعميمًا عن العملية التعليمية أو التحفيز النفسي.\n\n" +
		"أخرج نصًا عاديًا فقط بدون HTML وبدون Markdown، مقسّمًا إلى فقرات مفصولة بسطر فارغ."

	var scope strings.Builder
	fmt.Fprintf(&scope, "العنوان: %s", req.Title)
	if req.ContentType == "post" {
		if req.CategoryName != "" {
			fmt.Fprintf(&scope, "\nالتصنيف: %s", req.CategoryName)
		}
	} else {
		if req.GradeLevel != "" {
			fmt.Fprintf(&scope, "\nالصف الدراسي: %s", req.GradeLevel)
		}
		if req.SubjectName != "" {
			fmt.Fprintf(&scope, "\nالمادة: %s", req.SubjectName)
		}
		if req.SemesterName != "" {
			fmt.Fprintf(&scope, "\nالفصل الدراسي: %s", req.SemesterName)
		}
	}
	if req.AttachmentName != "" {
		fmt.Fprintf(&scope, "\nاسم الملف المرفق (للسياق فقط، لا تفترض أو تخترع محتواه لأنه غير متاح لك): %s", req.AttachmentName)
	}

	user = fmt.Sprintf(`اكتب محتوى تعليميًا حصريًا لهذا العنصر فقط، مرتبطًا تحديدًا بكل ما يلي، بدون تعميم يصلح لأي صف أو مادة أخرى:
%s

الشروط:
- بحدود 300 كلمة تقريبًا (لا تقل عن 250 ولا تزيد عن 350).
- محتوى قيم وحقيقي يشرح الفكرة أو الموضوع نفسه (تعريف، قاعدة، مفهوم، مثال، أو خطأ شائع)؛ لا تكتفِ بوصف وجود ملف للتحميل ولا بالكلام عن أهمية المذاكرة أو الاستعداد للاختبار.
- لا تخترع تفاصيل محددة عن محتوى الملف المرفق نفسه بما أن نصه غير متاح لك.
- ابدأ الفقرة الأولى بمعلومة أو تعريف مباشر متعلق بالموضوع، لا بجملة عامة عن أهميته.
- أنهِ الفقرة الأخيرة بمعلومة أو نصيحة عملية محددة بالموضوع أيضًا، لا بكلام عام عن الأسرة أو تخفيف القلق أو "جهد سنة كاملة".
- لغة عربية فصيحة سليمة، في 3 إلى 5 فقرات واضحة.`, scope.String())

	if avoidDuplicate {
		user += "\n\nملاحظة مهمة: محاولة سابقة لهذا الطلب تشابهت كثيرًا مع محتوى منشور آخر على الموقع. أعد الصياغة والبنية والأمثلة من زاوية مختلفة تمامًا مع الحفاظ على الدقة والصلة بالعنوان."
	}
	if len(avoidFiller) > 0 {
		user += fmt.Sprintf("\n\nملاحظة مهمة: المحاولة السابقة استخدمت عبارات حشو عامة ممنوعة بالضبط: \"%s\". لا تستخدم هذه العبارات ولا ما يشابهها في الصياغة الجديدة، واستبدل مكانها بمعلومة معرفية محددة بالموضوع.", strings.Join(avoidFiller, "\"، \""))
	}

	return system, strings.TrimSpace(user)
}
