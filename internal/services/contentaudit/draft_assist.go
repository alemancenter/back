package contentaudit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	coreai "github.com/imanjo/fiber-api/internal/services"
	"github.com/imanjo/fiber-api/internal/utils"
)

// ErrDraftAssistEmptySource means the caller gave nothing (or unusable input) to
// suggest from — this is a draft, so there is no saved audit/content row to fall
// back to.
var ErrDraftAssistEmptySource = errors.New("العنوان والنص فارغان أو غير كافيين لاقتراح شيء منهما")

const (
	draftKeywordsMinCount = 1
	draftKeywordsMaxCount = 8
	draftKeywordsMaxChars = 40
)

// GenerateDraftMetaDescription suggests a meta description directly from unsaved
// draft text (no ContentAIDecision, no persisted content row required), so the
// editor can get a suggestion while still writing instead of only after publish.
// It reuses the exact same generation/validation rules as the post-publish repair
// path (metadata_repair.go's generateSafeMetaDescription/validateSafeMetaDescription)
// so a draft-time suggestion and a later automated repair can never disagree.
func GenerateDraftMetaDescription(ctx context.Context, ai coreai.AIService, title, contentHTML string) (string, string, error) {
	plain := normalizePlainText(contentHTML)
	if strings.TrimSpace(title+plain) == "" {
		return "", "", ErrDraftAssistEmptySource
	}

	if ai != nil {
		response, err := ai.RunContentIntelligence(ctx, coreai.ContentIntelligenceRequest{
			Task:          "repair_meta_description",
			ModelStrategy: "balanced",
			ContentType:   "article",
			Title:         title,
			PlainText:     plain,
			Language:      "ar",
		})
		if err == nil {
			if candidate, validateErr := validateSafeMetaDescription(response.FixedMetaDescription, title, plain); validateErr == nil {
				return candidate, firstNonEmptyLocal(response.Model, response.Provider, "ai"), nil
			}
		}
	}

	fallback := deriveSafeMetaDescription(title, plain)
	candidate, err := validateSafeMetaDescription(fallback, title, plain)
	if err != nil {
		return "", "", err
	}
	return candidate, "extractive_fallback", nil
}

// GenerateDraftKeywords suggests internal taxonomy keywords (used for internal
// search and related-content, never Google's ignored meta-keywords tag — see
// contentquality.ContentQualitySignal) directly from unsaved draft text. Unlike
// the meta description path there is no safe extractive fallback for keywords, so
// a failed/unavailable AI call returns an error rather than a guessed list.
func GenerateDraftKeywords(ctx context.Context, ai coreai.AIService, title, contentHTML string) (string, string, error) {
	plain := normalizePlainText(contentHTML)
	if strings.TrimSpace(title+plain) == "" {
		return "", "", ErrDraftAssistEmptySource
	}
	if ai == nil {
		return "", "", fmt.Errorf("%w: خدمة الذكاء الاصطناعي غير متاحة", ErrDraftAssistEmptySource)
	}

	response, err := ai.RunContentIntelligence(ctx, coreai.ContentIntelligenceRequest{
		Task:          "suggest_keywords",
		ModelStrategy: "balanced",
		ContentType:   "article",
		Title:         title,
		PlainText:     plain,
		Language:      "ar",
	})
	if err != nil {
		return "", "", err
	}

	candidate, validateErr := validateDraftKeywords(response.FixedKeywords)
	if validateErr != nil {
		return "", "", validateErr
	}
	return candidate, firstNonEmptyLocal(response.Model, response.Provider, "ai"), nil
}

// SEOOptimizeInput is the draft-time source for a full SEO metadata bundle.
type SEOOptimizeInput struct {
	Title        string
	ContentHTML  string
	FocusKeyword string
	ContentType  string
	CountryCode  string
	GradeName    string
	SubjectName  string
	CategoryName string
}

