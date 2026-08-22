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

// QualityGate loads the latest saved audit decision and evaluates it. A missing
// decision is a normal unaudited state; infrastructure/database errors remain
// errors so callers can fail closed rather than silently permitting ads.
func (s *Service) QualityGate(ctx context.Context, contentType, contentID, countryCode string) (ContentQualityGate, error) {
	decision, err := s.LatestAIDecision(ctx, contentType, contentID, countryCode)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return UnauditedQualityGate(), nil
		}
		return ContentQualityGate{}, err
	}
	return EvaluateQualityGate(decision), nil
}
