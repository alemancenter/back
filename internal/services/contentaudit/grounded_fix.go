package contentaudit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imanjo/fiber-api/internal/config"
	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/fileextract"
	"github.com/imanjo/fiber-api/internal/models"
	"github.com/imanjo/fiber-api/internal/utils"
	"gorm.io/gorm"
)

var (
	ErrGroundedSourceInsufficient = errors.New("لا توجد أدلة مصدرية كافية لإنشاء إصلاح موثّق لهذا المحتوى")
	ErrGroundedValidationFailed   = errors.New("فشل التحقق من توثيق مسودة الإصلاح المقترحة")
	ErrUngroundedFixPreview       = errors.New("هذه المعاينة أُنشئت بنظام إصلاح قديم لا يمكن التحقق من توثيقه، ويمكن رفضها لكن لا يمكن اعتمادها")
	ErrGroundedAIUnavailable      = errors.New("تعذّر الوصول إلى مزوّد الذكاء الاصطناعي لإنشاء إصلاح موثّق")
	ErrGroundedSourceChanged      = errors.New("تغيّر المحتوى بعد إنشاء المعاينة؛ أعد إنشاء معاينة جديدة قبل التطبيق")
)

const (
	groundingSummaryPrefix = "[GROUNDING_V2]"
	groundingMinScore      = 90
	groundingPromptVersion = "grounded-content-repair-v2"
	groundingEvidenceLimit = 12000
	// Attachments are the richest, most substantive source available (a real worksheet/PDF,
	// not just the page's own possibly-thin existing body text), so they get a higher ceiling
	// than title/body/curriculum evidence — this is what lets the writer produce genuinely
	// deep, non-"thin" pages instead of a couple of sentences when the source supports it.
	groundingAttachmentTextLimit = 20000
)

type groundedEvidence struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	Text     string `json:"text"`
	Verified bool   `json:"verified"`
}

type groundedSourcePack struct {
	ContentType string             `json:"content_type"`
	ContentID   uint               `json:"content_id"`
	CountryCode string             `json:"country_code"`
	Title       string             `json:"title"`
	URL         string             `json:"url"`
	Curriculum  string             `json:"curriculum"`
	Evidence    []groundedEvidence `json:"evidence"`
}

type groundedFact struct {
	Claim       string   `json:"claim"`
	EvidenceIDs []string `json:"evidence_ids"`
	Confidence  int      `json:"confidence"`
}

type groundedFactExtraction struct {
	Purpose            string         `json:"purpose"`
	Audience           []string       `json:"audience"`
	Facts              []groundedFact `json:"facts"`
	InsufficientSource bool           `json:"insufficient_source"`
	SourceNotes        []string       `json:"source_notes"`
}

type groundedDraft struct {
	Title           string   `json:"title"`
	ContentHTML     string   `json:"content_html"`
	MetaDescription string   `json:"meta_description"`
	Keywords        []string `json:"keywords"`
	UsedFactIndexes []int    `json:"used_fact_indexes"`
	// QualityScore/QualityNotes are only requested by grounded_generate.go's new-content
	// writer prompt (self-reported in the same call instead of a separate qualitative-review
	// round trip) — grounded_fix.go's fix writer never asks for them, so they stay zero/nil
	// there and are simply ignored.
	QualityScore int      `json:"quality_score,omitempty"`
	QualityNotes []string `json:"quality_notes,omitempty"`
	// CoverAltText is likewise only requested for new-content generation (posts have a
	// featured-image alt text field the admin otherwise has to write by hand).
	CoverAltText string `json:"cover_alt_text,omitempty"`
}

type groundedValidation struct {
	GroundingScore    int      `json:"grounding_score"`
	SupportedClaims   int      `json:"supported_claims"`
	UnsupportedClaims []string `json:"unsupported_claims"`
	Notes             []string `json:"notes"`
}

type groundingSummaryMeta struct {
	Status      string
	Score       int
	Evidence    int
	Unsupported int
	Model       string
}

type groundedLoadedContent struct {
	Type              string
	ID                uint
	CountryCode       string
	Title             string
	Content           string
	MetaDescription   *string
	Keywords          *string
	URL               string
	GradeName         string
	SubjectName       string
	SemesterName      string
	CategoryName      string
	CurriculumContext string
	Files             []models.File
}

