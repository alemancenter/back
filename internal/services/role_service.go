package services

import (
	"errors"

	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
)

type RoleService interface {
	ListRoles() ([]models.Role, error)
	GetRole(id uint64) (*models.Role, error)
	CreateRole(callerID uint, name string, permissions []uint) (*models.Role, error)
	UpdateRole(callerID uint, id uint64, name string, permissions []uint) (*models.Role, error)
	DeleteRole(callerID uint, id uint64) error
	ListPermissions() ([]models.Permission, error)
	CreatePermission(callerID uint, name string) (*models.Permission, error)
	UpdatePermission(callerID uint, id uint64, name string) error
	DeletePermission(callerID uint, id uint64) error
}

type roleService struct {
	repo     repositories.RoleRepository
	userRepo repositories.UserRepository
}

func NewRoleService(repo repositories.RoleRepository, userRepo repositories.UserRepository) RoleService {
	return &roleService{repo: repo, userRepo: userRepo}
}

// requireSuperAdmin re-verifies (server-side, independent of route middleware
// or any client-supplied claim) that callerID belongs to an active Super Admin.
// Role/permission *definitions* are the root of the whole authorization system —
// unlike assigning an existing role to a user (gated by "manage roles"),
// creating/editing/deleting a role's permission set must never be reachable by
// a non-Super-Admin, since a "manage roles" holder could otherwise add
// "manage roles" (or any other permission) directly to their own current role
// and self-escalate without ever touching the user-role assignment endpoint.
func (s *roleService) requireSuperAdmin(callerID uint) error {
	if callerID == 0 {
		return ErrForbidden
	}
	caller, err := s.userRepo.FindByID(uint64(callerID))
	if err != nil {
		return ErrForbidden
	}
	if !caller.HasRole("Super Admin") {
		return ErrForbidden
	}
	return nil
}

func (s *roleService) ListRoles() ([]models.Role, error) {
	return s.repo.ListRoles()
}

func (s *roleService) GetRole(id uint64) (*models.Role, error) {
	return s.repo.GetRole(id)
}

func (s *roleService) CreateRole(callerID uint, name string, permissions []uint) (*models.Role, error) {
	if err := s.requireSuperAdmin(callerID); err != nil {
		return nil, err
	}

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

func (s *roleService) UpdateRole(callerID uint, id uint64, name string, permissions []uint) (*models.Role, error) {
	if err := s.requireSuperAdmin(callerID); err != nil {
		return nil, err
	}

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

func (s *roleService) DeleteRole(callerID uint, id uint64) error {
	if err := s.requireSuperAdmin(callerID); err != nil {
		return err
	}
	return s.repo.DeleteRole(id)
}

func (s *roleService) ListPermissions() ([]models.Permission, error) {
	return s.repo.ListPermissions()
}

func (s *roleService) CreatePermission(callerID uint, name string) (*models.Permission, error) {
	if err := s.requireSuperAdmin(callerID); err != nil {
		return nil, err
	}
	permission := &models.Permission{Name: name, GuardName: "api"}
	err := s.repo.CreatePermission(permission)
	return permission, MapError(err)
}

func (s *roleService) UpdatePermission(callerID uint, id uint64, name string) error {
	if err := s.requireSuperAdmin(callerID); err != nil {
		return err
	}
	return s.repo.UpdatePermission(id, name)
}

func (s *roleService) DeletePermission(callerID uint, id uint64) error {
	if err := s.requireSuperAdmin(callerID); err != nil {
		return err
	}
	return s.repo.DeletePermission(id)
}
