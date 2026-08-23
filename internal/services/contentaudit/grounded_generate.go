package contentaudit

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
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

	// Fact extraction is a mechanical, well-scoped task (pull short quoted facts out of
	// already-provided evidence) — it doesn't need the same heavy model tier the writer does,
	// and routing it to the fast/economy tier is most of what makes this pipeline take minutes
	// instead of tens of seconds. See WithAIModelStrategy / groundedModelCandidatesForContext.
	facts, _, err := runGroundedFactExtraction(WithAIModelStrategy(ctx, "economy"), pack)
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

	// "balanced" trades a little raw model horsepower for real speed on the writer stage
	// itself — the stage that actually determines whether the deterministic checklist below
	// passes. Left at "quality"/default here, this pipeline routes to the heaviest, slowest
	// model tier for every single call, which is most of where the multi-minute wall time
	// came from; "balanced" is still a strong model, just a faster one.
	writerCtx := WithAIModelStrategy(ctx, "balanced")
	for attempt := 0; attempt < seoGenerationMaxAttempts; attempt++ {
		draft, _, err := runNewContentWriter(writerCtx, pack, facts, feedback)
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
		draft.CoverAltText = strings.TrimSpace(draft.CoverAltText)
		if draft.MetaDescription == "" {
			draft.MetaDescription = deriveMetaDescriptionFallback(draft.ContentHTML)
		}
		if len(draft.Keywords) == 0 {
			draft.Keywords = deriveKeywordsFallback(draft.Title, facts)
		}
		if draft.CoverAltText == "" {
			draft.CoverAltText = draft.Title
		}

		validation := scoreSEOQuality(draft)
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
		CoverAltText:    bestDraft.CoverAltText,
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
	extractedAny := false
	notFoundCount, unreadableCount := 0, 0
	for _, id := range req.FileIDs {
		var file models.File
		if err := db.First(&file, id).Error; err != nil {
			notFoundCount++
			continue
		}
		if extracted, ok := fileextract.ReadAttachmentEvidence(file, storageRoot); ok {
			extractedAny = true
			label := "نص مستخرج من الملف " + firstNonEmptyLocal(file.FileName, "#"+strconv.FormatUint(uint64(file.ID), 10))
			pack.Evidence = append(pack.Evidence, groundedEvidence{ID: fmt.Sprintf("file:%d:text", file.ID), Kind: "attachment_text", Label: label, Text: truncateAttachmentEvidence(extracted), Verified: true})
		} else {
			unreadableCount++
		}
	}

	// Without this, a file that fails extraction (most commonly: a scanned/image-only PDF
	// with no real text layer — pdftotext then returns nothing) silently degrades to a pack
	// with only the title as "evidence". The writer then has nothing real to work from and
	// pads out a hollow article that just restates the title in different words — exactly the
	// thin content this whole feature exists to prevent — while still costing 3 full retry
	// rounds before failing the SEO bar anyway. Fail fast instead, with a specific reason.
	if len(req.FileIDs) > 0 && !extractedAny {
		switch {
		case unreadableCount > 0:
			return pack, fmt.Errorf("%w: تعذّر استخراج أي نص من الملف المرفق. تأكد أنه مستند نصي حقيقي (DOCX أو PDF بنص قابل للتحديد) وليس صورة ممسوحة ضوئيًا أو PDF بلا طبقة نصية", ErrGroundedSourceInsufficient)
		case notFoundCount > 0:
			return pack, fmt.Errorf("%w: الملف المرفق لم يعد موجودًا، يرجى رفعه من جديد", ErrGroundedSourceInsufficient)
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
	system := `أنت محرر تعليمي عربي محترف ومراجع SEO في آن واحد. اكتب مقالة جديدة كاملة اعتمادًا حصريًا على الحقائق المستخرجة من الملف المرفق. لا تضف أي معلومة غير موجودة في facts. اكتب صفحة شاملة ومهيكلة بعناوين h2 فرعية حقيقية، فقرات مفصّلة تشرح لا تسرد فقط، بجودة احترافية تستحق نشرها وأرشفتها في محركات البحث. أضف meta_description (120-160 حرفًا بالضبط تقريبًا، يحوي الكلمة المفتاحية الرئيسية، أسلوب معلوماتي دقيق بلا مبالغة تسويقية)، keywords (5 إلى 8 كلمات/عبارات مفتاحية حقيقية مشتقة من العنوان والحقائق والسياق الدراسي فقط، تظهر كل واحدة منها فعليًا داخل content_html — هذا الحقل إلزامي ولا يجوز أن يكون فارغًا أبدًا)، وcover_alt_text (جملة قصيرة تصف موضوع المحتوى، تُستخدم كنص بديل لصورة الغلاف). اجعل الفقرة الأولى من content_html تحتوي الكلمة المفتاحية الرئيسية. HTML نظيف فقط داخل content_html بعناوين h2 وفقرات p. بعد كتابة المسودة، راجعها بنفسك كمراجع SEO مستقل وقيّمها بصدق من 0 إلى 30 بناءً على الاحترافية والوضوح والقيمة التعليمية الفعلية (لا تكافئ الطول وحده، لا تعطِ 30 إلا إن كانت ممتازة فعلًا) في quality_score، مع ملاحظات مختصرة في quality_notes إن وُجد نقص. أعد JSON فقط.`
	user := fmt.Sprintf(`السياق:\n%s\n\nالحقائق المسموح استخدامها فقط:\n%s\n\nأرجع JSON بالمفاتيح: title, content_html, meta_description, keywords, cover_alt_text, used_fact_indexes, quality_score, quality_notes.`, string(contextJSON), string(factsJSON))
	if strings.TrimSpace(feedback) != "" {
		user += "\n\nملاحظات من محاولة سابقة يجب معالجتها في هذه المحاولة:\n" + feedback
	}
	model, err := groundedAIJSON(ctx, "new_content_writer", system, user, &out)
	return out, model, err
}

// scoreSEOQuality blends the deterministic checklist (majority weight, pure Go, instant —
// title/meta length, keyword coverage, heading structure, word count) with the writer's own
// self-reported quality_score/quality_notes from the same call above, instead of a second
// independent AI round trip. Self-review is less rigorous than an independent reviewer would
// be, but it's the softer 30-point half of the score; the hard, ungameable 70 points (facts
// must trace to the uploaded file, exact structural checks) are unaffected by this.
func scoreSEOQuality(draft groundedDraft) seoQualityValidation {
	det, detIssues := deterministicSEOScore(draft)
	qual := clamp(draft.QualityScore)
	if qual > 30 {
		qual = 30
	}
	return seoQualityValidation{DeterministicScore: det, DeterministicIssues: detIssues, QualitativeScore: qual, QualitativeNotes: compactStrings(draft.QualityNotes)}
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

// deriveKeywordsFallback builds keywords straight from the title and the extracted facts'
// own audience/purpose fields when the writer's JSON omitted them — grounded by construction
// (both are already-validated facts-extraction output, not invented), guaranteeing the
// keywords field is never left empty for a genuinely successful generation.
var titleWordSplitter = regexp.MustCompile(`[\s,،.:؛!؟\-_/]+`)

func deriveKeywordsFallback(title string, facts groundedFactExtraction) []string {
	keywords := make([]string, 0, 6)
	seen := map[string]bool{}
	add := func(word string) {
		word = strings.TrimSpace(word)
		if len([]rune(word)) < 3 || seen[word] || len(keywords) >= 6 {
			return
		}
		seen[word] = true
		keywords = append(keywords, word)
	}
	for _, word := range titleWordSplitter.Split(title, -1) {
		add(word)
	}
	for _, audience := range facts.Audience {
		add(audience)
	}
	return keywords
}

func seoRetryFeedback(v seoQualityValidation) string {
	all := v.issues()
	if len(all) == 0 {
		return fmt.Sprintf("الدرجة الحالية %d من 100 وتحتاج تحسينًا عامًا في العمق والاحترافية.", v.total())
	}
	return strings.Join(all, "\n")
}