func (s *Service) CreateGroundedFixPreview(ctx context.Context, decisionID uint64) (*models.ContentAIFixPreview, error) {
	decision, err := s.repo.GetAIDecision(ctx, decisionID)
	if err != nil {
		return nil, err
	}

	content, err := s.loadGroundedContent(ctx, decision)
	if err != nil {
		return nil, err
	}

	pack := buildGroundedSourcePack(content)
	if len(pack.Evidence) == 0 {
		return nil, ErrGroundedSourceInsufficient
	}

	facts, modelName, err := runGroundedFactExtraction(ctx, pack)
	if err != nil {
		return nil, err
	}
	facts = sanitizeGroundedFacts(facts, pack)
	if facts.InsufficientSource || len(facts.Facts) == 0 {
		detail := groundedSourceNotesSummary(facts.SourceNotes)
		if detail != "" {
			return nil, fmt.Errorf("%w: %s", ErrGroundedSourceInsufficient, detail)
		}
		return nil, fmt.Errorf("%w: لا توجد حقائق موثقة كافية في مصادر الصفحة أو المرفقات", ErrGroundedSourceInsufficient)
	}

	draft, writerModel, err := runGroundedWriter(ctx, pack, facts)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(writerModel) != "" {
		modelName = writerModel
	}
	draft = normalizeGroundedFixDraft(draft, content.Title)
	if err := validateGroundedFixDraft(draft, content.Content, len(facts.Facts)); err != nil {
		return nil, err
	}

	validation, validatorModel, err := runGroundedValidator(ctx, pack, facts, draft)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(validatorModel) != "" {
		modelName = validatorModel
	}
	validation = normalizeGroundedFixValidation(validation)

	// A validator rejection is useful feedback, not a reason to lower the safety gate.
	// Give the writer one bounded chance to remove/rephrase exactly those unsupported claims,
	// then validate the revised draft independently again. No revision can bypass the same
	// >=90 + zero-unsupported requirement used by the original draft.
	for revisionPass := 0; !groundedFixValidationPasses(validation) && revisionPass < groundedFixMaxRevisionPasses; revisionPass++ {
		revisedDraft, revisionModel, revisionErr := runGroundedFixRevision(ctx, facts, draft, validation)
		if revisionErr != nil {
			return nil, fmt.Errorf("%w: %s; revision_error=%v", ErrGroundedValidationFailed, groundedValidationFailureDetail(validation), revisionErr)
		}
		if strings.TrimSpace(revisionModel) != "" {
			modelName = revisionModel
		}
		revisedDraft = normalizeGroundedFixDraft(revisedDraft, content.Title)
		if err := validateGroundedFixDraft(revisedDraft, content.Content, len(facts.Facts)); err != nil {
			return nil, err
		}
		draft = revisedDraft

		validation, validatorModel, err = runGroundedValidator(ctx, pack, facts, draft)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(validatorModel) != "" {
			modelName = validatorModel
		}
		validation = normalizeGroundedFixValidation(validation)
	}

	if !groundedFixValidationPasses(validation) {
		return nil, fmt.Errorf("%w: %s", ErrGroundedValidationFailed, groundedValidationFailureDetail(validation))
	}

	// Smaller/cheaper writer models sometimes drop meta_description/keywords from their JSON
	// entirely despite the prompt, especially on long responses — this isn't an error the
	// validator can catch (an empty field trivially has no unsupported claims), so backfill
	// deterministically from material that's already been validated above, rather than
	// leaving these fields silently blank. No extra AI call, no new invented content.
	if draft.MetaDescription == "" {
		draft.MetaDescription = deriveMetaDescriptionFallback(draft.ContentHTML)
	}
	if len(draft.Keywords) == 0 && content.Keywords != nil {
		draft.Keywords = utils.SplitKeywords(*content.Keywords)
	}

	summary := buildGroundingSummary(validation, len(pack.Evidence), modelName, facts, pack)
	preview := &models.ContentAIFixPreview{
		DecisionID:              decision.ID,
		ContentType:             normalizeContentType(decision.ContentType),
		ContentID:               fmt.Sprintf("%s:%d", content.CountryCode, content.ID),
		CountryCode:             content.CountryCode,
		OriginalTitle:           content.Title,
		OriginalContent:         content.Content,
		OriginalMetaDescription: content.MetaDescription,
		OriginalKeywords:        content.Keywords,
		FixedTitle:              draft.Title,
		FixedContent:            draft.ContentHTML,
		FixedMetaDescription:    nonEmptyStringPtr(draft.MetaDescription),
		FixedKeywords:           nonEmptyStringPtr(strings.Join(draft.Keywords, "، ")),
		FixSummary:              summary,
		Status:                  models.AIFixStatusPreviewed,
	}
	if err := s.repo.SaveFixPreview(ctx, preview); err != nil {
		return nil, fmt.Errorf("save grounded fix preview failed: %w", err)
	}
	return preview, nil
}

