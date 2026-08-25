package contentaudit

import (
	"context"
	"errors"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/imanjo/fiber-api/internal/contentquality"
	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/models"
	coreai "github.com/imanjo/fiber-api/internal/services"
	"github.com/imanjo/fiber-api/internal/utils"
)

var (
	ErrSafeMetadataValidation = errors.New("فشل التحقق من الوصف التعريفي المقترح")
	ErrSafeMetadataStale      = errors.New("تغيّر المحتوى بعد إنشاء معاينة الوصف؛ أعد تشغيل الإصلاح")
	ErrMetadataNoRepairNeeded = errors.New("الوصف التعريفي الحالي يستوفي الحد الداخلي ولا يحتاج إصلاحًا تلقائيًا")
)

const (
	safeMetadataSummaryPrefix = "[SAFE_METADATA_V1]"
	safeMetadataMinChars      = 100
	safeMetadataMaxChars      = 180
	safeMetadataTargetChars   = 160
)

var safeMetadataURLPattern = regexp.MustCompile(`(?i)(https?://|www\.)`)

// AutoRepairMetaDescription creates a persisted, auditable preview and applies
// only its meta_description field. Title, body, keywords and publication state
// are guarded against mutation by this path.
func (s *Service) AutoRepairMetaDescription(ctx context.Context, decisionID uint64, userID *uint) (*models.ContentAIFixPreview, error) {
	decision, err := s.repo.GetAIDecision(ctx, decisionID)
	if err != nil {
		return nil, err
	}
	cc, normalizedID, numericID := normalizeContentReference(decision.ContentID, decision.CountryCode)
	if numericID == 0 {
		return nil, fmt.Errorf("%w: content id is missing", ErrSafeMetadataValidation)
	}

	lockKey := fmt.Sprintf("safe-meta:%s:%s", normalizeContentType(decision.ContentType), normalizedID)
	unlock, locked := acquireContentAILock(ctx, lockKey, 3*time.Minute)
	if !locked {
		return nil, ErrAIAnalysisInProgress
	}
	defer unlock()

	content, err := s.loadContentByRef(ctx, decision.ContentType, cc, numericID)
	if err != nil {
		return nil, err
	}
	if utf8.RuneCountInString(strings.TrimSpace(content.MetaDescription)) >= contentquality.DiagnosticMetaMinChars {
		return nil, ErrMetadataNoRepairNeeded
	}

	candidate, provider, err := s.generateSafeMetaDescription(ctx, content)
	if err != nil {
		return nil, err
	}
	originalMeta := content.MetaDescription
	preview := &models.ContentAIFixPreview{
		DecisionID:              decision.ID,
		ContentType:             normalizeContentType(decision.ContentType),
		ContentID:               normalizedID,
		CountryCode:             cc,
		OriginalTitle:           content.Title,
		OriginalContent:         content.Content,
		OriginalMetaDescription: &originalMeta,
		FixedTitle:              content.Title,
		FixedContent:            content.Content,
		FixedMetaDescription:    nonEmptyStringPtr(candidate),
		FixSummary:              fmt.Sprintf("%s source=current_page provider=%s fields=meta_description", safeMetadataSummaryPrefix, provider),
		Status:                  models.AIFixStatusPreviewed,
	}
	if err := s.repo.SaveFixPreview(ctx, preview); err != nil {
		return nil, fmt.Errorf("save safe metadata preview failed: %w", err)
	}
	return s.applySafeMetadataFix(ctx, preview, userID)
}

