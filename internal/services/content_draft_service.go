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
	// SEO carries AI-suggested metadata for the ImanSEO panel, tied specifically to this
	// title and this generated content (never invented independently) — nil only when every
	// SEO-generation attempt failed outright; ContentHTML above is still perfectly usable on
	// its own in that case.
	SEO *ContentDraftSEO `json:"seo,omitempty"`
	// SEOWarning is set when the best-scoring SEO attempt still fell short of
	// contentDraftSEOMinScore after exhausting retries, so the admin knows to double-check the
	// SEO panel manually rather than assume it already clears the bar.
	SEOWarning string `json:"seo_warning,omitempty"`
}

// ContentDraftSEO mirrors the subset of the seo_metadata fields (internal/models.SEOMetadata)
// that AI can meaningfully suggest from just a title and finished content — robots directives,
// canonical URL, and schema JSON-LD stay manual editorial decisions and aren't included here.
type ContentDraftSEO struct {
	SEOTitle           string `json:"seo_title"`
	MetaDescription    string `json:"meta_description"`
	FocusKeyword       string `json:"focus_keyword"`
	AdditionalKeywords string `json:"additional_keywords"`
	OGTitle            string `json:"og_title"`
	OGDescription      string `json:"og_description"`
	TwitterTitle       string `json:"twitter_title"`
	TwitterDescription string `json:"twitter_description"`
	SchemaType         string `json:"schema_type"`
	// Score is the same AnalyzeSEO (internal/services/seo_analyzer.go) result the manual
	// "تحليل الآن" button in ImanSeoPanel produces — not a separate, looser AI self-rating.
	Score int `json:"score"`
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
	models      []string
	httpClient  *http.Client
}

// defaultContentDraftModels is tried in order across retry attempts when no override is
// configured — zai-org/GLM-5.3-Flash first (the one this feature was built and tuned against),
// then a spread of other fast Together AI models as fallbacks so a slow or momentarily
// unavailable primary model doesn't sink the whole request.
var defaultContentDraftModels = []string{
	"zai-org/GLM-5.3-Flash",
	"Qwen/Qwen3.8-Flash",
	"openai/gpt-oss-120b",
	"Qwen/Qwen3.5-9B",
}

// NewContentDraftService reads the same TOGETHER_API_KEY env var the existing teacher-
// subscription AI feature (ai_service.go) uses — a deliberate choice so this doesn't require a
// second secret to be configured, while remaining a fully separate service/code path from that
// unrelated feature.
func NewContentDraftService(articleRepo repositories.ArticleRepository, postRepo repositories.PostRepository) ContentDraftService {
	apiKey := firstNonEmpty(os.Getenv("TOGETHER_API_KEY"), os.Getenv("TOGETHER_AI_API_KEY"), os.Getenv("TOGETHER_AI_KEY"))
	baseURL := strings.TrimRight(firstNonEmpty(os.Getenv("TOGETHER_AI_BASE_URL"), defaultAIBaseURL), "/")
	models := parseModelList(os.Getenv("CONTENT_DRAFT_AI_MODELS"))
	if len(models) == 0 {
		if single := strings.TrimSpace(os.Getenv("CONTENT_DRAFT_AI_MODEL")); single != "" {
			models = []string{single}
		} else {
			models = defaultContentDraftModels
		}
	}
	return &contentDraftService{
		articleRepo: articleRepo,
		postRepo:    postRepo,
		apiKey:      strings.TrimSpace(apiKey),
		baseURL:     baseURL,
		models:      models,
		httpClient:  &http.Client{Timeout: 40 * time.Second},
	}
}

