package repositories

import (
	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
)

type RoleRepository interface {
	ListRoles() ([]models.Role, error)
	GetRole(id uint64) (*models.Role, error)
	CreateRole(role *models.Role, permissions []uint) error
	UpdateRole(role *models.Role, permissions []uint) error
	DeleteRole(id uint64) error
	GetRoleByName(name string) (*models.Role, error)
	ListPermissions() ([]models.Permission, error)
	CreatePermission(permission *models.Permission) error
	UpdatePermission(id uint64, name string) error
	DeletePermission(id uint64) error
}

type roleRepository struct{}

func NewRoleRepository() RoleRepository {
	return &roleRepository{}
}

func (r *roleRepository) ListRoles() ([]models.Role, error) {
	var roles []models.Role
	err := database.DB().Preload("Permissions").Find(&roles).Error
	return roles, err
}

func (r *roleRepository) GetRole(id uint64) (*models.Role, error) {
	var role models.Role
	err := database.DB().Preload("Permissions").First(&role, id).Error
	return &role, err
}

func (r *roleRepository) GetRoleByName(name string) (*models.Role, error) {
	var role models.Role
	err := database.DB().Where("name = ?", name).First(&role).Error
	return &role, err
}

func (r *roleRepository) CreateRole(role *models.Role, permissionIDs []uint) error {
	db := database.DB()
	tx := db.Begin()
	if err := tx.Error; err != nil {
		return err
	}

	if err := tx.Create(role).Error; err != nil {
		tx.Rollback()
		return err
	}

	if len(permissionIDs) > 0 {
		var permissions []models.Permission
		if err := tx.Where("id IN ?", permissionIDs).Find(&permissions).Error; err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Model(role).Association("Permissions").Replace(permissions); err != nil {
			tx.Rollback()
			return err
		}
	}

	return tx.Commit().Error
}

func (r *roleRepository) UpdateRole(role *models.Role, permissionIDs []uint) error {
	db := database.DB()
	tx := db.Begin()
	if err := tx.Error; err != nil {
		return err
	}

	if err := tx.Save(role).Error; err != nil {
		tx.Rollback()
		return err
	}

	if permissionIDs != nil {
		var permissions []models.Permission
		if err := tx.Where("id IN ?", permissionIDs).Find(&permissions).Error; err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Model(role).Association("Permissions").Replace(permissions); err != nil {
			tx.Rollback()
			return err
		}
	}

	return tx.Commit().Error
}

func (r *roleRepository) DeleteRole(id uint64) error {
	return database.DB().Delete(&models.Role{}, id).Error
}

func (r *roleRepository) ListPermissions() ([]models.Permission, error) {
	var permissions []models.Permission
	err := database.DB().Order("name ASC").Find(&permissions).Error
	return permissions, err
}

func (r *roleRepository) CreatePermission(permission *models.Permission) error {
	return database.DB().Create(permission).Error
}

func (r *roleRepository) UpdatePermission(id uint64, name string) error {
	return database.DB().Model(&models.Permission{}).Where("id = ?", id).Update("name", name).Error
}

func (r *roleRepository) DeletePermission(id uint64) error {
	return database.DB().Delete(&models.Permission{}, id).Error
}
