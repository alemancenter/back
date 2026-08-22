package contentaudit

import (
	"context"
	"errors"

	"github.com/imanjo/fiber-api/internal/contentquality"
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
	content, err := s.loadContentByRef(ctx, normalizeContentType(contentType), cc, numericID)
	if err != nil {
		return ContentQualityGate{}, err
	}
	artifacts := contentquality.DetectReplacementArtifacts(
		contentquality.TextField{Name: "title", Value: content.Title},
		contentquality.TextField{Name: "content", Value: content.Content},
	)
	return contentquality.ApplyReplacementArtifactGuard(gate, artifacts), nil
}
