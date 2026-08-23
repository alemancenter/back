package contentaudit

import (
	"context"
	"fmt"
	"strings"

	coreai "github.com/imanjo/fiber-api/internal/services"
)

const (
	GenerationSourceModeGroundedFile             = "grounded_file"
	GenerationSourceModeGeneralKnowledgeFallback = "general_knowledge_fallback"
)

// GroundedGenerationResult keeps the existing SEOArticle response contract while adding
// explicit provenance. Embedded SEOArticle fields remain top-level in JSON, so existing
// frontend consumers keep working and newer clients can distinguish file-grounded output
// from the deliberate general-knowledge fallback used for unreadable/scanned PDFs.
type GroundedGenerationResult struct {
	*coreai.SEOArticle
	SourceMode         string   `json:"source_mode"`
	SourceFileReadable bool     `json:"source_file_readable"`
	SourceWarning      string   `json:"source_warning,omitempty"`
	GroundingScore     *int     `json:"grounding_score,omitempty"`
	GroundingVerified  bool     `json:"grounding_verified"`
	DraftQualityScore  int      `json:"draft_quality_score"`
	DraftQualityIssues []string `json:"draft_quality_issues,omitempty"`
}

type draftGroundingValidator func(groundedDraft) (groundedValidation, error)

// GenerateGroundedDraftWithProvenance is the production generation path used by the AI
// handler. It preserves v10's intentional fallback for image-only/unreadable Arabic PDFs,
// but makes that fallback explicit in the API and gives the readable-file path the same
// independent claim validation principle used by Grounded Content Repair v2.
func (s *Service) GenerateGroundedDraftWithProvenance(ctx context.Context, req GroundedGenerateRequest) (*GroundedGenerationResult, error) {
	if len(req.FileIDs) == 0 {
		return nil, ErrGroundedSourceInsufficient
	}

	pack, fileEvidenceFound, err := buildNewContentSourcePack(ctx, req)
	if err != nil {
		return nil, err
	}

	var (
		write          draftWriter
		audience       []string
		facts          groundedFactExtraction
		usingFallback  = !fileEvidenceFound
		fallbackReason string
	)

	if !fileEvidenceFound {
		fallbackReason = "تعذر استخراج نص موثوق من الملف المرفق؛ قد يكون الملف صورة ممسوحة ضوئيًا أو بلا طبقة نصية قابلة للاستخراج."
	}

	if fileEvidenceFound {
		facts, _, err = runGroundedFactExtraction(WithAIModelStrategy(ctx, "economy"), pack)
		if err != nil {
			return nil, err
		}
		facts = sanitizeGroundedFacts(facts, pack)
		if facts.InsufficientSource || len(facts.Facts) == 0 {
			usingFallback = true
			fallbackReason = "تم استخراج نص من الملف، لكن لم تكن الأدلة المستخرجة كافية لبناء مسودة موثقة بالكامل."
		} else {
			audience = facts.Audience
			writerCtx := WithAIModelStrategy(ctx, "balanced")
			write = func(feedback string) (groundedDraft, error) {
				draft, _, err := runNewContentWriter(writerCtx, pack, facts, feedback)
				return draft, err
			}
		}
	}

	if usingFallback {
		writerCtx := WithAIModelStrategy(ctx, "balanced")
		write = func(feedback string) (groundedDraft, error) {
			draft, _, err := runTitleOnlyWriter(writerCtx, pack, feedback)
			return draft, err
		}

		bestDraft, bestValidation, haveDraft := runQualityLoop(write, req.Title, audience)
		if !haveDraft {
			return nil, fmt.Errorf("%w: تعذر توليد مسودة صالحة", ErrGroundedValidationFailed)
		}

		qualityIssues := bestValidation.issues()
		if bestValidation.total() < seoQualityMinScore {
			qualityIssues = append([]string{fmt.Sprintf("لم تصل المسودة لحد الجودة الداخلي %d/100 بعد %d محاولات؛ الدرجة الفعلية %d/100.", seoQualityMinScore, seoGenerationMaxAttempts, bestValidation.total())}, qualityIssues...)
		}
		warning := fallbackReason + " تم إنشاء المسودة اعتمادًا على العنوان والسياق الدراسي والمعرفة العامة المستقرة؛ راجع الدقة الواقعية بعناية قبل النشر."
		return buildGenerationResult(bestDraft, bestValidation, nil, GenerationSourceModeGeneralKnowledgeFallback, fileEvidenceFound, warning, qualityIssues), nil
	}

	validatorCtx := WithAIModelStrategy(ctx, "quality")
	validate := func(draft groundedDraft) (groundedValidation, error) {
		validation, _, err := runGroundedValidator(validatorCtx, pack, facts, draft)
		return validation, err
	}

	bestDraft, bestValidation, grounding, haveDraft, err := runQualityLoopWithGrounding(write, validate, req.Title, audience, len(facts.Facts))
	if err != nil {
		return nil, err
	}
	if !haveDraft {
		return nil, fmt.Errorf("%w: لم تنتج أي محاولة مسودة اجتازت مدقق التوثيق المستقل", ErrGroundedValidationFailed)
	}

	qualityIssues := bestValidation.issues()
	if bestValidation.total() < seoQualityMinScore {
		qualityIssues = append([]string{fmt.Sprintf("لم تصل المسودة لحد الجودة الداخلي %d/100 بعد %d محاولات؛ الدرجة الفعلية %d/100.", seoQualityMinScore, seoGenerationMaxAttempts, bestValidation.total())}, qualityIssues...)
	}
	return buildGenerationResult(bestDraft, bestValidation, &grounding, GenerationSourceModeGroundedFile, true, "", qualityIssues), nil
}

