// This file adds one capability to contentDraftService (defined in content_draft_service.go):
// fixing an ALREADY-PUBLISHED article or post's specific, currently-detected AdSense
// content-policy problem — thin/medium content, near-duplicate content, generic AI-filler
// boilerplate, corrupted unresolved-template artifacts, a too-short title, or a missing/short
// meta description — the same problems the adsense-policy dashboard page
// (internal/handlers/adsensepolicy) surfaces from EvaluateDiagnostics/DetectSimilarity/
// DetectGenericFillerPhrases/DetectReplacementArtifacts. It reuses that exact scoring so the fix
// is judged by the same bar the dashboard warning came from, reuses generateSEODraft unchanged
// for the title/meta-description pass (only run when the title or meta description was itself
// flagged, not on every content fix — see FixPolicyContent), and — like GenerateDraft — never
// writes to the database itself: the result is always a reviewable draft the admin applies from
// inside the normal article/post edit page before clicking the ordinary Save button.
//
// fixContentWithRetries also runs a direct self-similarity check (contentquality.
// JaccardSimilarity against the original content) on every attempt — none of the other checks
// (word count, duplicate-against-corpus, filler phrases, artifacts) catch a model that just
// echoes the original text back almost verbatim, which happens because the prompt explicitly
// tells it to preserve correct existing information and it sometimes takes that too literally.
package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/imanjo/fiber-api/internal/contentquality"
	"github.com/imanjo/fiber-api/internal/database"
)

// PolicyFixRequest identifies exactly one already-published article or post to fix. Unlike
// ContentDraftRequest (which scopes a from-scratch generation), the fix service loads the
// current title/content/meta description/classification itself and re-derives which problems
// still apply right now — the client only ever needs to say which item to fix, never which
// problem, so a scan result that's gone stale (content edited since the last scan) can't cause a
// wrong fix to be applied.
type PolicyFixRequest struct {
	ContentType string // "article" | "post"
	ID          uint64
	CountryID   database.CountryID
}

// PolicyFixResult is always a draft for human review — ContentHTML/Title/MetaDescription are
// never saved by this service. IssuesAddressed lists, in Arabic, exactly which of the detected
// problems this draft targets, so the admin can see the fix matches the warning they clicked.
type PolicyFixResult struct {
	Title           string   `json:"title"`
	ContentHTML     string   `json:"content_html"`
	MetaDescription string   `json:"meta_description"`
	WordCount       int      `json:"word_count"`
	IssuesAddressed []string `json:"issues_addressed"`
	// SEOScore is the same AnalyzeSEO score generateSEODraft always computes — 0 if no SEO pass
	// ran (only possible when nothing about the title/meta/content needed fixing, which itself
	// short-circuits to ErrPolicyFixNothingToFix before a result is ever built).
	SEOScore int    `json:"seo_score,omitempty"`
	Warning  string `json:"warning,omitempty"`
}

// ErrPolicyFixNothingToFix is returned when a fresh re-check finds none of the AI-fixable
// problems (thin/medium content, duplicate content, corrupted artifacts, short title, short/
// missing meta description) still apply — most likely the content was already edited since the
// scan that surfaced the warning.
var ErrPolicyFixNothingToFix = errors.New("لم يتم رصد أي مشكلة قابلة للإصلاح بالذكاء الاصطناعي حاليًا لهذا العنصر — قد يكون قد عُدّل منذ آخر فحص، جرّب إعادة الفحص")

// policyFixMinAttempts/policyFixMaxAttempts bound fixContentWithRetries' Together AI calls — a
// deliberately smaller ceiling than GenerateDraft's contentDraftMinAttempts/MaxAttempts (3-4).
// A "fix" starts from the item's real existing content rather than a blank title, so a usable
// result on the first or second attempt is the common case; capping at 3 (worst case ~90s
// instead of ~120s) meaningfully cuts the wait for the by-far most common trigger (thin/medium
// content) without materially hurting quality — an admin unhappy with the result can always
// click "إصلاح بالذكاء الاصطناعي" again.
const (
	policyFixMinAttempts = 2
	policyFixMaxAttempts = 3
)

