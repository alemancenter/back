package contentaudit

import (
	"context"
	"errors"
	"strings"

	"github.com/imanjo/fiber-api/internal/models"
	"gorm.io/gorm"
)

var ErrInvalidEditorialDecision = errors.New("invalid editorial decision")

func NormalizeEditorialDecision(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case models.EditorialDecisionUnclassified,
		models.EditorialDecisionKeep,
		models.EditorialDecisionImprove,
		models.EditorialDecisionNoindex,
		models.EditorialDecisionMerge301:
		return value
	default:
		return ""
	}
}

func (s *Service) SaveEditorialDecision(ctx context.Context, decision *models.ContentEditorialDecision) error {
	if decision == nil || decision.ContentID == 0 {
		return ErrInvalidEditorialDecision
	}
	decision.CountryCode = strings.ToLower(strings.TrimSpace(decision.CountryCode))
	decision.ContentType = normalizeContentType(decision.ContentType)
	decision.Decision = NormalizeEditorialDecision(decision.Decision)
	if decision.CountryCode == "" || decision.ContentType == "" || decision.Decision == "" {
		return ErrInvalidEditorialDecision
	}
	return s.repo.SaveEditorialDecision(ctx, decision)
}

func (s *Service) LatestEditorialDecision(ctx context.Context, contentType string, contentID uint, countryCode string) (*models.ContentEditorialDecision, error) {
	contentType = normalizeContentType(contentType)
	countryCode = strings.ToLower(strings.TrimSpace(countryCode))
	if contentType == "" || contentID == 0 || countryCode == "" {
		return nil, ErrInvalidEditorialDecision
	}
	return s.repo.LatestEditorialDecision(ctx, contentType, contentID, countryCode)
}

func (s *Service) EditorialHistory(ctx context.Context, contentType string, contentID uint, countryCode string, limit int) ([]models.ContentEditorialDecision, error) {
	contentType = normalizeContentType(contentType)
	countryCode = strings.ToLower(strings.TrimSpace(countryCode))
	if contentType == "" || contentID == 0 || countryCode == "" {
		return nil, ErrInvalidEditorialDecision
	}
	return s.repo.ListEditorialHistory(ctx, contentType, contentID, countryCode, limit)
}

func editorialDecisionOrEmpty(decision *models.ContentEditorialDecision, err error) (string, error) {
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	if decision == nil {
		return "", nil
	}
	return decision.Decision, nil
}
