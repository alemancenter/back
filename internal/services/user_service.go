package services

import (
	"errors"

	"github.com/imanjo/fiber-api/internal/models"
	"github.com/imanjo/fiber-api/internal/repositories"
	"gorm.io/gorm"
)

type UserService interface {
	List(search, status, role, emailVerified string, limit, offset int) ([]models.User, int64, error)
	GetUserActivity(id uint64, limit int) (*repositories.UserActivityData, error)
	Search(query string) ([]models.User, error)
	GetByID(id uint64) (*models.User, error)
	Create(req *CreateUserRequest, callerID uint) (*models.User, error)
	Update(id uint64, req *UpdateUserRequest, callerID uint) (*models.User, error)
	UpdateRolesPermissions(id uint64, req *RolesPermissionsRequest, callerID uint) error
	Delete(id uint64, callerID uint) error
	BulkDelete(ids []uint, callerID uint) (int, error)
	UpdateStatus(ids []uint, status string, callerID uint) error
}

type BulkDeleteUsersResponse struct {
	Deleted int `json:"deleted"`
}

type CreateUserRequest struct {
	Name     string `json:"name" validate:"required,min=2,max=255"`
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
	Roles    []uint `json:"roles"`
}

type UpdateUserRequest struct {
	Name     string `json:"name" validate:"omitempty,min=2,max=255"`
	Phone    string `json:"phone"`
	JobTitle string `json:"job_title"`
	Gender   string `json:"gender" validate:"omitempty,oneof=male female other"`
	Country  string `json:"country"`
	Status   string `json:"status" validate:"omitempty,oneof=active inactive banned"`
	Password string `json:"password" validate:"omitempty,min=8"`
}

type RolesPermissionsRequest struct {
	Roles       []uint `json:"roles"`
	Permissions []uint `json:"permissions"`
}

type userService struct {
	repo        repositories.UserRepository
	securitySvc SecurityService
}

func NewUserService(repo repositories.UserRepository, securitySvc SecurityService) UserService {
	return &userService{repo: repo, securitySvc: securitySvc}
}

// Test seam only. Production behavior remains InvalidateUserCache.
var invalidateUserServiceCache = InvalidateUserCache

// requireSuperAdminForProtectedUser prevents ordinary administrators from
// mutating a Super Admin account. Ordinary users remain manageable by callers
// that have already passed the route-level "manage users" permission check.
func (s *userService) requireSuperAdminForProtectedUser(user *models.User, callerID uint) error {
	if user == nil || !user.HasRole("Super Admin") {
		return nil
	}

	if callerID == 0 {
		return errors.New("غير مصرح بتنفيذ العملية")
	}

	caller, err := s.repo.FindByID(uint64(callerID))
	if err != nil {
		return MapError(err)
	}

	if !caller.HasRole("Super Admin") {
		return errors.New("لا يحق لك تعديل حساب Super Admin")
	}

	return nil
}

// requireSuperAdminForProtectedIDs applies the same protection to bulk
// mutations. Missing/stale IDs retain the previous bulk semantics and are
// ignored here; the repository remains authoritative for the actual mutation.
func (s *userService) requireSuperAdminForProtectedIDs(ids []uint, callerID uint) error {
	for _, id := range ids {
		user, err := s.repo.FindByID(uint64(id))
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return MapError(err)
		}

		if err := s.requireSuperAdminForProtectedUser(user, callerID); err != nil {
			return err
		}
	}

	return nil
}

func (s *userService) List(search, status, role, emailVerified string, limit, offset int) ([]models.User, int64, error) {
	return s.repo.List(search, status, role, emailVerified, limit, offset)
}

func (s *userService) GetUserActivity(id uint64, limit int) (*repositories.UserActivityData, error) {
	if _, err := s.repo.FindByID(id); err != nil {
		return nil, MapError(err)
	}
	return s.repo.GetUserActivity(id, limit)
}

func (s *userService) Search(query string) ([]models.User, error) {
	if len(query) < 2 {
		return []models.User{}, nil
	}
	return s.repo.Search(query, 10)
}

func (s *userService) GetByID(id uint64) (*models.User, error) {
	user, err := s.repo.FindByID(id)
	return user, MapError(err)
}

func (s *userService) Create(req *CreateUserRequest, callerID uint) (*models.User, error) {
	count, err := s.repo.CountByEmail(req.Email)
	if err != nil {
		return nil, MapError(err)
	}
	if count > 0 {
		return nil, errors.New("البريد الإلكتروني مستخدم بالفعل")
	}

	user := &models.User{
		Name:   req.Name,
		Email:  req.Email,
		Status: "active",
	}
	if err := user.HashPassword(req.Password); err != nil {
		return nil, MapError(err)
	}

	db := s.repo.GetDB()

	if len(req.Roles) > 0 {
		var requestedRoles []models.Role
		if err := db.Where("id IN ?", req.Roles).Find(&requestedRoles).Error; err != nil {
			return nil, MapError(err)
		}
		if len(requestedRoles) != len(req.Roles) {
			return nil, errors.New("تم إرسال دور غير موجود")
		}

		for _, role := range requestedRoles {
			if role.Name == "Super Admin" {
				caller, err := s.repo.FindByID(uint64(callerID))
				if err != nil || !caller.HasRole("Super Admin") {
					return nil, errors.New("لا يحق لك منح دور Super Admin")
				}
				break
			}
		}
	}

	txErr := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(user).Error; err != nil {
			return MapError(err)
		}
		if len(req.Roles) > 0 {
			return AssignRoles(tx, user.ID, req.Roles)
		}
		return nil
	})
	if txErr != nil {
		return nil, errors.New("فشل إنشاء المستخدم")
	}

	if callerID > 0 {
		LogActivity("أنشأ مستخدم: "+user.Email, "User", user.ID, callerID)
	}

	return user, nil
}