// FixPolicyContent re-checks one article/post against the exact same diagnostics the
// adsense-policy scan uses, then fixes only whichever of those are still present:
//   - thin (<120 words) or medium (<300 words) content is expanded/strengthened;
//   - content that still duplicates another live item (checked with itself excluded from the
//     corpus) is substantially rewritten to be unique, given the sibling's title and an excerpt
//     as an explicit "differentiate from this" reference;
//   - generic AI-filler boilerplate already present in the content is removed and replaced with
//     real subject-matter substance;
//   - unresolved template-replacement artifacts (e.g. "${1}") are removed and the surrounding
//     text smoothed;
//   - a too-short title and/or a too-short/missing meta description are regenerated by the
//     existing, already-tested generateSEODraft pass (scored against the same AnalyzeSEO the
//     manual "تحليل الآن" button uses) — but ONLY when the title or meta description was itself
//     flagged, not automatically just because the content changed (an intentional speed/
//     completeness tradeoff: this used to roughly double the wait for the common
//     content-only-fix case, and a content fix alone rarely makes an already-adequate meta
//     description inaccurate since the underlying topic doesn't change).
func (s *contentDraftService) FixPolicyContent(ctx context.Context, req PolicyFixRequest) (*PolicyFixResult, error) {
	if s.apiKey == "" {
		return nil, ErrContentDraftUnavailable
	}

	title, contentHTML, meta, gradeLevel, subjectName, semesterName, categoryName, err := s.loadPolicyFixSource(req)
	if err != nil {
		return nil, err
	}

	plainText := contentquality.NormalizeForSimilarity(contentHTML)
	words := 0
	if plainText != "" {
		words = len(strings.Fields(plainText))
	}
	titleShort := len([]rune(strings.TrimSpace(title))) < contentquality.DiagnosticTitleMinChars
	contentThin := words < contentquality.DiagnosticReviewMinWords
	contentMedium := !contentThin && words < contentquality.DiagnosticStrongMinWords
	metaShort := len([]rune(strings.TrimSpace(meta))) < contentquality.DiagnosticMetaMinChars

	var excludeArticleID, excludePostID uint64
	if req.ContentType == "article" {
		excludeArticleID = req.ID
	} else {
		excludePostID = req.ID
	}
	candidateKey := fmt.Sprintf("%s:%d", req.ContentType, req.ID)
	dup, corpusByKey := s.findDuplicateMatch(req.CountryID, excludeArticleID, excludePostID, candidateKey, title, contentHTML)

	artifacts := contentquality.DetectReplacementArtifacts(
		contentquality.TextField{Name: "title", Value: title},
		contentquality.TextField{Name: "content", Value: contentHTML},
	)
	// The exact generic-padding phrases DetectGenericFillerPhrases catches are also a
	// fixable content problem in their own right — a published item can already clear every
	// word-count threshold and still be mostly low-value filler, exactly the pattern that got
	// the site rejected by AdSense before (see findWeakContent, adsensepolicy/handler.go, which
	// now flags this same check). Without this, clicking "AI Fix" on such an item would wrongly
	// report ErrPolicyFixNothingToFix.
	existingFiller := contentquality.DetectGenericFillerPhrases(plainText)

	needsContentFix := contentThin || contentMedium || dup != nil || len(artifacts) > 0 || len(existingFiller) > 0
	needsTitleFix := titleShort
	needsMetaFix := metaShort

	if !needsContentFix && !needsTitleFix && !needsMetaFix {
		return nil, ErrPolicyFixNothingToFix
	}

	finalTitle := title
	finalContentHTML := contentHTML
	finalMeta := meta
	var issues []string
	var warnings []string

	if needsContentFix {
		siblingTitle, siblingExcerpt := "", ""
		if dup != nil {
			siblingTitle = dup.Title
			if doc, ok := corpusByKey[dup.Key]; ok {
				siblingExcerpt = truncate(contentquality.NormalizeForSimilarity(doc.Content), 600)
			}
		}
		fixCtx := contentFixContext{
			ContentType: req.ContentType, Title: title, PlainContent: plainText,
			GradeLevel: gradeLevel, SubjectName: subjectName, SemesterName: semesterName, CategoryName: categoryName,
			Thin: contentThin, Medium: contentMedium,
			DuplicateTitle: siblingTitle, DuplicateExcerpt: siblingExcerpt,
			Artifacts:      artifacts,
			ExistingFiller: existingFiller,
		}
		fixedHTML, newDup, filler, fixWarning := s.fixContentWithRetries(ctx, req, candidateKey, fixCtx, excludeArticleID, excludePostID)
		if fixedHTML != "" {
			finalContentHTML = fixedHTML
			if w := combinedWarning(newDup, filler); w != "" {
				warnings = append(warnings, w)
			}
		}
		// fixWarning can be set alongside a successful fixedHTML too (e.g. corrupted-content
		// artifacts survived every retry attempt) as well as on total failure — surface it
		// either way rather than only when the whole fix attempt failed outright.
		if fixWarning != "" {
			warnings = append(warnings, fixWarning)
		}
		if contentThin {
			issues = append(issues, "المحتوى قصير جدًا")
		}
		if contentMedium {
			issues = append(issues, "المحتوى متوسط الطول ويحتاج تعزيزًا")
		}
		if dup != nil {
			issues = append(issues, fmt.Sprintf("تشابه مرتفع مع محتوى آخر (\"%s\")", dup.Title))
		}
		if len(artifacts) > 0 {
			issues = append(issues, "بقايا نص آلي غير مكتمل")
		}
		if len(existingFiller) > 0 {
			issues = append(issues, "عبارات حشو عامة نمطية للذكاء الاصطناعي")
		}
	}

	var seoScore int
	// Only run the separate SEO/meta pass when the title or meta description was ITSELF
	// flagged — not merely because content changed. Running it unconditionally after every
	// content fix used to roughly double the wait for the most common case (thin/medium content
	// alone), and a content-only fix does not change the page's underlying topic, so an
	// already-adequate meta description very rarely goes stale from it. This is a deliberate
	// speed/completeness tradeoff: an admin who wants the meta description refreshed after a
	// content-only fix can still do that with one click from the SEO panel's own "تحليل الآن".
	if needsTitleFix || needsMetaFix {
		seoReq := ContentDraftRequest{ContentType: req.ContentType, Title: title}
		if seo, seoWarning := s.generateSEODraft(ctx, seoReq, finalContentHTML); seo != nil {
			seoScore = seo.Score
			// generateSEOOnce falls back to the original title verbatim when the model's
			// response omits seo_title — only credit "العنوان قصير" as addressed when a
			// genuinely different (and by construction longer) title actually came back.
			if needsTitleFix && seo.SEOTitle != "" && seo.SEOTitle != title {
				finalTitle = seo.SEOTitle
				issues = append(issues, "العنوان قصير")
			}
			if needsMetaFix {
				issues = append(issues, "الوصف التعريفي قصير أو مفقود")
				finalMeta = seo.MetaDescription
			}
			if seoWarning != "" {
				warnings = append(warnings, seoWarning)
			}
		} else {
			warnings = append(warnings, "تعذر توليد اقتراح للعنوان/الوصف التعريفي بالذكاء الاصطناعي، راجعهما يدويًا.")
		}
	}

	return &PolicyFixResult{
		Title:           finalTitle,
		ContentHTML:     finalContentHTML,
		MetaDescription: finalMeta,
		WordCount:       contentquality.SimilarityWordCount(finalContentHTML),
		IssuesAddressed: issues,
		SEOScore:        seoScore,
		Warning:         strings.Join(warnings, " "),
	}, nil
}

