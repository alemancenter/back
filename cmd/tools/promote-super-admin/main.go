package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/services"
	"gorm.io/gorm"
)

const userModelType = "App\\Models\\User"

func main() {
	userID := flag.Uint("user-id", 0, "exact user ID to promote")
	email := flag.String("email", "", "exact user email to verify")
	flag.Parse()

	if *userID == 0 || strings.TrimSpace(*email) == "" {
		fmt.Fprintln(os.Stderr, "both --user-id and --email are required")
		os.Exit(2)
	}

	db := database.DB()
	var promotedUser models.User
	var superRole models.Role
	var permissionCount int

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Select("id", "name", "email", "status").First(&promotedUser, *userID).Error; err != nil {
			return fmt.Errorf("find user: %w", err)
		}
		if !strings.EqualFold(strings.TrimSpace(promotedUser.Email), strings.TrimSpace(*email)) {
			return fmt.Errorf("safety check failed: user %d email does not match", *userID)
		}

		if err := tx.Where("name = ?", "Super Admin").First(&superRole).Error; err != nil {
			if err != gorm.ErrRecordNotFound {
				return fmt.Errorf("find Super Admin role: %w", err)
			}
			superRole = models.Role{Name: "Super Admin", GuardName: "api"}
			if err := tx.Create(&superRole).Error; err != nil {
				return fmt.Errorf("create Super Admin role: %w", err)
			}
		}

		var permissions []models.Permission
		if err := tx.Order("id ASC").Find(&permissions).Error; err != nil {
			return fmt.Errorf("load permissions: %w", err)
		}
		permissionCount = len(permissions)
		if err := tx.Model(&superRole).Association("Permissions").Replace(permissions); err != nil {
			return fmt.Errorf("assign permissions to Super Admin role: %w", err)
		}
		if err := tx.Exec(
			"INSERT IGNORE INTO model_has_roles (role_id, model_type, model_id) VALUES (?, ?, ?)",
			superRole.ID, userModelType, promotedUser.ID,
		).Error; err != nil {
			return fmt.Errorf("assign Super Admin role: %w", err)
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "promotion failed:", err)
		os.Exit(1)
	}

	services.InvalidateUserCache(promotedUser.ID)
	fmt.Printf("promoted user #%d (%s) to Super Admin role #%d with %d permissions\n", promotedUser.ID, promotedUser.Email, superRole.ID, permissionCount)
}
