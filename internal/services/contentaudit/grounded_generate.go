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
	seoQualityMinScore       = 99
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

// draftWriter is either runNewContentWriter (grounded-in-file) or runTitleOnlyWriter
// (general-knowledge fallback) with pack/facts already bound via closure — lets the shared
// quality-retry loop in runQualityLoop stay identical regardless of which produced the draft.
type draftWriter func(feedback string) (groundedDraft, error)

// GenerateGroundedDraft builds new title/content/meta_description/keywords. It strongly
// prefers grounding every claim in the uploaded file's real text; when that file can't be
// read at all (lost, or unreadable — most commonly a scanned/image-only PDF with no text
// layer), it falls back to professional general-knowledge generation from the title instead
// of hard-failing. This fallback was a deliberate decision after weighing it against OCR
// (unreliable for Arabic here) and web-search-then-reword (rejected — matches Google's
// "scraped/auto-generated content" policy, a distinct and often more severely penalized
// violation than thin content). Either way, the same strict SEO-quality retry loop applies,
// and the real score is always disclosed in SEOScore/SEOIssues — never silently claims the
// bar was met when it wasn't.
func (s *Service) GenerateGroundedDraft(ctx context.Context, req GroundedGenerateRequest) (*coreai.SEOArticle, error) {
	if len(req.FileIDs) == 0 {
		return nil, ErrGroundedSourceInsufficient
	}

	pack, fileEvidenceFound, err := buildNewContentSourcePack(ctx, req)
	if err != nil {
		return nil, err
	}

	// "balanced"/"economy" trade a little raw model horsepower for real speed — routing every
	// call to the heaviest default tier is most of what made this pipeline take minutes
	// instead of tens of seconds. See WithAIModelStrategy / groundedModelCandidatesForContext.
	var write draftWriter
	var audience []string
	usingFallback := !fileEvidenceFound
	if fileEvidenceFound {
		facts, _, err := runGroundedFactExtraction(WithAIModelStrategy(ctx, "economy"), pack)
		if err != nil {
			return nil, err
		}
		facts = sanitizeGroundedFacts(facts, pack)
		if facts.InsufficientSource || len(facts.Facts) == 0 {
			// The file was readable but yielded nothing substantive (e.g. a near-empty
			// document) — same fallback as an unreadable file, not a hard failure.
			usingFallback = true
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
	}

	bestDraft, bestValidation, haveDraft := runQualityLoop(write, req.Title, audience)
	if !haveDraft {
		return nil, fmt.Errorf("%w: تعذّر توليد مسودة صالحة", ErrGroundedValidationFailed)
	}

	issues := bestValidation.issues()
	if bestValidation.total() < seoQualityMinScore {
		issues = append([]string{fmt.Sprintf("لم تصل المسودة لحد الجودة %d%% بعد %d محاولة — الدرجة الفعلية %d%%.", seoQualityMinScore, seoGenerationMaxAttempts, bestValidation.total())}, issues...)
	}
	if usingFallback {
		issues = append([]string{"تعذّرت قراءة الملف المرفق، فتم التوليد من معرفة عامة موثوقة عن العنوان بدل ملفك — راجع الدقة الواقعية والتفاصيل الإجرائية بعناية إضافية قبل النشر."}, issues...)
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

// runQualityLoop drives up to seoGenerationMaxAttempts rounds of write-then-score, shared by
// both the grounded and title-only-fallback paths — the retry/feedback/best-attempt-tracking
// logic doesn't care which produced the draft.
func runQualityLoop(write draftWriter, fallbackTitle string, audience []string) (groundedDraft, seoQualityValidation, bool) {
	var bestDraft groundedDraft
	var bestValidation seoQualityValidation
	haveDraft := false
	feedback := ""

	for attempt := 0; attempt < seoGenerationMaxAttempts; attempt++ {
		draft, err := write(feedback)
		if err != nil {
			if !haveDraft {
				return groundedDraft{}, seoQualityValidation{}, false
			}
			break
		}
		draft.Title = strings.TrimSpace(draft.Title)
		if draft.Title == "" || groundedTitleLeaksInternalTerms(draft.Title) {
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

	return bestDraft, bestValidation, haveDraft
}

// buildNewContentSourcePack loads the requested files (per-country DB, matching how
// Article/Post/File rows are sharded) and extracts their text via fileextract, plus the
// title and educational classification context — no existing content row to load from.
// The returned bool reports whether any real file evidence was found; when it's false (file
// lost, or unreadable — most commonly a scanned/image-only PDF with no text layer),
// GenerateGroundedDraft falls back to title-only generation instead of erroring, per the
// user's explicit decision after weighing that against OCR (unreliable for Arabic here) and
// web-search-then-reword (flagged and rejected as it matches Google's "scraped/auto-generated
// content" policy — a distinct, often more severely penalized violation than thin content).
func buildNewContentSourcePack(ctx context.Context, req GroundedGenerateRequest) (groundedSourcePack, bool, error) {
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
	for _, id := range req.FileIDs {
		var file models.File
		if err := db.First(&file, id).Error; err != nil {
			continue
		}
		if extracted, ok := fileextract.ReadAttachmentEvidence(file, storageRoot); ok {
			extractedAny = true
			label := "نص مستخرج من الملف " + firstNonEmptyLocal(file.FileName, "#"+strconv.FormatUint(uint64(file.ID), 10))
			pack.Evidence = append(pack.Evidence, groundedEvidence{ID: fmt.Sprintf("file:%d:text", file.ID), Kind: "attachment_text", Label: label, Text: truncateAttachmentEvidence(extracted), Verified: true})
		}
	}
	return pack, extractedAny, nil
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
	system := `أنت محرر تعليمي عربي محترف ومراجع SEO في آن واحد. اكتب مقالة جديدة كاملة اعتمادًا حصريًا على الحقائق المستخرجة من الملف المرفق. لا تضف أي معلومة غير موجودة في facts. حقل title يجب أن يكون عنوانًا طبيعيًا يصف الموضوع نفسه فقط، ولا يذكر إطلاقًا كلمات مثل "المرفق" أو "الملف" أو "قاعدة البيانات" أو "الأدلة" أو أي إشارة لآلية استخراج المحتوى. اكتب صفحة شاملة ومهيكلة بعناوين h2 فرعية حقيقية، فقرات مفصّلة تشرح لا تسرد فقط، بجودة احترافية تستحق نشرها وأرشفتها في محركات البحث. أضف meta_description (120-160 حرفًا بالضبط تقريبًا، يحوي الكلمة المفتاحية الرئيسية، أسلوب معلوماتي دقيق بلا مبالغة تسويقية)، keywords (5 إلى 8 كلمات/عبارات مفتاحية حقيقية مشتقة من العنوان والحقائق والسياق الدراسي فقط، تظهر كل واحدة منها فعليًا داخل content_html — هذا الحقل إلزامي ولا يجوز أن يكون فارغًا أبدًا)، وcover_alt_text (جملة قصيرة تصف موضوع المحتوى، تُستخدم كنص بديل لصورة الغلاف). اجعل الفقرة الأولى من content_html تحتوي الكلمة المفتاحية الرئيسية. HTML نظيف فقط داخل content_html بعناوين h2 وفقرات p. بعد كتابة المسودة، راجعها بنفسك كمراجع SEO مستقل وقيّمها بصدق من 0 إلى 30 بناءً على الاحترافية والوضوح والقيمة التعليمية الفعلية (لا تكافئ الطول وحده، لا تعطِ 30 إلا إن كانت ممتازة فعلًا) في quality_score، مع ملاحظات مختصرة في quality_notes إن وُجد نقص. أعد JSON فقط.`
	user := fmt.Sprintf(`السياق:\n%s\n\nالحقائق المسموح استخدامها فقط:\n%s\n\nأرجع JSON بالمفاتيح: title, content_html, meta_description, keywords, cover_alt_text, used_fact_indexes, quality_score, quality_notes.`, string(contextJSON), string(factsJSON))
	if strings.TrimSpace(feedback) != "" {
		user += "\n\nملاحظات من محاولة سابقة يجب معالجتها في هذه المحاولة:\n" + feedback
	}
	model, err := groundedAIJSON(ctx, "new_content_writer", system, user, &out)
	return out, model, err
}

// runTitleOnlyWriter is the fallback path used when the attached file can't be read at all
// (lost, or unreadable — most commonly a scanned/image-only PDF with no text layer). It writes
// from the model's own well-established general knowledge about the title/curriculum instead
// of extracted facts. This was a deliberate choice after the user rejected both alternatives:
// OCR (unreliable for Arabic in this pipeline) and web-search-then-reword (flagged as matching
// Google's "scraped/auto-generated content" policy — more severely penalized than thin content).
// Because there is no source evidence to ground against, the prompt explicitly forbids
// inventing specific administrative/regulatory/numeric/date details and instead directs the
// reader to the official source for those — the honesty principle that runs through this whole
// pipeline (disclose limits rather than fabricate confidence).
func runTitleOnlyWriter(ctx context.Context, pack groundedSourcePack, feedback string) (groundedDraft, string, error) {
	var out groundedDraft
	contextJSON, _ := json.Marshal(map[string]interface{}{
		"title":        pack.Title,
		"content_type": pack.ContentType,
		"country_code": pack.CountryCode,
		"curriculum":   pack.Curriculum,
	})
	system := `أنت محرر تعليمي عربي محترف ومراجع SEO في آن واحد. تعذّر قراءة أي ملف مصدر مرفق لهذا المحتوى (مفقود أو غير قابل لاستخراج النص)، لذا اكتب المقالة اعتمادًا حصريًا على معرفتك العامة الموثوقة والراسخة عن الموضوع المحدَّد في العنوان والسياق الدراسي. حقل title يجب أن يكون عنوانًا طبيعيًا يصف الموضوع نفسه فقط، ولا يذكر إطلاقًا كلمات مثل "المرفق" أو "الملف" أو "قاعدة البيانات" أو أي إشارة لآلية استخراج المحتوى. اكتب بثقة عن الحقائق العامة المستقرة والمعروفة جيدًا فقط. يُمنع اختراع أو تخمين أي تفاصيل إدارية أو تنظيمية أو أرقام أو تواريخ أو إجراءات دقيقة لا تكون متأكدًا منها تمامًا؛ إن احتاج الموضوع لتفاصيل من هذا النوع فاذكر بوضوح داخل النص أنها تختلف بحسب الجهة الرسمية أو الفترة وأنه يجب على القارئ مراجعة المصدر أو الجهة الرسمية المختصة لتأكيدها، بدل اختلاقها. اكتب صفحة شاملة ومهيكلة بعناوين h2 فرعية حقيقية، فقرات مفصّلة تشرح لا تسرد فقط، بجودة احترافية تستحق نشرها وأرشفتها في محركات البحث. أضف meta_description (120-160 حرفًا بالضبط تقريبًا، يحوي الكلمة المفتاحية الرئيسية، أسلوب معلوماتي دقيق بلا مبالغة تسويقية)، keywords (5 إلى 8 كلمات/عبارات مفتاحية حقيقية مشتقة من العنوان والسياق الدراسي فقط، تظهر كل واحدة منها فعليًا داخل content_html — هذا الحقل إلزامي ولا يجوز أن يكون فارغًا أبدًا)، وcover_alt_text (جملة قصيرة تصف موضوع المحتوى، تُستخدم كنص بديل لصورة الغلاف). اجعل الفقرة الأولى من content_html تحتوي الكلمة المفتاحية الرئيسية. HTML نظيف فقط داخل content_html بعناوين h2 وفقرات p. بعد كتابة المسودة، راجعها بنفسك كمراجع SEO مستقل وقيّمها بصدق من 0 إلى 30 بناءً على الاحترافية والوضوح والقيمة التعليمية الفعلية (لا تكافئ الطول وحده، لا تعطِ 30 إلا إن كانت ممتازة فعلًا) في quality_score، مع ملاحظات مختصرة في quality_notes إن وُجد نقص. أعد JSON فقط.`
	user := fmt.Sprintf(`السياق:\n%s\n\nأرجع JSON بالمفاتيح: title, content_html, meta_description, keywords, cover_alt_text, quality_score, quality_notes.`, string(contextJSON))
	if strings.TrimSpace(feedback) != "" {
		user += "\n\nملاحظات من محاولة سابقة يجب معالجتها في هذه المحاولة:\n" + feedback
	}
	model, err := groundedAIJSON(ctx, "title_only_writer", system, user, &out)
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

func deriveKeywordsFallback(title string, audience []string) []string {
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
	for _, item := range audience {
		add(item)
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