// loadPolicyFixSource fetches the current title/content/meta description and the classification
// context (grade/subject/semester for articles, category for posts) the content-draft prompts
// already use — read fresh from the database rather than trusting anything the client sent, so a
// stale/tampered request can't fix the wrong item or use stale context.
func (s *contentDraftService) loadPolicyFixSource(req PolicyFixRequest) (title, contentHTML, meta, gradeLevel, subjectName, semesterName, categoryName string, err error) {
	switch req.ContentType {
	case "article":
		article, findErr := s.articleRepo.FindByID(req.CountryID, req.ID)
		if findErr != nil {
			return "", "", "", "", "", "", "", fmt.Errorf("تعذر العثور على المقال")
		}
		title = article.Title
		contentHTML = article.Content
		if article.MetaDescription != nil {
			meta = *article.MetaDescription
		}
		if article.GradeLevel != nil {
			gradeLevel = *article.GradeLevel
		}
		if article.Subject != nil {
			subjectName = article.Subject.SubjectName
		}
		if article.Semester != nil {
			semesterName = article.Semester.SemesterName
		}
		return title, contentHTML, meta, gradeLevel, subjectName, semesterName, "", nil
	case "post":
		post, findErr := s.postRepo.FindByID(req.CountryID, req.ID)
		if findErr != nil {
			return "", "", "", "", "", "", "", fmt.Errorf("تعذر العثور على المنشور")
		}
		title = post.Title
		contentHTML = post.Content
		if post.MetaDescription != nil {
			meta = *post.MetaDescription
		}
		if post.Category != nil {
			categoryName = post.Category.Name
		}
		return title, contentHTML, meta, "", "", "", categoryName, nil
	default:
		return "", "", "", "", "", "", "", fmt.Errorf("نوع المحتوى غير صحيح")
	}
}

