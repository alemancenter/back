package repositories

import (
	"context"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
)

type SettingRepository interface {
	GetAll(ctx context.Context, countryID database.CountryID) ([]models.Setting, error)
	Upsert(ctx context.Context, countryID database.CountryID, key, value string) error
}

type settingRepository struct{}

func NewSettingRepository() SettingRepository {
	return &settingRepository{}
}

func (r *settingRepository) GetAll(ctx context.Context, countryID database.CountryID) ([]models.Setting, error) {
	db := database.DBForCountry(countryID)
	var rows []models.Setting
	err := db.WithContext(ctx).Order("`key`").Find(&rows).Error
	return rows, err
}

func (r *settingRepository) Upsert(ctx context.Context, countryID database.CountryID, key, value string) error {
	db := database.DBForCountry(countryID)
	return db.WithContext(ctx).Exec(
		"INSERT INTO settings (`key`, `value`, created_at, updated_at) VALUES (?, ?, NOW(), NOW()) "+
			"ON DUPLICATE KEY UPDATE `value` = VALUES(`value`), updated_at = NOW()",
		key, value,
	).Error
}
