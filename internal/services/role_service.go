package services

import (
	"errors"

	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
)

type RoleService interface {
	ListRoles() ([]models.Role, error)
	GetRole(id uint64) (*models.Role, error)
	CreateRole(name string, permissions []uint) (*models.Role, error)
	UpdateRole(id uint64, name string, permissions []uint) (*models.Role, error)
	DeleteRole(id uint64) error
	ListPermissions() ([]models.Permission, error)
	CreatePermission(name string) (*models.Permission, error)
	UpdatePermission(id uint64, name string) error
	DeletePermission(id uint64) error
}

type roleService struct {
	repo repositories.RoleRepository
}

func NewRoleService(repo repositories.RoleRepository) RoleService {
	return &roleService{repo: repo}
}

func (s *roleService) ListRoles() ([]models.Role, error) {
	return s.repo.ListRoles()
}

func (s *roleService) GetRole(id uint64) (*models.Role, error) {
	return s.repo.GetRole(id)
}

func (s *roleService) CreateRole(name string, permissions []uint) (*models.Role, error) {
	_, err := s.repo.GetRoleByName(name)
	if err == nil {
		return nil, errors.New("اسم الدور مستخدم بالفعل")
	}

	role := &models.Role{Name: name, GuardName: "api"}
	err = s.repo.CreateRole(role, permissions)
	if err != nil {
		return nil, MapError(err)
	}

	// Return role with preloaded permissions
	return s.repo.GetRole(uint64(role.ID))
}

func (s *roleService) UpdateRole(id uint64, name string, permissions []uint) (*models.Role, error) {
	role, err := s.repo.GetRole(id)
	if err != nil {
		return nil, MapError(err)
	}

	if name != "" {
		role.Name = name
	}

	err = s.repo.UpdateRole(role, permissions)
	if err != nil {
		return nil, MapError(err)
	}

	// Return role with preloaded permissions
	return s.repo.GetRole(id)
}

func (s *roleService) DeleteRole(id uint64) error {
	return s.repo.DeleteRole(id)
}

func (s *roleService) ListPermissions() ([]models.Permission, error) {
	return s.repo.ListPermissions()
}

func (s *roleService) CreatePermission(name string) (*models.Permission, error) {
	permission := &models.Permission{Name: name, GuardName: "api"}
	err := s.repo.CreatePermission(permission)
	return permission, MapError(err)
}

func (s *roleService) UpdatePermission(id uint64, name string) error {
	return s.repo.UpdatePermission(id, name)
}

func (s *roleService) DeletePermission(id uint64) error {
	return s.repo.DeletePermission(id)
}