// findDuplicateMatch is checkDuplicate's counterpart for content that already has a row in the
// database: excludeArticleID/excludePostID (only one is ever non-zero for a given call) keep the
// item from being compared against itself, which checkDuplicate — built for not-yet-saved
// drafts — has no reason to do. Also returns a Key->document lookup so the caller can pull an
// excerpt of whatever this matched against, to hand the model as concrete "differentiate from
// this" context instead of a blind retry.
func (s *contentDraftService) findDuplicateMatch(countryID database.CountryID, excludeArticleID, excludePostID uint64, candidateKey, title, content string) (*contentquality.DuplicateMatch, map[string]contentquality.SimilarityDocument) {
	corpus := make([]contentquality.SimilarityDocument, 0, 256)
	corpusByKey := make(map[string]contentquality.SimilarityDocument, 256)
	if rows, err := s.articleRepo.ListContentForDuplicateCheck(countryID, excludeArticleID); err == nil {
		for _, row := range rows {
			doc := contentquality.SimilarityDocument{Key: fmt.Sprintf("article:%d", row.ID), Title: row.Title, Content: row.Content}
			corpus = append(corpus, doc)
			corpusByKey[doc.Key] = doc
		}
	}
	if rows, err := s.postRepo.ListContentForDuplicateCheck(countryID, excludePostID); err == nil {
		for _, row := range rows {
			doc := contentquality.SimilarityDocument{Key: fmt.Sprintf("post:%d", row.ID), Title: row.Title, Content: row.Content}
			corpus = append(corpus, doc)
			corpusByKey[doc.Key] = doc
		}
	}
	candidate := contentquality.SimilarityDocument{Key: candidateKey, Title: title, Content: content}
	matches := contentquality.DetectDuplicateAgainstCorpus(candidate, corpus, contentquality.DefaultSimilarityOptions())
	for i := range matches {
		switch matches[i].Kind {
		case contentquality.SimilarityKindExact, contentquality.SimilarityKindNear, contentquality.SimilarityKindTemplate:
			return &matches[i], corpusByKey
		}
	}
	return nil, corpusByKey
}

