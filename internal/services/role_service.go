package services

import (
	"errors"

	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/models"
	"github.com/imanjo/fiber-api/internal/repositories"
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

func roleMemberIDs(roleID uint) ([]uint, error) {
	var userIDs []uint

	err := database.DB().
		Table("model_has_roles").
		Where("role_id = ? AND model_type = ?", roleID, modelTypeUser).
		Pluck("model_id", &userIDs).Error
	if err != nil {
		return nil, MapError(err)
	}

	return userIDs, nil
}

func permissionMemberIDs(permissionID uint) ([]uint, error) {
	var userIDs []uint

	err := database.DB().Raw(`
		SELECT DISTINCT model_id
		FROM (
			SELECT mhp.model_id
			FROM model_has_permissions mhp
			WHERE mhp.permission_id = ?
			  AND mhp.model_type = ?

			UNION

			SELECT mhr.model_id
			FROM role_has_permissions rhp
			JOIN model_has_roles mhr
			  ON mhr.role_id = rhp.role_id
			WHERE rhp.permission_id = ?
			  AND mhr.model_type = ?
		) affected_users
	`, permissionID, modelTypeUser, permissionID, modelTypeUser).
		Scan(&userIDs).Error
	if err != nil {
		return nil, MapError(err)
	}

	return userIDs, nil
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

	affectedUserIDs, err := roleMemberIDs(uint(id))
	if err != nil {
		return nil, err
	}

	role, err := s.repo.GetRole(id)
	if err != nil {
		return nil, MapError(err)
	}
	if (role.Name == "Super Admin" || role.Name == "Admin") && name != "" && name != role.Name {
		return nil, ErrProtectedRole
	}

	if name != "" {
		role.Name = name
	}

	err = s.repo.UpdateRole(role, permissions)
	if err != nil {
		return nil, MapError(err)
	}

	InvalidateUserCaches(affectedUserIDs)

	// Return role with preloaded permissions
	return s.repo.GetRole(id)
}

func (s *roleService) DeleteRole(callerID uint, id uint64) error {
	if err := s.requireSuperAdmin(callerID); err != nil {
		return err
	}
	role, err := s.repo.GetRole(id)
	if err != nil {
		return MapError(err)
	}
	if role.Name == "Super Admin" || role.Name == "Admin" {
		return ErrProtectedRole
	}

	affectedUserIDs, err := roleMemberIDs(role.ID)
	if err != nil {
		return err
	}

	if err := s.repo.DeleteRole(id); err != nil {
		return MapError(err)
	}

	InvalidateUserCaches(affectedUserIDs)
	return nil
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

	affectedUserIDs, err := permissionMemberIDs(uint(id))
	if err != nil {
		return err
	}

	if err := s.repo.UpdatePermission(id, name); err != nil {
		return MapError(err)
	}

	InvalidateUserCaches(affectedUserIDs)
	return nil
}

func (s *roleService) DeletePermission(callerID uint, id uint64) error {
	if err := s.requireSuperAdmin(callerID); err != nil {
		return err
	}

	affectedUserIDs, err := permissionMemberIDs(uint(id))
	if err != nil {
		return err
	}

	if err := s.repo.DeletePermission(id); err != nil {
		return MapError(err)
	}

	InvalidateUserCaches(affectedUserIDs)
	return nil
}
