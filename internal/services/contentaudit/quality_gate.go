package contentaudit

import (
	"context"
	"errors"
	"strings"

	"github.com/imanjo/fiber-api/internal/contentquality"
	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/models"
	"gorm.io/gorm"
)

const (
	QualityGateApprovedMinScore  = contentquality.ApprovedMinScore
	QualityGateDecisionUnaudited = contentquality.DecisionUnaudited
	QualityGateRiskUnknown       = contentquality.RiskUnknown
)

// ContentQualityGate is kept as a public alias for existing content-audit callers.
// The canonical policy implementation lives in internal/contentquality so sitemap,
// SEO and ad-status enforcement all consume the exact same decision logic.
type ContentQualityGate = contentquality.Gate

func UnauditedQualityGate() ContentQualityGate {
	return contentquality.Unaudited()
}

func EvaluateQualityGate(decision *models.ContentAIDecision) ContentQualityGate {
	return contentquality.Evaluate(decision)
}

// QualityGate evaluates the latest saved audit decision, checks current source
// corruption, then applies the latest safe human editorial override. A current
// corruption finding or explicit NOINDEX classification wins over an older AI
// approval. KEEP/IMPROVE/MERGE_301 remain workflow labels and do not silently
// change public SEO behavior.
func (s *Service) QualityGate(ctx context.Context, contentType, contentID, countryCode string) (ContentQualityGate, error) {
	gate := UnauditedQualityGate()
	decision, err := s.LatestAIDecision(ctx, contentType, contentID, countryCode)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return ContentQualityGate{}, err
		}
	} else {
		gate = EvaluateQualityGate(decision)
	}

	cc, _, numericID := normalizeContentReference(contentID, countryCode)
	normalizedType := normalizeContentType(contentType)
	artifacts, err := currentSourceReplacementArtifacts(ctx, normalizedType, cc, numericID)
	if err != nil {
		return ContentQualityGate{}, err
	}
	gate = contentquality.ApplyReplacementArtifactGuard(gate, artifacts)

	editorial, editorialErr := s.LatestEditorialDecision(ctx, normalizedType, numericID, cc)
	editorialName, editorialErr := editorialDecisionOrEmpty(editorial, editorialErr)
	if editorialErr != nil {
		return ContentQualityGate{}, editorialErr
	}
	return contentquality.ApplyEditorialDecision(gate, editorialName), nil
}

// currentSourceReplacementArtifacts reads the exact fields that can surface in
// the public page/SEO output. This avoids a stale AI decision hiding corruption
// introduced later, and keeps title/meta/keywords protection aligned with the
// dashboard scanner and sitemap filter.
func currentSourceReplacementArtifacts(ctx context.Context, contentType, countryCode string, id uint) ([]contentquality.ReplacementArtifact, error) {
	db := database.GetManager().GetByCode(countryCode).WithContext(ctx)
	type source struct {
		Title           string `gorm:"column:title"`
		Content         string `gorm:"column:content"`
		MetaDescription string `gorm:"column:meta_description"`
		Keywords        string `gorm:"column:keywords"`
	}
	var row source
	var err error
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "article":
		err = db.Table("articles").
			Select("title, content, COALESCE(meta_description, '') AS meta_description, '' AS keywords").
			Where("id = ?", id).
			Take(&row).Error
	case "post":
		err = db.Table("posts").
			Select("title, content, COALESCE(meta_description, '') AS meta_description, COALESCE(keywords, '') AS keywords").
			Where("id = ?", id).
			Take(&row).Error
	default:
		return nil, ErrUnsupportedContentType
	}
	if err != nil {
		return nil, err
	}
	return contentquality.DetectReplacementArtifacts(
		contentquality.TextField{Name: "title", Value: row.Title},
		contentquality.TextField{Name: "content", Value: row.Content},
		contentquality.TextField{Name: "meta_description", Value: row.MetaDescription},
		contentquality.TextField{Name: "keywords", Value: row.Keywords},
	), nil
}