// runQualityLoopWithGrounding only considers a draft eligible for the "best" slot after an
// independent validator confirms score >= groundingMinScore and zero unsupported claims.
// Unsupported drafts are fed back to the writer and retried; they can never win merely by
// having a high SEO/draft-quality score.
func runQualityLoopWithGrounding(write draftWriter, validate draftGroundingValidator, fallbackTitle string, audience []string, factCount int) (groundedDraft, seoQualityValidation, groundedValidation, bool, error) {
	var (
		bestDraft      groundedDraft
		bestQuality    seoQualityValidation
		bestGrounding  groundedValidation
		haveValidDraft bool
		feedback       string
		lastGrounding  groundedValidation
	)

	for attempt := 0; attempt < seoGenerationMaxAttempts; attempt++ {
		draft, err := write(feedback)
		if err != nil {
			if !haveValidDraft {
				return groundedDraft{}, seoQualityValidation{}, groundedValidation{}, false, err
			}
			break
		}
		draft = normalizeGeneratedDraft(draft, fallbackTitle, audience)

		if !validUsedFactIndexes(draft.UsedFactIndexes, factCount) {
			feedback = "المسودة لم تربط محتواها بالحقائق المستخرجة ربطًا صالحًا. أعد الكتابة مستخدمًا used_fact_indexes صحيحة فقط، ولا تضف أي ادعاء خارج facts."
			continue
		}

		grounding, err := validate(draft)
		if err != nil {
			if !haveValidDraft {
				return groundedDraft{}, seoQualityValidation{}, groundedValidation{}, false, err
			}
			break
		}
		grounding.GroundingScore = clamp(grounding.GroundingScore)
		grounding.UnsupportedClaims = compactStrings(grounding.UnsupportedClaims)
		grounding.Notes = compactStrings(grounding.Notes)
		lastGrounding = grounding

		if grounding.GroundingScore < groundingMinScore || len(grounding.UnsupportedClaims) > 0 {
			feedback = groundingRetryFeedback(grounding)
			continue
		}

		quality := scoreSEOQuality(draft)
		if !haveValidDraft || quality.total() > bestQuality.total() {
			bestDraft = draft
			bestQuality = quality
			bestGrounding = grounding
			haveValidDraft = true
		}
		if bestQuality.total() >= seoQualityMinScore {
			break
		}
		feedback = seoRetryFeedback(quality)
	}

	if !haveValidDraft {
		return groundedDraft{}, seoQualityValidation{}, lastGrounding, false, fmt.Errorf("%w: grounding_score=%d unsupported_claims=%d", ErrGroundedValidationFailed, lastGrounding.GroundingScore, len(lastGrounding.UnsupportedClaims))
	}
	return bestDraft, bestQuality, bestGrounding, true, nil
}