// contentDraftMinAttempts/contentDraftMaxAttempts bound total Together AI calls per request —
// at least 3 so the duplicate/filler retry logic still gets its retries even with a single
// configured model, at most 4 so a longer CONTENT_DRAFT_AI_MODELS list can't turn one click into
// an unbounded chain of 30s+ calls. Each attempt cycles to the next model in the list (wrapping
// around), so a slow or failing model on attempt N doesn't get retried with itself on attempt
// N+1 — it moves on to a different provider/model instead.
//
// contentDraftMinWords is the floor below which a response is unusable rather than just
// imperfect (a cut-off half-sentence, not a short-but-complete draft) — kept deliberately well
// below the ~500-word prompt target (buildContentDraftPrompts) and below the analyzer's >=450
// "good" content-length cutoff (seo_analyzer.go) on purpose: models reliably undershoot a
// requested word count by a wide margin, and rejecting every complete draft that lands under
// 450 words exhausted every attempt in practice and turned this into a hard failure for the
// admin instead of a usable-but-imperfect draft. A shorter accepted draft simply scores lower
// on the SEO content-length check, surfaced honestly via SEOWarning — that's a much better
// outcome than "تعذر توليد المحتوى" every time.
//
// contentDraftSEOMaxAttempts/contentDraftSEOMinScore bound the separate SEO-metadata
// generation pass that runs once the content itself is finalized: up to 3 attempts, each
// re-scored with the same AnalyzeSEO the manual "تحليل الآن" button uses, targeting (but not
// guaranteeing — see above) the 85% floor the admin asked this feature to aim for.
const (
	contentDraftMinAttempts = 3
	contentDraftMaxAttempts = 4
	contentDraftMinWords    = 180

	contentDraftSEOMaxAttempts = 3
	contentDraftSEOMinScore    = 85
)

