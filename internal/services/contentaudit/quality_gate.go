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

// QualityGate evaluates the latest saved audit decision and then checks the
// current source text for deterministic corruption artifacts. A current-source
// corruption finding always wins over an older AI approval: the page becomes
// critical, non-indexable, and ineligible for ads until the source is fixed.
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
	artifacts, err := currentSourceReplacementArtifacts(ctx, normalizeContentType(contentType), cc, numericID)
	if err != nil {
		return ContentQualityGate{}, err
	}
	return contentquality.ApplyReplacementArtifactGuard(gate, artifacts), nil
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
		err = db.Raw(`SELECT title, content, COALESCE(meta_description, '') AS meta_description, '' AS keywords FROM articles WHERE id = ? LIMIT 1`, id).Scan(&row).Error
	case "post":
		err = db.Raw(`SELECT title, content, COALESCE(meta_description, '') AS meta_description, COALESCE(keywords, '') AS keywords FROM posts WHERE id = ? LIMIT 1`, id).Scan(&row).Error
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