func (s *Service) generateSafeMetaDescription(ctx context.Context, content *loadedContent) (string, string, error) {
	plain := normalizePlainText(content.Content)
	if strings.TrimSpace(content.Title+plain) == "" {
		return "", "", fmt.Errorf("%w: مصدر الصفحة فارغ", ErrSafeMetadataValidation)
	}

	if s.ai != nil {
		response, err := s.ai.RunContentIntelligence(ctx, coreai.ContentIntelligenceRequest{
			Task:              "repair_meta_description",
			ModelStrategy:     firstNonEmptyLocal(aiModelStrategyFromContext(ctx), "balanced"),
			ContentType:       content.Type,
			ContentID:         fmt.Sprintf("%d", content.ID),
			CountryCode:       content.CountryCode,
			GradeName:         content.GradeName,
			SubjectName:       content.SubjectName,
			SemesterName:      content.SemesterName,
			CategoryName:      content.CategoryName,
			CurriculumContext: content.CurriculumContext,
			Title:             content.Title,
			Content:           content.Content,
			PlainText:         plain,
			MetaDescription:   content.MetaDescription,
			URL:               content.URL,
			Language:          "ar",
			JobID:             aiJobIDFromContext(ctx),
		})
		if err == nil {
			if candidate, validateErr := validateSafeMetaDescription(response.FixedMetaDescription, content.Title, plain); validateErr == nil {
				return candidate, firstNonEmptyLocal(response.Model, response.Provider, "ai"), nil
			}
		}
	}

	fallback := deriveSafeMetaDescription(content.Title, plain)
	candidate, err := validateSafeMetaDescription(fallback, content.Title, plain)
	if err != nil {
		return "", "", err
	}
	return candidate, "extractive_fallback", nil
}

func (s *Service) applySafeMetadataFix(ctx context.Context, preview *models.ContentAIFixPreview, userID *uint) (*models.ContentAIFixPreview, error) {
	if preview == nil || preview.Status != models.AIFixStatusPreviewed {
		return nil, ErrFixAlreadyClosed
	}
	if !strings.HasPrefix(strings.TrimSpace(preview.FixSummary), safeMetadataSummaryPrefix) ||
		preview.FixedMetaDescription == nil ||
		preview.FixedTitle != preview.OriginalTitle ||
		preview.FixedContent != preview.OriginalContent {
		return nil, ErrSafeMetadataValidation
	}
	candidate, err := validateSafeMetaDescription(*preview.FixedMetaDescription, preview.OriginalTitle, normalizePlainText(preview.OriginalContent))
	if err != nil {
		return nil, err
	}

	_, _, id := normalizeContentReference(preview.ContentID, preview.CountryCode)
	db := database.GetManager().GetByCode(preview.CountryCode).WithContext(ctx)
	switch normalizeContentType(preview.ContentType) {
	case "article":
		var item models.Article
		if err := db.First(&item, id).Error; err != nil {
			return nil, err
		}
		if !safeMetadataSourceMatches(item.Title, item.Content, item.MetaDescription, preview) {
			return nil, ErrSafeMetadataStale
		}
		result := db.Model(&models.Article{}).Where("id = ? AND updated_at = ?", item.ID, item.UpdatedAt).Update("meta_description", candidate)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected != 1 {
			return nil, ErrSafeMetadataStale
		}
	case "post":
		var item models.Post
		if err := db.First(&item, id).Error; err != nil {
			return nil, err
		}
		if !safeMetadataSourceMatches(item.Title, item.Content, item.MetaDescription, preview) {
			return nil, ErrSafeMetadataStale
		}
		result := db.Model(&models.Post{}).Where("id = ? AND updated_at = ?", item.ID, item.UpdatedAt).Update("meta_description", candidate)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected != 1 {
			return nil, ErrSafeMetadataStale
		}
	default:
		return nil, ErrUnsupportedContentType
	}

	now := time.Now()
	preview.FixedMetaDescription = &candidate
	preview.Status = models.AIFixStatusApplied
	preview.AppliedByUserID = userID
	preview.AppliedAt = &now
	if err := s.repo.UpdateFixPreview(ctx, preview); err != nil {
		return nil, err
	}
	_ = s.repo.CreateApprovalLog(ctx, &models.ContentAIApprovalLog{
		FixPreviewID: preview.ID,
		DecisionID:   preview.DecisionID,
		Action:       models.AIFixStatusApplied,
		UserID:       userID,
		Note:         "إصلاح تلقائي آمن للوصف التعريفي فقط؛ لم يُغيّر العنوان أو المحتوى أو الكلمات المفتاحية.",
	})
	return preview, nil
}

