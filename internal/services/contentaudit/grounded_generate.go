package contentaudit

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/imanjo/fiber-api/internal/config"
	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/fileextract"
	"github.com/imanjo/fiber-api/internal/models"
	coreai "github.com/imanjo/fiber-api/internal/services"
)

const (
	// seoQualityMinScore is deliberately strict — content only clears it with near-perfect
	// compliance on every deterministic check plus a strong AI qualitative pass. The retry
	// loop below (not luck on one attempt) is what makes hitting it realistic.
	seoQualityMinScore      = 99
	seoGenerationMaxAttempts = 3
)

// GroundedGenerateRequest describes a brand-new article/post to draft from uploaded file(s) —
// no existing Article/Post/ContentAIDecision required, unlike CreateGroundedFixPreview above.
type GroundedGenerateRequest struct {
	Title             string
	ContentType       string // "article" | "post"
	CountryCode       string
	FileIDs           []uint
	GradeName         string
	SubjectName       string
	SemesterName      string
	CategoryName      string
	CurriculumContext string
}

// seoQualityValidation blends a deterministic checklist (majority weight — title/meta length,
// keyword coverage, heading structure, word count) with a short AI qualitative pass
// (professionalism/genuine value) — not a single opaque AI-self-reported integer, so a "99%"
// claim actually means something checkable.
type seoQualityValidation struct {
	DeterministicScore  int
	DeterministicIssues []string
	QualitativeScore    int
	QualitativeNotes    []string
}

func (v seoQualityValidation) total() int {
	return clamp(v.DeterministicScore + v.QualitativeScore)
}

func (v seoQualityValidation) issues() []string {
	return compactStrings(append(append([]string{}, v.DeterministicIssues...), v.QualitativeNotes...))
}