func (s *Service) ApplyGroundedFix(ctx context.Context, previewID uint64, userID *uint, note string) (*models.ContentAIFixPreview, error) {
	preview, err := s.repo.GetFixPreview(ctx, previewID)
	if err != nil {
		return nil, err
	}
	if preview.Status != models.AIFixStatusPreviewed {
		return nil, ErrFixAlreadyClosed
	}
	meta, ok := parseGroundingSummary(preview.FixSummary)
	if !ok {
		return nil, ErrUngroundedFixPreview
	}
	if meta.Status != "grounded" || meta.Score < groundingMinScore || meta.Unsupported != 0 {
		return nil, fmt.Errorf("%w: score=%d unsupported=%d", ErrGroundedValidationFailed, meta.Score, meta.Unsupported)
	}
	if strings.TrimSpace(normalizePlainText(preview.FixedContent)) == "" {
		return nil, fmt.Errorf("%w: المسودة الموثقة فارغة", ErrGroundedValidationFailed)
	}

	_, _, id := normalizeContentReference(preview.ContentID, preview.CountryCode)
	db := database.GetManager().GetByCode(preview.CountryCode).WithContext(ctx)

	var notifType, notifTitle, notifMsg, notifURL string
	var authorID *uint

	// Applying now writes to two tables for an article (the row itself, plus the
	// article_keyword join table) instead of one — wrap both in a transaction so a failed
	// keyword write can no longer leave the row updated but the preview still "previewed".
	err = db.Transaction(func(tx *gorm.DB) error {
		switch normalizeContentType(preview.ContentType) {
		case "article":
			var item models.Article
			if err := tx.Preload("Subject").Preload("Subject.SchoolClass").Preload("Semester").Preload("Semester.SchoolClass").Preload("KeywordsRel").First(&item, id).Error; err != nil {
				return err
			}
			if !groundedSourceMatches(item.Title, item.Content, item.MetaDescription, articleKeywordsString(item.KeywordsRel), preview) {
				return ErrGroundedSourceChanged
			}
			item.Title = utils.SanitizeInput(preview.FixedTitle)
			item.Content = utils.SanitizeHTML(preview.FixedContent)
			if preview.FixedMetaDescription != nil {
				metaDescription := utils.SanitizeInput(*preview.FixedMetaDescription)
				item.MetaDescription = &metaDescription
			}
			if err := tx.Save(&item).Error; err != nil {
				return err
			}
			if preview.FixedKeywords != nil {
				if err := applyArticleKeywords(tx, item.ID, utils.SanitizeInput(*preview.FixedKeywords)); err != nil {
					return err
				}
			}
			authorID = item.AuthorID
			notifType = `App\Notifications\ArticleUpdatedByAI`
			notifTitle = fmt.Sprintf("تم تحديث المقالة: %s", shortNotificationTitle(item.Title, 70))
			notifMsg = fmt.Sprintf("تم اعتماد تحسين موثق بالأدلة وتحديث المقالة: %s", item.Title)
			notifURL = contentAuditEditURL("article", item.ID, preview.CountryCode)
		case "post":
			var item models.Post
			if err := tx.Preload("Category").First(&item, id).Error; err != nil {
				return err
			}
			if !groundedSourceMatches(item.Title, item.Content, item.MetaDescription, item.Keywords, preview) {
				return ErrGroundedSourceChanged
			}
			item.Title = utils.SanitizeInput(preview.FixedTitle)
			item.Content = utils.StripBlockedLinks(utils.SanitizeHTML(preview.FixedContent))
			if preview.FixedMetaDescription != nil {
				metaDescription := utils.SanitizeInput(*preview.FixedMetaDescription)
				item.MetaDescription = &metaDescription
			}
			if preview.FixedKeywords != nil {
				keywords := utils.SanitizeInput(*preview.FixedKeywords)
				item.Keywords = &keywords
			}
			if err := tx.Save(&item).Error; err != nil {
				return err
			}
			authorID = item.AuthorID
			notifType = `App\Notifications\PostUpdatedByAI`
			notifTitle = fmt.Sprintf("تم تحديث المنشور: %s", shortNotificationTitle(item.Title, 70))
			notifMsg = fmt.Sprintf("تم اعتماد تحسين موثق بالأدلة وتحديث المنشور: %s", item.Title)
			notifURL = contentAuditEditURL("post", item.ID, preview.CountryCode)
		default:
			return ErrUnsupportedContentType
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	now := time.Now()
	preview.Status = models.AIFixStatusApplied
	preview.AppliedByUserID = userID
	preview.AppliedAt = &now
	if err := s.repo.UpdateFixPreview(ctx, preview); err != nil {
		return nil, err
	}
	_ = s.repo.CreateApprovalLog(ctx, &models.ContentAIApprovalLog{FixPreviewID: preview.ID, DecisionID: preview.DecisionID, Action: models.AIFixStatusApplied, UserID: userID, Note: strings.TrimSpace(note)})

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

func (s *Service) loadGroundedContent(ctx context.Context, decision *models.ContentAIDecision) (*groundedLoadedContent, error) {
	cc, _, id := normalizeContentReference(decision.ContentID, decision.CountryCode)
	if id == 0 {
		return nil, strconv.ErrSyntax
	}
	db := database.GetManager().GetByCode(cc).WithContext(ctx)
	switch normalizeContentType(decision.ContentType) {
	case "article":
		var item models.Article
		if err := db.Preload("Subject").Preload("Subject.SchoolClass").Preload("Semester").Preload("Semester.SchoolClass").Preload("Files").Preload("KeywordsRel").First(&item, id).Error; err != nil {
			return nil, err
		}
		gradeName, subjectName, semesterName := articleEducationContext(item)
		return &groundedLoadedContent{
			Type:              "article",
			ID:                item.ID,
			CountryCode:       cc,
			Title:             item.Title,
			Content:           item.Content,
			MetaDescription:   item.MetaDescription,
			Keywords:          articleKeywordsString(item.KeywordsRel),
			URL:               fmt.Sprintf("/%s/lesson/articles/%d", cc, item.ID),
			GradeName:         gradeName,
			SubjectName:       subjectName,
			SemesterName:      semesterName,
			CurriculumContext: buildCurriculumContext(cc, gradeName, subjectName, semesterName, ""),
			Files:             item.Files,
		}, nil
	case "post":
		var item models.Post
		if err := db.Preload("Category").Preload("Files").First(&item, id).Error; err != nil {
			return nil, err
		}
		categoryName := ""
		if item.Category != nil {
			categoryName = strings.TrimSpace(item.Category.Name)
		}
		return &groundedLoadedContent{
			Type:              "post",
			ID:                item.ID,
			CountryCode:       cc,
			Title:             item.Title,
			Content:           item.Content,
			MetaDescription:   item.MetaDescription,
			Keywords:          item.Keywords,
			URL:               fmt.Sprintf("/%s/posts/%d", cc, item.ID),
			CategoryName:      categoryName,
			CurriculumContext: buildCurriculumContext(cc, "", "", "", categoryName),
			Files:             item.Files,
		}, nil
	default:
		return nil, ErrUnsupportedContentType
	}
}

// applyArticleKeywords mirrors internal/repositories/article_repository.go's UpdateKeywords —
// same FirstOrCreate-per-keyword + Association("KeywordsRel").Replace(...) pattern, reused
// verbatim here (rather than imported) because this call already has a transaction (tx) open
// and Article's keywords are a many2many relation, unlike Post's plain string column.
func applyArticleKeywords(tx *gorm.DB, articleID uint, keywordsStr string) error {
	keywordList := utils.SplitKeywords(keywordsStr)
	if len(keywordList) == 0 {
		return tx.Model(&models.Article{ID: articleID}).Association("KeywordsRel").Clear()
	}
	var keywords []models.Keyword
	for _, kw := range keywordList {
		var keyword models.Keyword
		if err := tx.Where("keyword = ?", kw).FirstOrCreate(&keyword, models.Keyword{Keyword: kw}).Error; err == nil {
			keywords = append(keywords, keyword)
		}
	}
	return tx.Model(&models.Article{ID: articleID}).Association("KeywordsRel").Replace(keywords)
}

// deriveMetaDescriptionFallback builds a meta description straight from content_html when
// the writer's own JSON omitted one. Grounded by construction — it's a literal excerpt of
// text the grounding validator already reviewed and approved, not new/invented material.
func deriveMetaDescriptionFallback(contentHTML string) string {
	const targetLength = 155
	plain := strings.Join(strings.Fields(normalizePlainText(contentHTML)), " ")
	runes := []rune(plain)
	if len(runes) <= targetLength {
		return plain
	}
	cut := string(runes[:targetLength])
	if lastSpace := strings.LastIndex(cut, " "); lastSpace > 0 {
		cut = cut[:lastSpace]
	}
	return strings.TrimSpace(cut) + "…"
}

// nonEmptyStringPtr returns nil for a blank value instead of a pointer to an empty string,
// matching the nullable-column convention used throughout this model (omitempty on save).
func nonEmptyStringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

// articleKeywordsString mirrors the plain comma-separated Keywords convention Post already
// uses natively, so groundedLoadedContent.Keywords stays a single *string across both content
// types regardless of Article's many2many KeywordsRel relation underneath.
func articleKeywordsString(keywords []models.Keyword) *string {
	if len(keywords) == 0 {
		return nil
	}
	names := make([]string, 0, len(keywords))
	for _, kw := range keywords {
		if name := strings.TrimSpace(kw.Keyword); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	joined := strings.Join(names, "، ")
	return &joined
}

func groundedSourceMatches(title, content string, metaDescription, keywords *string, preview *models.ContentAIFixPreview) bool {
	if preview == nil {
		return false
	}
	return title == preview.OriginalTitle &&
		content == preview.OriginalContent &&
		stringPointerValue(metaDescription) == stringPointerValue(preview.OriginalMetaDescription) &&
		normalizedGroundedKeywords(keywords) == normalizedGroundedKeywords(preview.OriginalKeywords)
}

func normalizedGroundedKeywords(value *string) string {
	if value == nil {
		return ""
	}
	keywords := utils.SplitKeywords(*value)
	for index := range keywords {
		keywords[index] = strings.ToLower(strings.TrimSpace(keywords[index]))
	}
	sort.Strings(keywords)
	return strings.Join(keywords, "\x00")
}

func buildGroundedSourcePack(content *groundedLoadedContent) groundedSourcePack {
	pack := groundedSourcePack{
		ContentType: content.Type,
		ContentID:   content.ID,
		CountryCode: content.CountryCode,
		Title:       strings.TrimSpace(content.Title),
		URL:         content.URL,
		Curriculum:  strings.TrimSpace(content.CurriculumContext),
	}
	if pack.Title != "" {
		pack.Evidence = append(pack.Evidence, groundedEvidence{ID: "content:title", Kind: "title", Label: "عنوان الصفحة", Text: pack.Title, Verified: true})
	}
	if body := truncateGroundingEvidence(normalizePlainText(content.Content)); body != "" {
		// Existing body text may contain legacy/generated claims. Keep it available
		// for editorial context, but never allow it to support grounded facts by itself.
		pack.Evidence = append(pack.Evidence, groundedEvidence{ID: "content:body", Kind: "body_context", Label: "النص الحالي (سياق فقط)", Text: body, Verified: false})
	}
	if pack.Curriculum != "" {
		pack.Evidence = append(pack.Evidence, groundedEvidence{ID: "content:curriculum", Kind: "curriculum", Label: "السياق الدراسي من قاعدة البيانات", Text: pack.Curriculum, Verified: true})
	}

	files := append([]models.File(nil), content.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].ID < files[j].ID })
	for _, file := range files {
		meta := attachmentMetadata(file)
		pack.Evidence = append(pack.Evidence, groundedEvidence{ID: fmt.Sprintf("attachment:%d:meta", file.ID), Kind: "attachment_metadata", Label: "بيانات المرفق " + firstNonEmptyLocal(file.FileName, filepath.Base(file.FilePath)), Text: meta, Verified: true})
		if extracted, ok := fileextract.ReadAttachmentEvidence(file, config.Get().Storage.Path); ok {
			pack.Evidence = append(pack.Evidence, groundedEvidence{ID: fmt.Sprintf("attachment:%d:text", file.ID), Kind: "attachment_text", Label: "نص مستخرج من المرفق " + firstNonEmptyLocal(file.FileName, filepath.Base(file.FilePath)), Text: truncateAttachmentEvidence(extracted), Verified: true})
		}
	}
	return pack
}

func attachmentMetadata(file models.File) string {
	parts := []string{}
	if name := strings.TrimSpace(file.FileName); name != "" {
		parts = append(parts, "اسم الملف: "+name)
	}
	if t := strings.TrimSpace(file.FileType); t != "" {
		parts = append(parts, "نوع الملف: "+t)
	}
	if file.FileCategory != nil && strings.TrimSpace(*file.FileCategory) != "" {
		parts = append(parts, "تصنيف الملف: "+strings.TrimSpace(*file.FileCategory))
	}
	if mime := strings.TrimSpace(file.MimeType); mime != "" {
		parts = append(parts, "MIME: "+mime)
	}
	if file.FileSize > 0 {
		parts = append(parts, fmt.Sprintf("الحجم: %d بايت", file.FileSize))
	}
	if len(parts) == 0 {
		parts = append(parts, "يوجد مرفق مرتبط بهذه الصفحة")
	}
	return strings.Join(parts, " | ")
}

func truncateGroundingEvidence(value string) string {
	return truncateGroundingEvidenceTo(value, groundingEvidenceLimit)
}

func truncateAttachmentEvidence(value string) string {
	return truncateGroundingEvidenceTo(value, groundingAttachmentTextLimit)
}

func truncateGroundingEvidenceTo(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len([]rune(value)) <= limit {
		return value
	}
	return string([]rune(value)[:limit]) + "…"
}

func sanitizeGroundedFacts(in groundedFactExtraction, pack groundedSourcePack) groundedFactExtraction {
	validEvidence := map[string]bool{}
	for _, evidence := range pack.Evidence {
		validEvidence[evidence.ID] = evidence.Verified
	}
	cleanFacts := make([]groundedFact, 0, len(in.Facts))
	for _, fact := range in.Facts {
		fact.Claim = strings.TrimSpace(fact.Claim)
		if fact.Claim == "" {
			continue
		}
		ids := []string{}
		seen := map[string]bool{}
		for _, id := range fact.EvidenceIDs {
			id = strings.TrimSpace(id)
			if id != "" && validEvidence[id] && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			continue
		}
		fact.EvidenceIDs = ids
		if fact.Confidence < 0 {
			fact.Confidence = 0
		}
		if fact.Confidence > 100 {
			fact.Confidence = 100
		}
		cleanFacts = append(cleanFacts, fact)
	}
	in.Facts = cleanFacts
	in.Audience = compactStrings(in.Audience)
	in.SourceNotes = compactStrings(in.SourceNotes)
	if len(in.Facts) == 0 {
		in.InsufficientSource = true
	}
	return in
}

func validUsedFactIndexes(indexes []int, factCount int) bool {
	if factCount <= 0 || len(indexes) == 0 {
		return false
	}
	for _, index := range indexes {
		if index < 0 || index >= factCount {
			return false
		}
	}
	return true
}

func compactStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func groundedSourceNotesSummary(notes []string) string {
	notes = compactStrings(notes)
	if len(notes) == 0 {
		return ""
	}
	if len(notes) > 2 {
		notes = notes[:2]
	}
	for i := range notes {
		r := []rune(notes[i])
		if len(r) > 240 {
			notes[i] = string(r[:240]) + "…"
		}
	}
	return strings.Join(notes, " | ")
}

func runGroundedFactExtraction(ctx context.Context, pack groundedSourcePack) (groundedFactExtraction, string, error) {
	var out groundedFactExtraction
	packJSON, _ := json.Marshal(pack)
	system := `أنت مدقق مصادر لمحتوى تعليمي. لا تكتب المقال. استخرج فقط حقائق صريحة يمكن إثباتها من الأدلة التي verified=true. لا تستخدم أي دليل verified=false لدعم حقيقة. كل حقيقة يجب أن تشير إلى evidence_ids صحيحة وموثقة. ممنوع الاستنتاج من عنوان الصفحة وحده أن المرفق يحتوي أهدافًا أو أنشطة أو أسئلة أو حلولًا ما لم يذكر دليل موثق ذلك. إذا لم تكف الأدلة لإنشاء صفحة مفيدة ودقيقة، اجعل insufficient_source=true. أعد JSON فقط، بلا Markdown وبلا شرح خارج JSON.`
	user := fmt.Sprintf(`نسخة البرومبت: %s\nحزمة المصدر:\n%s\n\nأرجع JSON بالمفاتيح فقط: purpose, audience, facts, insufficient_source, source_notes. facts عنصره: claim, evidence_ids, confidence. قيود الإخراج: حد أقصى 18 حقيقة (استخرج كل الحقائق الموثقة المتاحة حتى هذا الحد، لا تكتفِ بعدد قليل إذا كانت الأدلة تدعم أكثر)؛ كل claim مختصر (نحو 240 حرفًا أو أقل)؛ audience بحد أقصى 4 عناصر؛ source_notes بحد أقصى عنصرين مختصرين.`, groundingPromptVersion, string(packJSON))
	model, err := groundedAIJSON(ctx, "fact_extractor", system, user, &out)
	return out, model, err
}

func runGroundedWriter(ctx context.Context, pack groundedSourcePack, facts groundedFactExtraction) (groundedDraft, string, error) {
	var out groundedDraft
	factsJSON, _ := json.Marshal(facts)
	contextJSON, _ := json.Marshal(map[string]interface{}{
		"title":        pack.Title,
		"content_type": pack.ContentType,
		"country_code": pack.CountryCode,
		"curriculum":   pack.Curriculum,
	})
	system := `أنت محرر تعليمي عربي. اكتب مسودة مفيدة ودقيقة اعتمادًا حصريًا على الحقائق المستخرجة. لا تضف أي معلومة غير موجودة في facts. لا تطارد عدد كلمات ولا تضف حشو SEO. إذا كانت الأدلة محدودة فاكتب صفحة قصيرة صادقة بدل اختراع تفاصيل. أما إذا كانت الحقائق كافية وغنية، فاكتب صفحة شاملة ومهيكلة تغطي كل حقيقة متاحة بعمق حقيقي: عناوين h2 فرعية لكل محور، فقرات مفصّلة تشرح لا تسرد فقط — الهدف صفحة تستحق فهرستها في محركات البحث، لا فقرة مختصرة، لكن دون تجاوز حدود ما تثبته الحقائق. لا تقل إن الملف يحتوي أهدافًا أو أنشطة أو أسئلة أو حلولًا إلا إذا كانت حقيقة صريحة. اجعل الملف جزءًا من المحتوى لا مجرد غلاف له: اشرح ما هو المورد، لمن يفيد، وما الذي يمكن إثباته عنه، ثم وجّه القارئ للاستفادة من المرفق — لكن هذا خاص بـ content_html فقط. قواعد صارمة لحقل title تحديدًا: العنوان نص يراه القارئ العام مباشرة، يجب أن يصف موضوع المحتوى نفسه بشكل طبيعي وواضح، ويطابق الصف/المادة/الفصل الفعليين المذكورين في السياق دون خلط بينها وبين أي صف أو مادة أخرى. ممنوع تمامًا أن يذكر العنوان كلمات مثل "المرفق" أو "الملف" أو "قاعدة البيانات" أو "الأدلة" أو "المصدر" أو أي إشارة لآلية استخراج المحتوى أو معالجته الداخلية — هذه المصطلحات تخص المعالجة الداخلية فقط ولا يجوز أن تظهر في نص يقرأه المستخدم. التزم بالعنوان الأصلي المُعطى في السياق ما أمكن، ولا تُعدّله إلا لتصحيح خطأ واضح فيه، وحتى عندها حافظ على أسلوبه الطبيعي. أعد JSON فقط.`
	user := fmt.Sprintf(`السياق:\n%s\n\nالحقائق المسموح استخدامها فقط:\n%s\n\nأرجع JSON بالمفاتيح: title, content_html, meta_description, keywords, used_fact_indexes. used_fact_indexes هي فهارس الحقائق المستخدمة (تبدأ من 0). اجعل المسودة مركزة ولا تكرر الحقائق، لكن استخدم كل حقيقة متاحة ذات صلة بدل الاقتصار على جزء منها. تذكير: title يجب أن يبقى قريبًا من العنوان الأصلي في السياق أعلاه ومطابقًا لصفه ومادته، ولا يذكر إطلاقًا "المرفق" أو "الملف" أو "قاعدة البيانات" أو أي مصطلح معالجة داخلية. تذكير أخير: meta_description وkeywords حقلان إلزاميان تمامًا مثل content_html — لا تُرجع أيًا منهما فارغًا أبدًا.`, string(contextJSON), string(factsJSON))
	model, err := groundedAIJSON(ctx, "grounded_writer", system, user, &out)
	return out, model, err
}

func runGroundedValidator(ctx context.Context, pack groundedSourcePack, facts groundedFactExtraction, draft groundedDraft) (groundedValidation, string, error) {
	var out groundedValidation
	payload, _ := json.Marshal(map[string]interface{}{"source_pack": pack, "facts": facts, "draft": draft})
	system := `أنت مدقق ادعاءات صارم. قارن كل ادعاء واقعي في المسودة — بما في ذلك title وcontent_html وmeta_description وkeywords معًا، لا content_html فقط — مع الحقائق وحزمة الأدلة. الأدلة التي verified=false سياق فقط ولا يجوز اعتبارها مصدر إثبات. اعتبر أي معلومة غير مثبتة unsupported حتى لو بدت منطقية تربويًا، بما في ذلك أي كلمة مفتاحية تشير لموضوع غير مذكور في الحقائق. لا تكافئ طول النص. grounding_score يقيس نسبة الادعاءات المدعومة ودقتها من 0 إلى 100. إذا وجد ادعاء مهم غير مدعوم فاذكره باختصار في unsupported_claims. أعد JSON فقط.`
	user := fmt.Sprintf(`راجع هذه البيانات:\n%s\n\nأرجع JSON بالمفاتيح: grounding_score, supported_claims, unsupported_claims, notes. اجعل notes مختصرة.`, string(payload))
	model, err := groundedAIJSON(ctx, "claim_validator", system, user, &out)
	return out, model, err
}

func groundedAIJSON(ctx context.Context, stage, systemPrompt, userPrompt string, out interface{}) (string, error) {
	return groundedAIJSONV3(ctx, stage, systemPrompt, userPrompt, out)
}

// Kept for package compatibility; new requests should use the context-aware order.
func groundedModelCandidates() []string {
	return groundedModelCandidatesForContext(context.Background())
}

func cleanGroundedJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	if start := strings.Index(raw, "{"); start >= 0 {
		if end := strings.LastIndex(raw, "}"); end > start {
			raw = raw[start : end+1]
		}
	}
	return raw
}

func buildGroundingSummary(validation groundedValidation, evidenceCount int, model string, facts groundedFactExtraction, pack groundedSourcePack) string {
	status := "grounded"
	labels := []string{}
	for _, evidence := range pack.Evidence {
		if !evidence.Verified {
			continue
		}
		labels = append(labels, evidence.Label)
	}
	if len(labels) > 5 {
		labels = labels[:5]
	}
	return fmt.Sprintf("%s status=%s score=%d evidence=%d unsupported=%d model=%s prompt=%s\nتم إنشاء المسودة من أدلة المصدر الموثقة فقط، ثم فحص الادعاءات بشكل مستقل. الغرض المستخلص: %s. مصادر بارزة: %s. لا يوجد حد كلمات إجباري؛ الأولوية للدقة والفائدة.", groundingSummaryPrefix, status, validation.GroundingScore, evidenceCount, len(validation.UnsupportedClaims), safeGroundingToken(model), groundingPromptVersion, strings.TrimSpace(facts.Purpose), strings.Join(labels, "، "))
}

var groundingSummaryPattern = regexp.MustCompile(`^\[GROUNDING_V2\]\s+status=([^\s]+)\s+score=(\d+)\s+evidence=(\d+)\s+unsupported=(\d+)\s+model=([^\s]+)`)

func parseGroundingSummary(summary string) (groundingSummaryMeta, bool) {
	matches := groundingSummaryPattern.FindStringSubmatch(strings.TrimSpace(summary))
	if len(matches) != 6 {
		return groundingSummaryMeta{}, false
	}
	score, err1 := strconv.Atoi(matches[2])
	evidence, err2 := strconv.Atoi(matches[3])
	unsupported, err3 := strconv.Atoi(matches[4])
	if err1 != nil || err2 != nil || err3 != nil {
		return groundingSummaryMeta{}, false
	}
	return groundingSummaryMeta{Status: matches[1], Score: score, Evidence: evidence, Unsupported: unsupported, Model: matches[5]}, true
}

func safeGroundingToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	value = strings.ReplaceAll(value, " ", "_")
	value = strings.ReplaceAll(value, "\n", "_")
	return value
}