func safeMetadataSourceMatches(title, content string, meta *string, preview *models.ContentAIFixPreview) bool {
	return title == preview.OriginalTitle &&
		content == preview.OriginalContent &&
		stringPointerValue(meta) == stringPointerValue(preview.OriginalMetaDescription)
}

func stringPointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func validateSafeMetaDescription(value, title, plainText string) (string, error) {
	raw := strings.TrimSpace(html.UnescapeString(value))
	if raw == "" || strings.ContainsAny(raw, "<>") || safeMetadataURLPattern.MatchString(raw) {
		return "", ErrSafeMetadataValidation
	}
	candidate := strings.Join(strings.Fields(utils.SanitizeInput(raw)), " ")
	length := utf8.RuneCountInString(candidate)
	if length < safeMetadataMinChars || length > safeMetadataMaxChars {
		return "", fmt.Errorf("%w: يجب أن يكون الوصف بين %d و%d حرفًا", ErrSafeMetadataValidation, safeMetadataMinChars, safeMetadataMaxChars)
	}
	if contentquality.ContainsReplacementArtifact(candidate) {
		return "", ErrSafeMetadataValidation
	}

	source := strings.Join(strings.Fields(title+" "+plainText), " ")
	candidateTokens := meaningfulMetadataTokens(candidate)
	sourceTokens := meaningfulMetadataTokens(source)
	overlap := 0
	for token := range candidateTokens {
		if _, ok := sourceTokens[token]; ok {
			overlap++
		}
	}
	minimumOverlap := 2
	if len(candidateTokens) >= 6 {
		minimumOverlap = 3
	}
	if len(candidateTokens) >= 4 && (overlap < minimumOverlap || overlap*100/len(candidateTokens) < 40) {
		return "", fmt.Errorf("%w: الوصف لا يستند بما يكفي إلى نص الصفحة", ErrSafeMetadataValidation)
	}
	sourceNumbers := numericMetadataTokens(source)
	for number := range numericMetadataTokens(candidate) {
		if _, ok := sourceNumbers[number]; !ok {
			return "", fmt.Errorf("%w: يحتوي الوصف على رقم غير موجود في المصدر", ErrSafeMetadataValidation)
		}
	}
	return candidate, nil
}

func deriveSafeMetaDescription(title, plainText string) string {
	source := strings.Join(strings.Fields(strings.TrimSpace(title)+" — "+strings.TrimSpace(plainText)), " ")
	if utf8.RuneCountInString(source) <= safeMetadataTargetChars {
		return source
	}
	runes := []rune(source)
	cutRunes := runes[:safeMetadataTargetChars]
	lastSpace := -1
	for index, value := range cutRunes {
		if unicode.IsSpace(value) {
			lastSpace = index
		}
	}
	if lastSpace >= safeMetadataMinChars {
		cutRunes = cutRunes[:lastSpace]
	}
	cut := strings.TrimSpace(string(cutRunes))
	return strings.TrimRight(cut, "،؛:.-— ")
}

func meaningfulMetadataTokens(value string) map[string]struct{} {
	tokens := make(map[string]struct{})
	for _, token := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		if utf8.RuneCountInString(token) < 3 {
			continue
		}
		tokens[token] = struct{}{}
	}
	return tokens
}

func numericMetadataTokens(value string) map[string]struct{} {
	tokens := make(map[string]struct{})
	for _, token := range strings.FieldsFunc(value, func(r rune) bool { return !unicode.IsNumber(r) }) {
		if token != "" {
			tokens[token] = struct{}{}
		}
	}
	return tokens
}