// SEOOptimizeBundle is the validated, clamped set of SEO fields the editor fills.
type SEOOptimizeBundle struct {
	SEOTitle           string `json:"seo_title"`
	MetaDescription    string `json:"meta_description"`
	FocusKeyword       string `json:"focus_keyword"`
	AdditionalKeywords string `json:"additional_keywords"`
	OGTitle            string `json:"og_title"`
	OGDescription      string `json:"og_description"`
	TwitterTitle       string `json:"twitter_title"`
	TwitterDescription string `json:"twitter_description"`
	SchemaType         string `json:"schema_type"`
}

// seoOptimizeSchemaTypes is the analyzer-safe subset: these score 4/4 with no
// custom JSON-LD, unlike HowTo/FAQPage/… which need a full graph.
var seoOptimizeSchemaTypes = map[string]bool{"Article": true, "NewsArticle": true, "BlogPosting": true}

// GenerateDraftSEOBundle fills every SEO field from unsaved editor text, tuned to
// the deterministic AnalyzeSEO rubric. It reuses the same validators as the
// post-publish metadata repair path so a draft suggestion and a later automated
// repair can never disagree. When the AI is unavailable or fails it degrades to a
// deterministic bundle rather than erroring — the editor still gets a fill. The
// third return value is the AI failure reason (empty when the model was used).
func GenerateDraftSEOBundle(ctx context.Context, ai coreai.AIService, in SEOOptimizeInput) (SEOOptimizeBundle, string, string, error) {
	title := strings.TrimSpace(in.Title)
	plain := normalizePlainText(in.ContentHTML)
	if strings.TrimSpace(title+plain) == "" {
		return SEOOptimizeBundle{}, "", "", ErrDraftAssistEmptySource
	}

	defaultSchema := "Article"
	if strings.EqualFold(strings.TrimSpace(in.ContentType), "post") {
		defaultSchema = "BlogPosting"
	}

	var (
		aiTitle, aiFocus, aiMeta, aiAddKw, aiSchema string
		aiOGTitle, aiOGDesc, aiTwTitle, aiTwDesc    string
		provider                                    = "extractive_fallback"
		aiErr                                       string
	)
	if ai != nil {
		resp, err := ai.RunContentIntelligence(ctx, coreai.ContentIntelligenceRequest{
			Task:          "optimize_seo",
			ModelStrategy: "balanced",
			ContentType:   in.ContentType,
			CountryCode:   in.CountryCode,
			GradeName:     in.GradeName,
			SubjectName:   in.SubjectName,
			CategoryName:  in.CategoryName,
			Title:         title,
			PlainText:     plain,
			FocusKeyword:  in.FocusKeyword,
			Language:      "ar",
		})
		switch {
		case err != nil:
			aiErr = err.Error()
		case resp != nil:
			aiTitle, aiFocus, aiMeta = resp.SEOTitle, resp.SEOFocusKeyword, resp.FixedMetaDescription
			aiAddKw, aiSchema = resp.SEOAdditionalKeywords, strings.TrimSpace(resp.SEOSchemaType)
			aiOGTitle, aiOGDesc = resp.SEOOGTitle, resp.SEOOGDescription
			aiTwTitle, aiTwDesc = resp.SEOTwitterTitle, resp.SEOTwitterDescription
			provider = firstNonEmptyLocal(resp.Model, resp.Provider, "ai")
		}
	}

	bundle := SEOOptimizeBundle{}

	// Focus keyword must actually appear in the page text so the analyzer's
	// intro/density checks can pass: model pick → editor's pick → leading title words.
	fk := sanitizeSEOPhrase(aiFocus, 60)
	if fk == "" || !seoPhraseInText(fk, plain, title) {
		if editor := sanitizeSEOPhrase(in.FocusKeyword, 60); editor != "" && seoPhraseInText(editor, plain, title) {
			fk = editor
		} else {
			fk = leadingSignificantWords(title, 3)
		}
	}
	// A too-generic keyword stuffs itself in a short article (analyzer flags
	// density > 3.5% as an error). If the pick is over-dense, promote a longer
	// title-anchored phrase or an AI synonym that still reads naturally.
	if keywordDensityPct(fk, plain) > 3.5 {
		fk = pickLessDenseKeyword(fk, plain, title, aiAddKw)
	}
	bundle.FocusKeyword = fk

	// Meta description: same validator as the automated repair path; on failure
	// fall back to a deterministic extractive summary.
	if candidate, err := validateSafeMetaDescription(aiMeta, title, plain); err == nil {
		bundle.MetaDescription = candidate
	} else {
		derived := deriveSafeMetaDescription(title, plain)
		if candidate, err := validateSafeMetaDescription(derived, title, plain); err == nil {
			bundle.MetaDescription = candidate
		} else {
			bundle.MetaDescription = strings.TrimSpace(derived)
		}
	}

	bundle.SEOTitle = clampSEOTitle(aiTitle, title)

	if kw, err := validateDraftKeywords(aiAddKw); err == nil {
		bundle.AdditionalKeywords = kw
	}

	bundle.OGTitle = firstNonEmptyLocal(sanitizeSEOLine(aiOGTitle, 90), bundle.SEOTitle)
	bundle.TwitterTitle = firstNonEmptyLocal(sanitizeSEOLine(aiTwTitle, 90), bundle.SEOTitle)
	bundle.OGDescription = firstNonEmptyLocal(sanitizeSEOLine(aiOGDesc, 300), bundle.MetaDescription)
	bundle.TwitterDescription = firstNonEmptyLocal(sanitizeSEOLine(aiTwDesc, 300), bundle.MetaDescription)

	if !seoOptimizeSchemaTypes[aiSchema] {
		aiSchema = defaultSchema
	}
	bundle.SchemaType = aiSchema

	return bundle, provider, aiErr, nil
}