// GenerateGroundedDraft builds new title/content/meta_description/keywords grounded in
// uploaded file evidence, retrying with specific feedback until the SEO-quality bar is
// cleared or attempts are exhausted. Always returns its best attempt with the real score
// disclosed in SEOScore/SEOIssues — never silently claims the bar was met when it wasn't.
func (s *Service) GenerateGroundedDraft(ctx context.Context, req GroundedGenerateRequest) (*coreai.SEOArticle, error) {
	if len(req.FileIDs) == 0 {
		return nil, ErrGroundedSourceInsufficient
	}

	pack, err := buildNewContentSourcePack(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(pack.Evidence) == 0 {
		return nil, ErrGroundedSourceInsufficient
	}

	facts, _, err := runGroundedFactExtraction(ctx, pack)
	if err != nil {
		return nil, err
	}
	facts = sanitizeGroundedFacts(facts, pack)
	if facts.InsufficientSource || len(facts.Facts) == 0 {
		detail := groundedSourceNotesSummary(facts.SourceNotes)
		if detail != "" {
			return nil, fmt.Errorf("%w: %s", ErrGroundedSourceInsufficient, detail)
		}
		return nil, fmt.Errorf("%w: لا توجد حقائق موثقة كافية في الملف المرفوع", ErrGroundedSourceInsufficient)
	}

	var bestDraft groundedDraft
	var bestValidation seoQualityValidation
	haveDraft := false
	feedback := ""

	for attempt := 0; attempt < seoGenerationMaxAttempts; attempt++ {
		draft, _, err := runNewContentWriter(ctx, pack, facts, feedback)
		if err != nil {
			if !haveDraft {
				return nil, err
			}
			break
		}
		draft.Title = strings.TrimSpace(draft.Title)
		if draft.Title == "" {
			draft.Title = strings.TrimSpace(req.Title)
		}
		draft.ContentHTML = normalizeFixedHTML(draft.ContentHTML)
		draft.MetaDescription = strings.TrimSpace(draft.MetaDescription)
		draft.Keywords = compactStrings(draft.Keywords)
		if draft.MetaDescription == "" {
			draft.MetaDescription = deriveMetaDescriptionFallback(draft.ContentHTML)
		}

		validation := scoreSEOQuality(ctx, draft, pack)
		if !haveDraft || validation.total() > bestValidation.total() {
			bestDraft = draft
			bestValidation = validation
			haveDraft = true
		}
		if bestValidation.total() >= seoQualityMinScore {
			break
		}
		feedback = seoRetryFeedback(validation)
	}

	if !haveDraft {
		return nil, fmt.Errorf("%w: تعذّر توليد مسودة صالحة من الملف المرفوع", ErrGroundedValidationFailed)
	}

	issues := bestValidation.issues()
	if bestValidation.total() < seoQualityMinScore {
		issues = append([]string{fmt.Sprintf("لم تصل المسودة لحد الجودة %d%% بعد %d محاولة — الدرجة الفعلية %d%%.", seoQualityMinScore, seoGenerationMaxAttempts, bestValidation.total())}, issues...)
	}

	plainText := normalizePlainText(bestDraft.ContentHTML)
	return &coreai.SEOArticle{
		Title:           bestDraft.Title,
		MetaDescription: bestDraft.MetaDescription,
		Keywords:        bestDraft.Keywords,
		Content:         plainText,
		ContentHTML:     bestDraft.ContentHTML,
		SEOScore:        bestValidation.total(),
		SEOIssues:       issues,
		WordCount:       len(strings.Fields(plainText)),
	}, nil
}

// buildNewContentSourcePack loads the requested files (per-country DB, matching how
// Article/Post/File rows are sharded) and extracts their text via fileextract, plus the
// title and educational classification context — no existing content row to load from.
func buildNewContentSourcePack(ctx context.Context, req GroundedGenerateRequest) (groundedSourcePack, error) {
	cc := strings.ToLower(strings.TrimSpace(req.CountryCode))
	if cc == "" {
		cc = "jo"
	}
	pack := groundedSourcePack{
		ContentType: normalizeContentType(req.ContentType),
		CountryCode: cc,
		Title:       strings.TrimSpace(req.Title),
		Curriculum:  strings.TrimSpace(buildCurriculumContext(cc, req.GradeName, req.SubjectName, req.SemesterName, req.CategoryName)),
	}
	if req.CurriculumContext != "" {
		if pack.Curriculum != "" {
			pack.Curriculum += " | "
		}
		pack.Curriculum += strings.TrimSpace(req.CurriculumContext)
	}
	if pack.Title != "" {
		pack.Evidence = append(pack.Evidence, groundedEvidence{ID: "content:title", Kind: "title", Label: "العنوان المطلوب", Text: pack.Title, Verified: true})
	}
	if pack.Curriculum != "" {
		pack.Evidence = append(pack.Evidence, groundedEvidence{ID: "content:curriculum", Kind: "curriculum", Label: "السياق الدراسي", Text: pack.Curriculum, Verified: true})
	}

	db := database.GetManager().GetByCode(cc).WithContext(ctx)
	storageRoot := config.Get().Storage.Path
	for _, id := range req.FileIDs {
		var file models.File
		if err := db.First(&file, id).Error; err != nil {
			continue
		}
		if extracted, ok := fileextract.ReadAttachmentEvidence(file, storageRoot); ok {
			label := "نص مستخرج من الملف " + firstNonEmptyLocal(file.FileName, "#"+strconv.FormatUint(uint64(file.ID), 10))
			pack.Evidence = append(pack.Evidence, groundedEvidence{ID: fmt.Sprintf("file:%d:text", file.ID), Kind: "attachment_text", Label: label, Text: truncateAttachmentEvidence(extracted), Verified: true})
		}
	}
	return pack, nil
}

func runNewContentWriter(ctx context.Context, pack groundedSourcePack, facts groundedFactExtraction, feedback string) (groundedDraft, string, error) {
	var out groundedDraft
	factsJSON, _ := json.Marshal(facts)
	contextJSON, _ := json.Marshal(map[string]interface{}{
		"title":        pack.Title,
		"content_type": pack.ContentType,
		"country_code": pack.CountryCode,
		"curriculum":   pack.Curriculum,
	})
	system := `أنت محرر تعليمي عربي محترف. اكتب مقالة جديدة كاملة اعتمادًا حصريًا على الحقائق المستخرجة من الملف المرفق. لا تضف أي معلومة غير موجودة في facts. اكتب صفحة شاملة ومهيكلة بعناوين h2 فرعية حقيقية، فقرات مفصّلة تشرح لا تسرد فقط، بجودة احترافية تستحق نشرها وأرشفتها في محركات البحث. أضف meta_description (120-160 حرفًا بالضبط تقريبًا، يحوي الكلمة المفتاحية الرئيسية، أسلوب معلوماتي دقيق بلا مبالغة تسويقية) وkeywords (5 إلى 8 كلمات/عبارات مفتاحية حقيقية مشتقة من العنوان والحقائق والسياق الدراسي فقط، تظهر كل واحدة منها فعليًا داخل content_html). اجعل الفقرة الأولى من content_html تحتوي الكلمة المفتاحية الرئيسية. HTML نظيف فقط داخل content_html بعناوين h2 وفقرات p. أعد JSON فقط.`
	user := fmt.Sprintf(`السياق:\n%s\n\nالحقائق المسموح استخدامها فقط:\n%s\n\nأرجع JSON بالمفاتيح: title, content_html, meta_description, keywords, used_fact_indexes.`, string(contextJSON), string(factsJSON))
	if strings.TrimSpace(feedback) != "" {
		user += "\n\nملاحظات من محاولة سابقة يجب معالجتها في هذه المحاولة:\n" + feedback
	}
	model, err := groundedAIJSON(ctx, "new_content_writer", system, user, &out)
	return out, model, err
}

func scoreSEOQuality(ctx context.Context, draft groundedDraft, pack groundedSourcePack) seoQualityValidation {
	det, detIssues := deterministicSEOScore(draft)
	qual, qualNotes := qualitativeSEOScore(ctx, draft, pack)
	return seoQualityValidation{DeterministicScore: det, DeterministicIssues: detIssues, QualitativeScore: qual, QualitativeNotes: qualNotes}
}

func deterministicSEOScore(draft groundedDraft) (int, []string) {
	score := 0
	issues := []string{}

	titleLen := len([]rune(strings.TrimSpace(draft.Title)))
	if titleLen >= 30 && titleLen <= 60 {
		score += 10
	} else {
		issues = append(issues, fmt.Sprintf("طول العنوان %d حرفًا؛ المطلوب بين 30 و60.", titleLen))
	}

	metaLen := len([]rune(draft.MetaDescription))
	if metaLen >= 120 && metaLen <= 160 {
		score += 10
	} else {
		issues = append(issues, fmt.Sprintf("طول الوصف التعريفي %d حرفًا؛ المطلوب بين 120 و160.", metaLen))
	}

	plain := normalizePlainText(draft.ContentHTML)
	lowerPlain := normalizeText(plain)

	metaHasKeyword := len(draft.Keywords) > 0 && strings.Contains(normalizeText(draft.MetaDescription), normalizeText(draft.Keywords[0]))
	if metaHasKeyword {
		score += 5
	} else {
		issues = append(issues, "الوصف التعريفي لا يحوي الكلمة المفتاحية الرئيسية.")
	}

	if len(draft.Keywords) >= 5 && len(draft.Keywords) <= 8 {
		score += 10
	} else {
		issues = append(issues, fmt.Sprintf("عدد الكلمات المفتاحية %d؛ المطلوب بين 5 و8.", len(draft.Keywords)))
	}

	missingKeywords := 0
	for _, kw := range draft.Keywords {
		if kw == "" {
			continue
		}
		if !strings.Contains(lowerPlain, normalizeText(kw)) {
			missingKeywords++
		}
	}
	if len(draft.Keywords) > 0 && missingKeywords == 0 {
		score += 10
	} else if missingKeywords > 0 {
		issues = append(issues, fmt.Sprintf("%d كلمة مفتاحية غير موجودة فعليًا داخل المحتوى.", missingKeywords))
	}

	h2Count := strings.Count(strings.ToLower(draft.ContentHTML), "<h2")
	if h2Count >= 2 {
		score += 10
	} else {
		issues = append(issues, fmt.Sprintf("عدد العناوين الفرعية h2 هو %d؛ المطلوب اثنان على الأقل.", h2Count))
	}

	wordCount := len(strings.Fields(plain))
	if wordCount >= 600 && wordCount <= 1500 {
		score += 10
	} else {
		issues = append(issues, fmt.Sprintf("عدد الكلمات %d؛ المطلوب بين 600 و1500.", wordCount))
	}

	firstPara := plain
	if idx := strings.Index(plain, ". "); idx > 0 && idx < len(plain)-1 {
		firstPara = plain[:idx]
	}
	if len(draft.Keywords) > 0 && strings.Contains(normalizeText(firstPara), normalizeText(draft.Keywords[0])) {
		score += 5
	} else {
		issues = append(issues, "الفقرة الأولى لا تحوي الكلمة المفتاحية الرئيسية.")
	}

	return score, issues
}

func qualitativeSEOScore(ctx context.Context, draft groundedDraft, pack groundedSourcePack) (int, []string) {
	type qualResult struct {
		Score int      `json:"score"`
		Notes []string `json:"notes"`
	}
	var out qualResult
	draftJSON, _ := json.Marshal(map[string]interface{}{"title": draft.Title, "content_html": draft.ContentHTML, "meta_description": draft.MetaDescription})
	system := `أنت محرر SEO محترف. قيّم هذه المسودة التعليمية من 0 إلى 30 بناءً على: الاحترافية والوضوح، القيمة التعليمية الفعلية، وسلاسة القراءة. لا تكافئ الطول وحده. إن كانت ممتازة أعطِ 30. أعد JSON فقط.`
	user := fmt.Sprintf(`راجع هذه المسودة:\n%s\n\nأرجع JSON بالمفاتيح: score, notes (ملاحظات مختصرة إن وُجد نقص).`, string(draftJSON))
	if _, err := groundedAIJSON(ctx, "seo_qualitative_reviewer", system, user, &out); err != nil {
		return 0, []string{"تعذّر تشغيل المراجعة النوعية لهذه المحاولة."}
	}
	if out.Score < 0 {
		out.Score = 0
	}
	if out.Score > 30 {
		out.Score = 30
	}
	return out.Score, compactStrings(out.Notes)
}

func seoRetryFeedback(v seoQualityValidation) string {
	all := v.issues()
	if len(all) == 0 {
		return fmt.Sprintf("الدرجة الحالية %d من 100 وتحتاج تحسينًا عامًا في العمق والاحترافية.", v.total())
	}
	return strings.Join(all, "\n")
}
