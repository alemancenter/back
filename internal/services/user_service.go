package services

import (
	"errors"

	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
	"gorm.io/gorm"
)

type UserService interface {
	List(search, status, role string, limit, offset int) ([]models.User, int64, error)
	Search(query string) ([]models.User, error)
	GetByID(id uint64) (*models.User, error)
	Create(req *CreateUserRequest, callerID uint) (*models.User, error)
	Update(id uint64, req *UpdateUserRequest, callerID uint) (*models.User, error)
	UpdateRolesPermissions(id uint64, req *RolesPermissionsRequest) error
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

func (s *userService) List(search, status, role string, limit, offset int) ([]models.User, int64, error) {
	return s.repo.List(search, status, role, limit, offset)
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
		if err := user.HashPassword(req.Password); err == nil {
			// Password hashed and set in user model
		}
	}

	if err := s.repo.Update(user); err != nil {
		return nil, errors.New("فشل تحديث المستخدم")
	}

	if callerID > 0 {
		LogActivity("قام بتحديث مستخدم: "+user.Email, "User", user.ID, callerID)
	}

	return user, nil
}

func (s *userService) UpdateRolesPermissions(id uint64, req *RolesPermissionsRequest) error {
	user, err := s.repo.FindByID(id)
	if err != nil {
		return MapError(err)
	}

	db := s.repo.GetDB()

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

	if err := s.repo.BulkDelete(filteredIDs); err != nil {
		return 0, errors.New("فشل حذف المستخدمين المحددين")
	}

	return len(filteredIDs), nil
}

func (s *userService) UpdateStatus(ids []uint, status string, callerID uint) error {
	if err := s.repo.UpdateStatus(ids, status); err != nil {
		return errors.New("فشل تحديث حالة المستخدمين")
	}
	if status == "banned" && s.securitySvc != nil {
		var blockedBy *uint
		if callerID > 0 {
			blockedBy = &callerID
		}
		go s.securitySvc.BlockUserIPs(ids, "user banned", blockedBy)
	}
	return nil
}
