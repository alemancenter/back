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