func normalizeGeneratedDraft(draft groundedDraft, fallbackTitle string, audience []string) groundedDraft {
	draft.Title = strings.TrimSpace(draft.Title)
	if draft.Title == "" {
		draft.Title = strings.TrimSpace(fallbackTitle)
	}
	draft.ContentHTML = normalizeFixedHTML(draft.ContentHTML)
	draft.MetaDescription = strings.TrimSpace(draft.MetaDescription)
	draft.Keywords = compactStrings(draft.Keywords)
	draft.CoverAltText = strings.TrimSpace(draft.CoverAltText)
	if draft.MetaDescription == "" {
		draft.MetaDescription = deriveMetaDescriptionFallback(draft.ContentHTML)
	}
	if len(draft.Keywords) == 0 {
		draft.Keywords = deriveKeywordsFallback(draft.Title, audience)
	}
	if draft.CoverAltText == "" {
		draft.CoverAltText = draft.Title
	}
	return draft
}

func groundingRetryFeedback(v groundedValidation) string {
	parts := []string{fmt.Sprintf("مدقق التوثيق المستقل أعطى المسودة %d/100، والحد الأدنى %d/100.", clamp(v.GroundingScore), groundingMinScore)}
	unsupported := compactStrings(v.UnsupportedClaims)
	if len(unsupported) > 3 {
		unsupported = unsupported[:3]
	}
	if len(unsupported) > 0 {
		parts = append(parts, "ادعاءات غير مدعومة يجب حذفها أو إعادة صياغتها اعتمادًا حصريًا على facts: "+strings.Join(unsupported, " | "))
	}
	parts = append(parts, "لا تعوض حذف الادعاءات غير المدعومة بإضافة معلومات جديدة غير موجودة في facts.")
	return strings.Join(parts, "\n")
}

func buildGenerationResult(draft groundedDraft, quality seoQualityValidation, grounding *groundedValidation, sourceMode string, sourceFileReadable bool, sourceWarning string, qualityIssues []string) *GroundedGenerationResult {
	plainText := normalizePlainText(draft.ContentHTML)
	article := &coreai.SEOArticle{
		Title:           draft.Title,
		MetaDescription: draft.MetaDescription,
		Keywords:        draft.Keywords,
		Content:         plainText,
		ContentHTML:     draft.ContentHTML,
		CoverAltText:    draft.CoverAltText,
		SEOScore:        quality.total(), // legacy compatibility; UI should prefer draft_quality_score.
		SEOIssues:       append([]string(nil), qualityIssues...),
		WordCount:       len(strings.Fields(plainText)),
	}
	result := &GroundedGenerationResult{
		SEOArticle:         article,
		SourceMode:         sourceMode,
		SourceFileReadable: sourceFileReadable,
		SourceWarning:      strings.TrimSpace(sourceWarning),
		GroundingVerified:  grounding != nil && grounding.GroundingScore >= groundingMinScore && len(grounding.UnsupportedClaims) == 0,
		DraftQualityScore:  quality.total(),
		DraftQualityIssues: append([]string(nil), qualityIssues...),
	}
	if grounding != nil {
		score := clamp(grounding.GroundingScore)
		result.GroundingScore = &score
	}
	// Keep the old seo_issues contract useful for older frontends by surfacing provenance
	// warnings there too, while draft_quality_issues remains quality-only for newer clients.
	if result.SourceWarning != "" {
		article.SEOIssues = append([]string{result.SourceWarning}, article.SEOIssues...)
	}
	return result
}