func (s *userService) Update(id uint64, req *UpdateUserRequest, callerID uint) (*models.User, error) {
	user, err := s.repo.FindByID(id)
	if err != nil {
		return nil, MapError(err)
	}

	if err := s.requireSuperAdminForProtectedUser(user, callerID); err != nil {
		return nil, err
	}

	if req.Name != "" {
		user.Name = req.Name
	}
	if req.Phone != "" {
		user.Phone = &req.Phone
	}
	if req.JobTitle != "" {
		user.JobTitle = &req.JobTitle
	}
	if req.Gender != "" {
		user.Gender = &req.Gender
	}
	if req.Country != "" {
		user.Country = &req.Country
	}
	if req.Status != "" {
		user.Status = req.Status
	}
	if req.Password != "" {
		if err := user.HashPassword(req.Password); err != nil {
			return nil, MapError(err)
		}

		// Password changes revoke all previously issued access and refresh tokens.
		user.AuthVersion++
	}

	if err := s.repo.Update(user); err != nil {
		return nil, errors.New("فشل تحديث المستخدم")
	}

	// Auth() caches the complete User object, including status and RBAC state.
	invalidateUserServiceCache(user.ID)

	if callerID > 0 {
		LogActivity("قام بتحديث مستخدم: "+user.Email, "User", user.ID, callerID)
	}

	return user, nil
}

func (s *userService) UpdateRolesPermissions(id uint64, req *RolesPermissionsRequest, callerID uint) error {
	if callerID == 0 {
		return errors.New("غير مصرح بتنفيذ العملية")
	}

	if uint64(callerID) == id {
		return errors.New("لا يمكنك تعديل أدوارك أو صلاحياتك بنفسك")
	}

	caller, err := s.repo.FindByID(uint64(callerID))
	if err != nil {
		return MapError(err)
	}

	if !caller.HasPermission("manage roles") && !caller.HasRole("Super Admin") {
		return errors.New("لا تملك صلاحية إدارة الأدوار")
	}

	user, err := s.repo.FindByID(id)
	if err != nil {
		return MapError(err)
	}

	if user.HasRole("Super Admin") && !caller.HasRole("Super Admin") {
		return errors.New("لا يحق لك تعديل حساب Super Admin")
	}

	db := s.repo.GetDB()

	var requestedRoles []models.Role
	if len(req.Roles) > 0 {
		if err := db.Where("id IN ?", req.Roles).Find(&requestedRoles).Error; err != nil {
			return MapError(err)
		}

		if len(requestedRoles) != len(req.Roles) {
			return errors.New("تم إرسال دور غير موجود")
		}
	}

	for _, role := range requestedRoles {
		if role.Name == "Super Admin" && !caller.HasRole("Super Admin") {
			return errors.New("لا يحق لك منح دور Super Admin")
		}
	}

	if err := AssignRoles(db, user.ID, req.Roles); err != nil {
		return errors.New("فشل تحديث الأدوار")
	}
	if err := AssignPermissions(db, user.ID, req.Permissions); err != nil {
		return errors.New("فشل تحديث الصلاحيات")
	}

	InvalidateUserCache(user.ID)

	return nil
}

func (s *userService) Delete(id uint64, callerID uint) error {
	if callerID > 0 && callerID == uint(id) {
		return errors.New("لا يمكنك حذف حسابك الخاص")
	}

	user, err := s.repo.FindByID(id)
	if err != nil {
		return MapError(err)
	}

	if err := s.requireSuperAdminForProtectedUser(user, callerID); err != nil {
		return err
	}

	if err := s.repo.Delete(user); err != nil {
		return errors.New("فشل حذف المستخدم")
	}

	if callerID > 0 {
		LogActivity("حذف مستخدم: "+user.Email, "User", user.ID, callerID)
	}

	return nil
}

func (s *userService) BulkDelete(ids []uint, callerID uint) (int, error) {
	filteredIDs := make([]uint, 0)
	for _, id := range ids {
		if callerID == 0 || id != callerID {
			filteredIDs = append(filteredIDs, id)
		}
	}

	if len(filteredIDs) == 0 {
		return 0, nil
	}

	if err := s.requireSuperAdminForProtectedIDs(filteredIDs, callerID); err != nil {
		return 0, err
	}

	if err := s.repo.BulkDelete(filteredIDs); err != nil {
		return 0, errors.New("فشل حذف المستخدمين المحددين")
	}

	return len(filteredIDs), nil
}

func (s *userService) UpdateStatus(ids []uint, status string, callerID uint) error {
	if err := s.requireSuperAdminForProtectedIDs(ids, callerID); err != nil {
		return err
	}

	if err := s.repo.UpdateStatus(ids, status); err != nil {
		return errors.New("فشل تحديث حالة المستخدمين")
	}

	// Auth() checks status from the cached User object.
	InvalidateUserCaches(ids)

	if status == "banned" && s.securitySvc != nil {
		var blockedBy *uint
		if callerID > 0 {
			blockedBy = &callerID
		}
		go s.securitySvc.BlockUserIPs(ids, "user banned", blockedBy)
	}
	return nil
}
