package database

import (
	"fmt"
	"gorm.io/gorm"
)

// RequireTables performs validation only. Schema changes belong to --migrate-only.
func RequireTables(db *gorm.DB, targets ...interface{}) error {
	if db == nil {
		return fmt.Errorf("database unavailable")
	}
	if db.Error != nil {
		return db.Error
	}
	for _, model := range targets {
		if !db.Migrator().HasTable(model) {
			return fmt.Errorf("schema missing for %T; run --migrate-only", model)
		}
	}
	return nil
}