// sanitizeSEOPhrase cleans a short keyword phrase: no HTML, no URLs, single
// spaces, rune-clamped. Returns "" if unusable.
func sanitizeSEOPhrase(raw string, maxRunes int) string {
	v := strings.Join(strings.Fields(utils.SanitizeInput(raw)), " ")
	if v == "" || strings.ContainsAny(v, "<>{}") || safeMetadataURLPattern.MatchString(v) {
		return ""
	}
	if utf8.RuneCountInString(v) > maxRunes {
		return ""
	}
	return v
}

// sanitizeSEOLine cleans a title/description line, trimming to a word boundary
// at or below maxRunes. Returns "" if unusable.
func sanitizeSEOLine(raw string, maxRunes int) string {
	v := strings.Join(strings.Fields(utils.SanitizeInput(raw)), " ")
	if v == "" || strings.ContainsAny(v, "<>") || safeMetadataURLPattern.MatchString(v) {
		return ""
	}
	return trimToWordBoundary(v, maxRunes)
}

// clampSEOTitle keeps the model's title unless it is unusably short or much
// longer than the analyzer's 65-rune "good" ceiling, in which case it trims to a
// word boundary; a too-short/empty result falls back to the page title.
func clampSEOTitle(aiTitle, fallback string) string {
	v := strings.Join(strings.Fields(utils.SanitizeInput(aiTitle)), " ")
	if utf8.RuneCountInString(v) < 12 || strings.ContainsAny(v, "<>") || safeMetadataURLPattern.MatchString(v) {
		return strings.Join(strings.Fields(fallback), " ")
	}
	if utf8.RuneCountInString(v) > 70 {
		trimmed := trimToWordBoundary(v, 65)
		if utf8.RuneCountInString(trimmed) >= 25 {
			return trimmed
		}
	}
	return v
}

