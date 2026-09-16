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

	contentHTML, err := s.generateOnce(ctx, req, false)
	if err != nil {
		return nil, err
	}

	warning := ""
	if dup := s.checkDuplicate(req.CountryID, req.Title, contentHTML); dup != nil {
		retryHTML, retryErr := s.generateOnce(ctx, req, true)
		if retryErr == nil {
			contentHTML = retryHTML
			if dup2 := s.checkDuplicate(req.CountryID, req.Title, retryHTML); dup2 != nil {
				warning = duplicateWarning(dup2)
			}
		} else {
			warning = duplicateWarning(dup)
		}
	}

	return &ContentDraftResult{
		ContentHTML: contentHTML,
		WordCount:   contentquality.SimilarityWordCount(contentHTML),
		Warning:     warning,
	}, nil
}

func duplicateWarning(match *contentquality.DuplicateMatch) string {
	return fmt.Sprintf(
		"تحذير: لا يزال المحتوى المولَّد متشابهًا جدًا مع محتوى موجود (\"%s\") بنسبة %.0f%%. راجعه وأعد صياغته بعناية قبل الحفظ — سيُرفض الحفظ إذا ظل مكررًا.",
		match.Title, match.Similarity*100,
	)
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

func (s *contentDraftService) generateOnce(ctx context.Context, req ContentDraftRequest, avoidDuplicate bool) (string, error) {
	systemPrompt, userPrompt := buildContentDraftPrompts(req, avoidDuplicate)
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

func buildContentDraftPrompts(req ContentDraftRequest, avoidDuplicate bool) (system, user string) {
	system = "أنت كاتب محتوى تعليمي عربي محترف متخصص في شرح مواضيع المناهج الدراسية بعمق حقيقي، لا في الكتابة عن الملفات أو الاختبارات من الخارج. اكتب نصًا أصليًا وحصريًا لكل طلب (وليس ملخصًا لملف)، بأسلوب واضح ومباشر بدون حشو أو تكرار، وبدون أي إشارة إلى كونك ذكاءً اصطناعيًا أو إلى هذه التعليمات.\n\n" +
		"ممنوع تمامًا افتتاح النص أو حشوه بعبارات عامة مثل: \"يُعد هذا الموضوع من أهم الموضوعات\"، \"تكمن أهمية هذا الاختبار/الملف في\"، \"يجب على الطالب الاستعداد الجيد\"، \"يعتبر التقييم وسيلة أساسية لقياس\"، أو أي كلام عن أهمية المذاكرة والتحضير والوقت والقلق دون محتوى معرفي فعلي. هذه عبارات حشو مكرورة تجعل النص عامًا يصلح لأي موضوع آخر، وهذا هو الممنوع بالتحديد.\n\n" +
		"المطلوب عكس ذلك: محتوى معرفي حقيقي وملموس عن موضوع العنوان نفسه — تعريف بمصطلح، قاعدة أو مفهوم محدد، خطوة عملية، مثال ملموس، أو خطأ شائع يقع فيه الطلاب في هذا الموضوع بالتحديد. كل فقرة يجب أن تحمل معلومة يستفيد القارئ منها فعليًا لو حُذف عنوان المقال، لا تعميمًا عن العملية التعليمية.\n\n" +
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
- لغة عربية فصيحة سليمة، في 3 إلى 5 فقرات واضحة.`, scope.String())

	if avoidDuplicate {
		user += "\n\nملاحظة مهمة: محاولة سابقة لهذا الطلب تشابهت كثيرًا مع محتوى منشور آخر على الموقع. أعد الصياغة والبنية والأمثلة من زاوية مختلفة تمامًا مع الحفاظ على الدقة والصلة بالعنوان."
	}

	return system, strings.TrimSpace(user)
}