// contentFixContext carries everything buildContentFixPrompts needs to describe exactly one
// content-fix request: the item's classification (for the same scoping line
// buildContentDraftPrompts already uses), which quality problems apply, and — for a duplicate —
// the sibling item's title/excerpt to differentiate from.
type contentFixContext struct {
	ContentType  string
	Title        string
	PlainContent string

	GradeLevel   string
	SubjectName  string
	SemesterName string
	CategoryName string

	Thin   bool
	Medium bool

	DuplicateTitle   string
	DuplicateExcerpt string

	Artifacts []contentquality.ReplacementArtifact
	// ExistingFiller is whichever contentquality.GenericFillerPhrases were found in the
	// content BEFORE this fix attempt — distinct from the avoidFiller parameter
	// fixContentWithRetries passes on retry (which is about the AI's own PRIOR attempt at this
	// fix reintroducing filler); this is about the ORIGINAL published content already having it.
	ExistingFiller []string
}

// fixContentWithRetries mirrors GenerateDraft's retry loop (same attempt bounds, same
// duplicate/filler feedback pattern) but starts from the item's EXISTING content instead of
// generating from a blank title, and additionally re-checks for the specific problem(s) that
// triggered the fix (word count, unresolved artifacts) before accepting an attempt. Returns
// ("", nil, nil, warning) only if every attempt failed outright or stayed unusable — the caller
// then keeps the original, unmodified content rather than showing an empty draft.
func (s *contentDraftService) fixContentWithRetries(ctx context.Context, req PolicyFixRequest, candidateKey string, fixCtx contentFixContext, excludeArticleID, excludePostID uint64) (html string, dup *contentquality.DuplicateMatch, filler []string, warning string) {
	attempts := len(s.models)
	if attempts < policyFixMinAttempts {
		attempts = policyFixMinAttempts
	}
	if attempts > policyFixMaxAttempts {
		attempts = policyFixMaxAttempts
	}

	var (
		best        string
		lastErr     error
		avoidDup    bool
		avoidFiller []string
		avoidBarely bool
		bestBarely  bool
	)

	for attempt := 0; attempt < attempts; attempt++ {
		model := s.models[attempt%len(s.models)]
		attemptHTML, truncated, err := s.fixContentOnce(ctx, model, fixCtx, avoidDup, avoidFiller, avoidBarely)
		if err != nil {
			lastErr = err
			continue
		}
		wordCount := contentquality.SimilarityWordCount(attemptHTML)
		if truncated || wordCount < contentDraftMinWords {
			if truncated {
				lastErr = fmt.Errorf("%w: انقطع الرد قبل اكتماله (%d كلمة فقط)", ErrContentDraftFailed, wordCount)
			} else {
				lastErr = fmt.Errorf("%w: الرد قصير جدًا وغير كافٍ (%d كلمة فقط)", ErrContentDraftFailed, wordCount)
			}
			avoidDup, avoidFiller, avoidBarely = false, nil, false
			continue
		}

		best = attemptHTML
		lastErr = nil
		attemptDup, _ := s.findDuplicateMatch(req.CountryID, excludeArticleID, excludePostID, candidateKey, fixCtx.Title, attemptHTML)
		attemptFiller := contentquality.DetectGenericFillerPhrases(attemptHTML)
		remainingArtifacts := contentquality.DetectReplacementArtifacts(contentquality.TextField{Name: "content", Value: attemptHTML})
		stillTooShort := (fixCtx.Thin || fixCtx.Medium) && wordCount < contentquality.DiagnosticStrongMinWords
		// The model is explicitly asked to preserve correct existing information, and it takes
		// that instruction too literally on some attempts — echoing the original almost verbatim
		// (sometimes with a single corrupted character) instead of actually rewriting it. None of
		// the other checks catch this: word count stays fine, there's no duplicate, no filler, no
		// artifacts. A direct self-similarity check against the original plain content is the only
		// thing that catches "the AI changed nothing."
		selfSimilarity := contentquality.JaccardSimilarity(attemptHTML, fixCtx.PlainContent, 5)
		barelyChanged := selfSimilarity >= 0.75

		dup, filler = attemptDup, attemptFiller
		bestBarely = barelyChanged
		if attemptDup == nil && len(attemptFiller) == 0 && len(remainingArtifacts) == 0 && !stillTooShort && !barelyChanged {
			return best, nil, nil, ""
		}
		avoidDup, avoidFiller, avoidBarely = attemptDup != nil, attemptFiller, barelyChanged
	}

	if best == "" {
		if lastErr != nil {
			return "", nil, nil, fmt.Sprintf("تعذر إصلاح المحتوى بالذكاء الاصطناعي: %v", lastErr)
		}
		return "", nil, nil, "تعذر إصلاح المحتوى بالذكاء الاصطناعي، حاول مرة أخرى."
	}
	// The loop above already re-checks artifacts on every attempt and keeps retrying while any
	// remain (see stillTooShort/attemptDup/attemptFiller alongside it) — this only fires when
	// every attempt still left something behind, so the admin is told explicitly rather than a
	// still-broken snippet slipping through silently.
	if remaining := contentquality.DetectReplacementArtifacts(contentquality.TextField{Name: "content", Value: best}); len(remaining) > 0 {
		return best, dup, filler, "تنبيه: لا تزال هناك بقايا نص آلي غير مكتمل بعد عدة محاولات — راجع المحتوى يدويًا قبل الحفظ."
	}
	if bestBarely {
		return best, dup, filler, "تنبيه: الصياغة الناتجة قريبة جدًا من النص الأصلي بعد عدة محاولات ولم تُعالج المشكلة فعليًا — راجع المحتوى يدويًا قبل الحفظ."
	}
	return best, dup, filler, ""
}