func trimToWordBoundary(v string, maxRunes int) string {
	runes := []rune(v)
	if len(runes) <= maxRunes {
		return v
	}
	cut := runes[:maxRunes]
	for i := len(cut) - 1; i >= 0; i-- {
		if cut[i] == ' ' {
			return strings.TrimRight(strings.TrimSpace(string(cut[:i])), "،؛:.-—")
		}
	}
	return strings.TrimSpace(string(cut))
}

// seoPhraseInText reports whether phrase appears in any of texts after light
// Arabic normalization (diacritics/tatweel removed, lowercased).
func seoPhraseInText(phrase string, texts ...string) bool {
	needle := normalizeArabicLite(phrase)
	if needle == "" {
		return false
	}
	for _, text := range texts {
		if strings.Contains(normalizeArabicLite(text), needle) {
			return true
		}
	}
	return false
}

func normalizeArabicLite(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		switch {
		case r == 'ـ', // tatweel
			r >= 'ً' && r <= 'ٟ', // harakat
			r == 'ٰ',             // superscript alef
			r >= 'ۖ' && r <= 'ۭ': // quranic annotation marks
			continue
		case r == 'أ' || r == 'إ' || r == 'آ' || r == 'ٱ': // أ إ آ ٱ
			r = 'ا' // ا
		case r == 'ى': // ى
			r = 'ي' // ي
		case r == 'ة': // ة
			r = 'ه' // ه
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// keywordDensityPct mirrors the analyzer's keyword-density metric (whole-phrase,
// space-delimited count × phrase-word-count ÷ total words × 100) on lightly
// normalized Arabic so the guard sees roughly the same number the analyzer will.
func keywordDensityPct(phrase, plainText string) float64 {
	np := normalizeArabicLite(plainText)
	nk := normalizeArabicLite(phrase)
	words := strings.Fields(np)
	kwWords := strings.Fields(nk)
	if len(words) == 0 || len(kwWords) == 0 {
		return 0
	}
	count := strings.Count(" "+np+" ", " "+nk+" ")
	return float64(count) * float64(len(kwWords)) / float64(len(words)) * 100
}

// pickLessDenseKeyword returns a phrase that is present in the body, ideally also
// in the title, at a healthy density (≤2.5%). It tries progressively longer
// title prefixes, then AI-suggested synonyms. Falls back to the original.
func pickLessDenseKeyword(current, plain, title, synonyms string) string {
	candidates := []string{
		leadingSignificantWords(title, 5),
		leadingSignificantWords(title, 4),
		leadingSignificantWords(title, 3),
	}
	for _, syn := range strings.FieldsFunc(synonyms, func(r rune) bool { return r == ',' || r == '،' || r == ';' }) {
		candidates = append(candidates, strings.Join(strings.Fields(syn), " "))
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || candidate == current || strings.ContainsAny(candidate, "<>") {
			continue
		}
		density := keywordDensityPct(candidate, plain)
		if seoPhraseInText(candidate, plain) && density >= 0.4 && density <= 2.5 {
			return candidate
		}
	}
	return current
}

func leadingSignificantWords(title string, n int) string {
	out := make([]string, 0, n)
	for _, word := range strings.Fields(title) {
		if utf8.RuneCountInString(word) < 2 {
			continue
		}
		out = append(out, word)
		if len(out) >= n {
			break
		}
	}
	return strings.Join(out, " ")
}

func validateDraftKeywords(raw string) (string, error) {
	items := utils.SplitKeywords(raw)
	cleaned := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || strings.ContainsAny(item, "<>") || safeMetadataURLPattern.MatchString(item) {
			continue
		}
		if utf8.RuneCountInString(item) > draftKeywordsMaxChars {
			continue
		}
		cleaned = append(cleaned, item)
		if len(cleaned) >= draftKeywordsMaxCount {
			break
		}
	}
	if len(cleaned) < draftKeywordsMinCount {
		return "", fmt.Errorf("%w: لم يقترح النموذج كلمات دلالية صالحة", ErrSafeMetadataValidation)
	}
	return strings.Join(cleaned, ", "), nil
}
