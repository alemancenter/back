package contentaudit

import (
	"context"

	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/models"
)

func latestReadinessEditorialDecisions(ctx context.Context, contentType, countryCode string) (map[uint]*models.ContentEditorialDecision, error) {
	var rows []models.ContentEditorialDecision
	if err := database.DB().WithContext(ctx).
		Where("content_type = ? AND country_code = ?", contentType, countryCode).
		Order("created_at DESC, id DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	latest := make(map[uint]*models.ContentEditorialDecision, len(rows))
	for i := range rows {
		row := &rows[i]
		if row.ContentID == 0 {
			continue
		}
		if _, exists := latest[row.ContentID]; exists {
			continue
		}
		latest[row.ContentID] = row
	}
	return latest, nil
}