// fixContentOnce is fixContentWithRetries' single Together AI call — identical HTTP mechanics
// and response handling to generateOnce (content_draft_service.go), differing only in the prompt
// builder used (buildContentFixPrompts instead of buildContentDraftPrompts).
func (s *contentDraftService) fixContentOnce(ctx context.Context, model string, fixCtx contentFixContext, avoidDuplicate bool, avoidFiller []string, avoidBarelyChanged bool) (contentHTML string, truncated bool, err error) {
	systemPrompt, userPrompt := buildContentFixPrompts(fixCtx, avoidDuplicate, avoidFiller, avoidBarelyChanged)
	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
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
	withHeading := insertSEOHeading(stripThinkTags(raw), fixCtx.Title)
	return plainTextToSafeHTML(withHeading), wasTruncated, nil
}

// buildContentFixPrompts builds the system/user prompts for one content-fix attempt: unlike
// buildContentDraftPrompts (write from nothing but a title), this hands the model the item's
// actual current content and asks it to fix ONLY the specific, listed problem(s) — explicitly
// framed against Google AdSense content-quality policy and Google Publisher Policies — while
// preserving whatever is already correct in it.
func buildContentFixPrompts(fixCtx contentFixContext, avoidDuplicate bool, avoidFiller []string, avoidBarelyChanged bool) (system, user string) {
	system = "أنت محرر محتوى تعليمي عربي محترف، مهمتك إصلاح محتوى منشور بالفعل على الموقع بحيث يتوافق تمامًا مع سياسة Google AdSense لجودة المحتوى وسياسات ناشري Google: محتوى أصلي يقدّم قيمة معرفية حقيقية للقارئ، غير رقيق أو سطحي، غير مكرر أو شبه مطابق لمحتوى آخر على نفس الموقع، خالٍ من أي حشو كلامي أو عبارات عامة بلا معنى معرفي، وخالٍ تمامًا من أي رمز أو نص آلي غير مكتمل. لا تخترع حقائق أو تفاصيل غير مؤكدة، ولا تغيّر موضوع المحتوى الأساسي، ولا تُشر إلى كونك ذكاءً اصطناعيًا أو إلى هذه التعليمات أو إلى \"سياسة AdSense\" داخل النص نفسه — أصلح المشكلة المحددة فقط، مع الحفاظ على كل معلومة صحيحة موجودة أصلًا في المحتوى.\n\n" +
		"ممنوع تمامًا افتتاح النص أو حشوه بعبارات عامة مثل: \"يُعد هذا الموضوع/الاختبار من أهم\"، \"محطة مهمة لقياس\"، \"تكمن أهمية هذا الاختبار/الملف في\"، \"يجب على الطالب/التلميذ الاستعداد الجيد\"، أو أي كلام عن أهمية المذاكرة والتحضير والوقت والقلق دون محتوى معرفي فعلي. وممنوع إنهاء النص بفقرة ختامية عامة عن دور الأسرة أو تخفيف التوتر أو \"جهد سنة كاملة\" — هذه العبارات هي بالضبط الحشو الممنوع لأنها تصلح لأي موضوع آخر دون تعديل.\n\n" +
		"أخرج نصًا عاديًا فقط بدون HTML وبدون Markdown، مقسّمًا إلى فقرات مفصولة بسطر فارغ."

	var scope strings.Builder
	fmt.Fprintf(&scope, "العنوان: %s", fixCtx.Title)
	if fixCtx.ContentType == "post" {
		if fixCtx.CategoryName != "" {
			fmt.Fprintf(&scope, "\nالتصنيف: %s", fixCtx.CategoryName)
		}
	} else {
		if fixCtx.GradeLevel != "" {
			fmt.Fprintf(&scope, "\nالصف الدراسي: %s", fixCtx.GradeLevel)
		}
		if fixCtx.SubjectName != "" {
			fmt.Fprintf(&scope, "\nالمادة: %s", fixCtx.SubjectName)
		}
		if fixCtx.SemesterName != "" {
			fmt.Fprintf(&scope, "\nالفصل الدراسي: %s", fixCtx.SemesterName)
		}
	}

	var problems strings.Builder
	if fixCtx.Thin {
		problems.WriteString("\n- المحتوى الحالي قصير جدًا (أقل من 120 كلمة) ويُعدّ محتوى رقيقًا وفق سياسة AdSense لجودة المحتوى. وسّعه ليتجاوز 300 كلمة من الشرح المعرفي الحقيقي (تعريف أو حقيقة مباشرة، قاعدة أو مفهوم أساسي، مثال ملموس، استراتيجية عملية) لا مجرد إعادة صياغة الجمل الموجودة أو تكرارها.")
	} else if fixCtx.Medium {
		problems.WriteString("\n- المحتوى الحالي متوسط الطول وقد يحتاج تعزيزًا (أقل من 300 كلمة). عزّزه بمعلومة أو مثال أو خطوة عملية إضافية حقيقية حتى يتجاوز 300 كلمة، دون حشو أو تكرار لما هو موجود.")
	}
	if fixCtx.DuplicateTitle != "" {
		fmt.Fprintf(&problems, "\n- المحتوى الحالي يتشابه بشكل مرتفع مع محتوى آخر منشور فعلًا على نفس الموقع بعنوان \"%s\"، وهذا يخالف سياسة AdSense بشأن المحتوى المكرر. أعد الصياغة والبنية وترتيب الأمثلة من زاوية مختلفة تمامًا بحيث يصبح محتوى فريدًا ومستقلًا، مع بقائه دقيقًا ومرتبطًا بالعنوان نفسه. تجنّب أي تشابه في الصياغة أو ترتيب الجمل مع هذا المقتطف من المحتوى الآخر:\n\"%s\"", fixCtx.DuplicateTitle, fixCtx.DuplicateExcerpt)
	}
	if len(fixCtx.Artifacts) > 0 {
		tokens := make([]string, 0, len(fixCtx.Artifacts))
		seen := make(map[string]bool, len(fixCtx.Artifacts))
		for _, a := range fixCtx.Artifacts {
			if seen[a.Token] {
				continue
			}
			seen[a.Token] = true
			tokens = append(tokens, a.Token)
		}
		fmt.Fprintf(&problems, "\n- المحتوى الحالي يحتوي على بقايا نص آلي غير مكتمل (رموز استبدال لم تُحل قط): %s. احذف هذه الرموز تمامًا من النص وأعد صياغة الجزء المحيط بها بحيث يكون النص متكاملاً ومفهومًا وطبيعيًا دون أي فجوة أو رمز غريب، ودون اختراع تفاصيل غير مؤكدة مكانها.", strings.Join(tokens, "، "))
	}
	if len(fixCtx.ExistingFiller) > 0 {
		fmt.Fprintf(&problems, "\n- المحتوى الحالي يحتوي فعليًا على عبارات حشو عامة نمطية ممنوعة بالضبط: \"%s\". احذف هذه العبارات أينما وردت واستبدلها بمعلومة معرفية حقيقية محددة بالموضوع (تعريف، قاعدة، مثال، أو خطوة عملية) — هذا النوع من الحشو هو بالضبط ما تسبب سابقًا برفض الموقع من AdSense لضعف قيمة المحتوى.", strings.Join(fixCtx.ExistingFiller, "\"، \""))
	}

	user = fmt.Sprintf(`فيما يلي المحتوى الحالي المنشور بالفعل لهذا العنصر التعليمي. أصلح فيه حصرًا المشكلة (أو المشاكل) المحددة أدناه، مع الحفاظ على أي معلومة صحيحة موجودة فيه ودون تغيير موضوعه الأساسي:
%s

المشاكل المطلوب إصلاحها فقط:%s

المحتوى الحالي (نص عادي مستخرج من الصفحة المنشورة):
"""
%s
"""

الشروط:
- أعد نصًا عاديًا كاملاً بديلاً للمحتوى الحالي بأكمله (وليس فقط الجزء المعدّل)، مقسّمًا إلى فقرات مفصولة بسطر فارغ.
- أعد صياغة الجمل والفقرات فعليًا (ترتيب أفكار، أسلوب، أمثلة) بدلاً من نسخ النص الأصلي شبه حرفيًا — النسخ شبه الحرفي لا يُصلح أي مشكلة من المشاكل المذكورة أعلاه.
- لغة عربية فصيحة سليمة، بدون HTML وبدون Markdown.
- لا تذكر داخل النص نفسه أنك تُصلح مشكلة أو تشير إلى أي سياسة أو إلى كونك ذكاءً اصطناعيًا.`, scope.String(), problems.String(), fixCtx.PlainContent)

	if avoidDuplicate {
		user += "\n\nملاحظة مهمة: المحاولة السابقة لا تزال متشابهة جدًا مع محتوى آخر على الموقع. أعد الصياغة والبنية من زاوية مختلفة تمامًا هذه المرة."
	}
	if len(avoidFiller) > 0 {
		user += fmt.Sprintf("\n\nملاحظة مهمة: المحاولة السابقة استخدمت عبارات حشو عامة ممنوعة بالضبط: \"%s\". لا تستخدم هذه العبارات ولا ما يشابهها، واستبدلها بمعلومة معرفية محددة.", strings.Join(avoidFiller, "\"، \""))
	}
	if avoidBarelyChanged {
		user += "\n\nملاحظة مهمة جدًا: المحاولة السابقة كانت شبه مطابقة حرفيًا للنص الأصلي (مجرد إعادة نسخ مع تغييرات طفيفة لا تُذكر) ولم تُصلح المشكلة فعليًا. هذه المرة أعد صياغة الفقرات فعليًا بترتيب وأسلوب وأمثلة مختلفة عن الأصل، مع الحفاظ على المعلومات الصحيحة فقط — لا تكتفِ بنسخ الجمل كما هي."
	}

	return system, strings.TrimSpace(user)
}