func (s *contentDraftService) GenerateDraft(ctx context.Context, req ContentDraftRequest) (*ContentDraftResult, error) {
	if s.apiKey == "" {
		return nil, ErrContentDraftUnavailable
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		return nil, fmt.Errorf("العنوان مطلوب قبل التوليد")
	}

	var (
		contentHTML    string
		dup            *contentquality.DuplicateMatch
		filler         []string
		avoidDuplicate bool
		avoidFiller    []string
		lastErr        error
	)

	attempts := len(s.models)
	if attempts < contentDraftMinAttempts {
		attempts = contentDraftMinAttempts
	}
	if attempts > contentDraftMaxAttempts {
		attempts = contentDraftMaxAttempts
	}

	for attempt := 0; attempt < attempts; attempt++ {
		model := s.models[attempt%len(s.models)]
		html, truncated, err := s.generateOnce(ctx, model, req, avoidDuplicate, avoidFiller)
		if err != nil {
			lastErr = err
			continue
		}
		wordCount := contentquality.SimilarityWordCount(html)
		if truncated || wordCount < contentDraftMinWords {
			// Unusable — a cut-off half-sentence isn't a draft worth showing, and isn't worth
			// checking for duplication/filler either. Try again from a clean prompt (dropping
			// any pending correction notes, since those aren't why this attempt failed). The two
			// cases get different messages: `truncated` is the provider's own finish_reason
			// signal (an actual mid-sentence cutoff), while landing under contentDraftMinWords
			// with a complete response is a different, much rarer problem — conflating them
			// previously made every short-but-legitimate draft look like a cutoff bug.
			if truncated {
				lastErr = fmt.Errorf("%w: انقطع الرد قبل اكتماله (%d كلمة فقط)", ErrContentDraftFailed, wordCount)
			} else {
				lastErr = fmt.Errorf("%w: الرد قصير جدًا وغير كافٍ (%d كلمة فقط)", ErrContentDraftFailed, wordCount)
			}
			avoidDuplicate, avoidFiller = false, nil
			continue
		}

		contentHTML = html
		dup = s.checkDuplicate(req.CountryID, req.Title, contentHTML)
		filler = detectGenericFillerPhrases(contentHTML)
		lastErr = nil
		if dup == nil && len(filler) == 0 {
			break
		}
		avoidDuplicate, avoidFiller = dup != nil, filler
	}

	if contentHTML == "" {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, ErrContentDraftFailed
	}

	result := &ContentDraftResult{
		ContentHTML: contentHTML,
		WordCount:   contentquality.SimilarityWordCount(contentHTML),
		Warning:     combinedWarning(dup, filler),
	}
	if seo, seoWarning := s.generateSEODraft(ctx, req, contentHTML); seo != nil {
		result.SEO = seo
		result.SEOWarning = seoWarning
	}
	return result, nil
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

// generateOnce returns (html, truncated, err). truncated is true when the provider cut the
// response off before it finished (finish_reason "length") — reported separately from err
// because the HTTP call itself succeeded; the caller decides whether a truncated response is
// worth retrying rather than treating it as a hard failure.
func (s *contentDraftService) generateOnce(ctx context.Context, model string, req ContentDraftRequest, avoidDuplicate bool, avoidFiller []string) (contentHTML string, truncated bool, err error) {
	systemPrompt, userPrompt := buildContentDraftPrompts(req, avoidDuplicate, avoidFiller)
	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		// Generous headroom above the ~300-word (roughly 450-600 token) target: if the model
		// emits any reasoning/thinking tokens before the visible answer despite
		// reasoning.enabled=false (provider-dependent, not guaranteed to be fully honored),
		// those count against this same budget — a tight limit here is exactly what produced
		// the reported bug, a response cut off mid-sentence because the visible answer never
		// got to finish before hitting max_tokens.
		"max_tokens":  2200,
		"temperature": 0.6,
		"reasoning":   map[string]interface{}{"enabled": false},
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", false, MapError(err)
	}

	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(requestCtx, http.MethodPost, s.baseURL+"/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", false, MapError(err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return "", false, fmt.Errorf("%w: %v", ErrContentDraftFailed, err)
	}
	defer resp.Body.Close()

	responseBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false, MapError(err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := extractAPIError(responseBytes)
		if apiErr == "" {
			apiErr = string(responseBytes)
		}
		return "", false, fmt.Errorf("%w: together ai status %d: %s", ErrContentDraftFailed, resp.StatusCode, truncate(apiErr, 200))
	}

	raw, wasTruncated, err := parseContentDraftResponse(responseBytes)
	if err != nil {
		return "", false, fmt.Errorf("%w: %v", ErrContentDraftFailed, err)
	}
	withHeading := insertSEOHeading(stripThinkTags(raw), req.Title)
	return plainTextToSafeHTML(withHeading), wasTruncated, nil
}

// parseContentDraftResponse is a local, minimal parser (rather than reusing ai_service.go's
// parseAIRawContent) specifically so finish_reason is available — that field is the direct,
// authoritative truncation signal Together AI's OpenAI-compatible API reports, instead of only
// inferring truncation indirectly from word count after the fact.
func parseContentDraftResponse(bodyBytes []byte) (content string, truncated bool, err error) {
	var data struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(bodyBytes, &data); err != nil {
		return "", false, MapError(err)
	}
	if len(data.Choices) == 0 {
		return "", false, errors.New("no content generated")
	}
	content = strings.TrimSpace(data.Choices[0].Message.Content)
	if content == "" {
		return "", false, errors.New("empty content generated")
	}
	return content, data.Choices[0].FinishReason == "length", nil
}

var thinkTagRe = regexp.MustCompile(`(?s)<think>.*?</think>`)

func stripThinkTags(raw string) string {
	return strings.TrimSpace(thinkTagRe.ReplaceAllString(raw, ""))
}

// insertSEOHeading inserts one deterministic H2 subheading before the second paragraph (the
// rule/example section, per buildContentDraftPrompts' 5-part structure) — done in code, not
// asked of the model, so the SEO analyzer's headings check (internal/services/seo_analyzer.go)
// always finds one regardless of whether the model honors yet another formatting instruction on
// top of everything else it's already asked to follow. plainTextToSafeHTML below renders the
// "## " marker this produces as an actual <h2>.
func insertSEOHeading(raw, title string) string {
	paragraphs := regexp.MustCompile(`\n\s*\n`).Split(strings.TrimSpace(raw), -1)
	if len(paragraphs) < 2 {
		return raw
	}
	heading := "## شرح " + strings.TrimSpace(title)
	out := make([]string, 0, len(paragraphs)+1)
	out = append(out, paragraphs[0], heading)
	out = append(out, paragraphs[1:]...)
	return strings.Join(out, "\n\n")
}

// plainTextToSafeHTML converts the model's plain-text response (paragraphs separated by a blank
// line — the prompt explicitly asks for this, never HTML/Markdown) into safe markup: a paragraph
// starting with "## " (only ever produced by insertSEOHeading above) becomes an <h2>, everything
// else becomes a <p>. Text is HTML-escaped before wrapping so nothing the model emits can inject
// markup, and the result still goes through utils.SanitizeHTML (the same bluemonday policy every
// manually-typed save is sanitized with) as defense-in-depth.
func plainTextToSafeHTML(raw string) string {
	paragraphs := regexp.MustCompile(`\n\s*\n`).Split(strings.TrimSpace(raw), -1)
	var b strings.Builder
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if heading, ok := strings.CutPrefix(p, "## "); ok {
			b.WriteString("<h2>")
			b.WriteString(html.EscapeString(strings.TrimSpace(heading)))
			b.WriteString("</h2>")
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
		"البنية المثالية التي يجب اتباعها (وهي النمط الذي أعطى أفضل النتائج فعليًا): افتح بجملة تعريف أو حقيقة مباشرة عن الموضوع، ثم اشرح قاعدة أو مفهومًا أساسيًا واحدًا بدقة، ثم قدّم مثالًا ملموسًا يوضّح هذا المفهوم عمليًا — إن كان الموضوع يحتمل خطأ شائعًا يقع فيه الطلاب (كقاعدة نحوية أو رياضية أو علمية) فاعرضه كزوج خطأ ✗ مقابل الصواب ✓ محدد (مثل \"She don't like\" ✗ و \"She doesn't like\" ✓)، وإن لم يحتمل الموضوع ذلك (كموضوع خبري أو تنظيمي عام) فاستبدله بمثال أو تطبيق عملي مباشر من واقع الموضوع نفسه، ثم فقرة عن استراتيجية عملية محددة للتعامل مع هذا الموضوع (خطوة دراسة، طريقة حل، أو أسلوب تحقق)، وأخيرًا فقرة ختامية تنتهي بنصيحة عملية مرتبطة تحديدًا بقاعدة أو مهارة من الموضوع نفسه — لا بكلام عام عن الأسرة أو القلق أو جهد السنة.\n\n" +
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
- بحدود 500 كلمة تقريبًا (لا تقل عن 450 ولا تزيد عن 600) — هذا الطول ضروري لعمق الشرح الحقيقي وليس لملء المساحة، فوسّع كل نقطة بمعلومة أو مثال إضافي حقيقي بدل إعادة صياغة الجملة نفسها.
- اتبع هذه البنية بالترتيب: (1) فقرة تعريف أو حقيقة مباشرة عن الموضوع، (2) فقرة تشرح قاعدة أو مفهومًا أساسيًا واحدًا بدقة، (3) فقرة تعرض مثالًا ملموسًا من صميم الموضوع نفسه — زوج خطأ شائع ✗ مقابل الصواب ✓ إن كان الموضوع يحتمل ذلك (قاعدة نحوية أو رياضية أو علمية)، أو مثالًا تطبيقيًا مباشرًا إن كان الموضوع خبريًا أو تنظيميًا لا يحتمل صيغة الخطأ والصواب، (4) فقرة عن استراتيجية عملية محددة (خطوة دراسة أو طريقة حل أو تحقق)، (5) فقرة ختامية بنصيحة عملية مرتبطة بقاعدة أو مهارة من الموضوع نفسه.
- محتوى قيم وحقيقي يشرح الفكرة أو الموضوع نفسه؛ لا تكتفِ بوصف وجود ملف للتحميل ولا بالكلام عن أهمية المذاكرة أو الاستعداد للاختبار.
- لا تخترع تفاصيل محددة عن محتوى الملف المرفق نفسه بما أن نصه غير متاح لك.
- ابدأ الفقرة الأولى بمعلومة أو تعريف مباشر متعلق بالموضوع، لا بجملة عامة عن أهميته.
- أنهِ الفقرة الأخيرة بمعلومة أو نصيحة عملية محددة بالموضوع أيضًا، لا بكلام عام عن الأسرة أو تخفيف القلق أو "جهد سنة كاملة".
- لغة عربية فصيحة سليمة، في 5 فقرات واضحة بحسب البنية أعلاه.`, scope.String())

	if avoidDuplicate {
		user += "\n\nملاحظة مهمة: محاولة سابقة لهذا الطلب تشابهت كثيرًا مع محتوى منشور آخر على الموقع. أعد الصياغة والبنية والأمثلة من زاوية مختلفة تمامًا مع الحفاظ على الدقة والصلة بالعنوان."
	}
	if len(avoidFiller) > 0 {
		user += fmt.Sprintf("\n\nملاحظة مهمة: المحاولة السابقة استخدمت عبارات حشو عامة ممنوعة بالضبط: \"%s\". لا تستخدم هذه العبارات ولا ما يشابهها في الصياغة الجديدة، واستبدل مكانها بمعلومة معرفية محددة بالموضوع.", strings.Join(avoidFiller, "\"، \""))
	}

	return system, strings.TrimSpace(user)
}

// generateSEODraft asks the model for SEO metadata scoped to exactly this title and this
// finished content (never a separate, invented topic), then scores the combination with the
// same AnalyzeSEO (seo_analyzer.go) the manual "تحليل الآن" button in ImanSeoPanel uses — so
// what this returns is held to the identical bar an admin would see, not a separate, looser
// self-rating. Each retry feeds back exactly which checks failed so the next attempt targets
// the real gap instead of guessing again from scratch. Returns (nil, "") only if every attempt
// failed outright (API errors) — the caller still has a perfectly usable ContentHTML in that
// case, so this never blocks the overall draft.
func (s *contentDraftService) generateSEODraft(ctx context.Context, req ContentDraftRequest, contentHTML string) (*ContentDraftSEO, string) {
	schemaType := "Article"
	if req.ContentType == "post" {
		schemaType = "BlogPosting"
	}

	// The same first-350-characters window AnalyzeSEO's keyword_intro check reads (see
	// seo_analyzer.go) — handing the model that exact excerpt and asking it to pick a focus
	// keyword that already appears in it verbatim all but guarantees that check passes.
	introExcerpt := seoPlainText(contentHTML)
	if rs := []rune(introExcerpt); len(rs) > 350 {
		introExcerpt = string(rs[:350])
	}

	var (
		best      *ContentDraftSEO
		bestScore = -1
		feedback  string
	)

	for attempt := 0; attempt < contentDraftSEOMaxAttempts; attempt++ {
		model := s.models[attempt%len(s.models)]
		seo, truncated, err := s.generateSEOOnce(ctx, model, req, introExcerpt, feedback)
		if err != nil || truncated || seo == nil {
			continue
		}
		seo.SchemaType = schemaType
		analysis := AnalyzeSEO(SEOAnalysisInput{
			Title:           seo.SEOTitle,
			Content:         contentHTML,
			MetaDescription: seo.MetaDescription,
			FocusKeyword:    seo.FocusKeyword,
			SchemaType:      schemaType,
		})
		seo.Score = analysis.Score
		if analysis.Score > bestScore {
			best, bestScore = seo, analysis.Score
		}
		if analysis.Score >= contentDraftSEOMinScore {
			return seo, ""
		}
		feedback = seoAnalysisFeedback(analysis)
	}

	if best == nil {
		return nil, ""
	}
	return best, fmt.Sprintf(
		"تحذير: أفضل تحليل SEO تم الوصول إليه %d%% (الهدف %d%%) — راجع حقول SEO يدويًا قبل النشر.",
		bestScore, contentDraftSEOMinScore,
	)
}

// seoAnalysisFeedback turns every non-"good" AnalyzeSEO check into a short Arabic correction
// note fed back into the next SEO-generation attempt's prompt.
func seoAnalysisFeedback(analysis SEOAnalysisResult) string {
	parts := make([]string, 0, len(analysis.Checks))
	for _, check := range analysis.Checks {
		if check.Status == "good" {
			continue
		}
		if check.Recommendation != "" {
			parts = append(parts, check.Message+" — "+check.Recommendation)
		} else {
			parts = append(parts, check.Message)
		}
	}
	return strings.Join(parts, "؛ ")
}

// generateSEOOnce makes one Together AI call asking strictly for a JSON object of SEO fields
// (small output, low truncation risk compared to the ~500-word content call) and fills in any
// og_*/twitter_* fields the model left blank from seo_title/meta_description — those are
// legitimate mirrors, not invented content.
func (s *contentDraftService) generateSEOOnce(ctx context.Context, model string, req ContentDraftRequest, introExcerpt, feedback string) (*ContentDraftSEO, bool, error) {
	systemPrompt, userPrompt := buildSEODraftPrompts(req, introExcerpt, feedback)
	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"max_tokens":      700,
		"temperature":     0.4,
		"reasoning":       map[string]interface{}{"enabled": false},
		"response_format": map[string]interface{}{"type": "json_object"},
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, false, MapError(err)
	}

	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(requestCtx, http.MethodPost, s.baseURL+"/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, false, MapError(err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrContentDraftFailed, err)
	}
	defer resp.Body.Close()

	responseBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, MapError(err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, fmt.Errorf("%w: together ai status %d", ErrContentDraftFailed, resp.StatusCode)
	}

	raw, wasTruncated, err := parseContentDraftResponse(responseBytes)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrContentDraftFailed, err)
	}
	if wasTruncated {
		return nil, true, nil
	}

	var parsed struct {
		SEOTitle           string `json:"seo_title"`
		MetaDescription    string `json:"meta_description"`
		FocusKeyword       string `json:"focus_keyword"`
		AdditionalKeywords string `json:"additional_keywords"`
		OGTitle            string `json:"og_title"`
		OGDescription      string `json:"og_description"`
		TwitterTitle       string `json:"twitter_title"`
		TwitterDescription string `json:"twitter_description"`
	}
	clean := cleanJSONPayload(stripThinkTags(raw))
	if err := json.Unmarshal([]byte(clean), &parsed); err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrContentDraftFailed, err)
	}

	seo := &ContentDraftSEO{
		SEOTitle:           strings.TrimSpace(parsed.SEOTitle),
		MetaDescription:    strings.TrimSpace(parsed.MetaDescription),
		FocusKeyword:       strings.TrimSpace(parsed.FocusKeyword),
		AdditionalKeywords: strings.TrimSpace(parsed.AdditionalKeywords),
		OGTitle:            strings.TrimSpace(parsed.OGTitle),
		OGDescription:      strings.TrimSpace(parsed.OGDescription),
		TwitterTitle:       strings.TrimSpace(parsed.TwitterTitle),
		TwitterDescription: strings.TrimSpace(parsed.TwitterDescription),
	}
	if seo.MetaDescription == "" || seo.FocusKeyword == "" {
		return nil, false, fmt.Errorf("%w: استجابة SEO ناقصة الحقول", ErrContentDraftFailed)
	}
	if seo.SEOTitle == "" {
		seo.SEOTitle = req.Title
	}
	if seo.OGTitle == "" {
		seo.OGTitle = seo.SEOTitle
	}
	if seo.OGDescription == "" {
		seo.OGDescription = seo.MetaDescription
	}
	if seo.TwitterTitle == "" {
		seo.TwitterTitle = seo.SEOTitle
	}
	if seo.TwitterDescription == "" {
		seo.TwitterDescription = seo.MetaDescription
	}
	return seo, false, nil
}

func buildSEODraftPrompts(req ContentDraftRequest, introExcerpt, feedback string) (system, user string) {
	system = "أنت متخصص SEO عربي محترف. مهمتك اقتراح بيانات SEO لعنصر تعليمي واحد بالاعتماد فقط على عنوانه ومقدمة محتواه الفعلي المعطى لك — لا تخترع موضوعًا مختلفًا ولا تنقل بيانات تصلح لأي صفحة أخرى. أعد الرد بصيغة JSON صالحة فقط دون أي تنسيق Markdown، وبلا أي شرح خارج كائن JSON نفسه."

	user = fmt.Sprintf(`العنوان: %s
مقدمة المحتوى الفعلي (أول ~350 حرفًا منه، هذا كل ما تحتاجه لاختيار العبارة المفتاحية): %s

أعد كائن JSON بهذه المفاتيح بالضبط:
{
  "seo_title": "عنوان SEO بين 35 و65 حرفًا، طبيعي ومقروء، يحتوي حرفيًا على العبارة المفتاحية التي تختارها",
  "meta_description": "وصف تعريفي بين 110 و165 حرفًا، يحتوي حرفيًا على نفس العبارة المفتاحية مرة واحدة، ويلخص فائدة المحتوى فعليًا لا وصفًا عامًا",
  "focus_keyword": "عبارة من 2 إلى 4 كلمات تظهر حرفيًا داخل نص المقدمة أعلاه — اخترها من النص نفسه، لا عبارة غير موجودة فيه",
  "additional_keywords": "3 إلى 6 كلمات أو عبارات مرتبطة بالموضوع، مفصولة بفواصل",
  "og_title": "نفس عنوان SEO أو صياغة مقاربة له لمشاركة السوشيال ميديا",
  "og_description": "نفس الوصف التعريفي أو صياغة مقاربة له",
  "twitter_title": "نفس عنوان SEO أو صياغة مقاربة",
  "twitter_description": "نفس الوصف التعريفي أو صياغة مقاربة"
}

شروط صارمة:
- العبارة المفتاحية focus_keyword يجب أن تظهر حرفيًا في seo_title وفي meta_description وفي نص المقدمة المعطى أعلاه.
- لا تكرر العبارة المفتاحية أكثر من مرة واحدة داخل seo_title وأكثر من مرة واحدة داخل meta_description.
- لا تخترع تفاصيل غير موجودة في العنوان أو في نص المقدمة.
- أعد فقط كائن JSON صالح دون أي نص قبله أو بعده ودون تنسيق Markdown.`, req.Title, introExcerpt)

	if feedback != "" {
		user += fmt.Sprintf("\n\nملاحظة من تحليل SEO لمحاولة سابقة، صحّح هذه النقاط بالتحديد قبل أي شيء آخر: %s", feedback)
	}
	return system, user
}
