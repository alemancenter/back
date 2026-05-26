package repositories

import (
	"strings"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"gorm.io/gorm"
)

// UserRepository handles database operations for users
type UserRepository interface {
	GetDB() *gorm.DB
	CountByEmail(email string) (int64, error)
	List(search, status, role string, limit, offset int) ([]models.User, int64, error)
	Search(query string, limit int) ([]models.User, error)
	Create(user *models.User) error
	FindByEmail(email string) (*models.User, error)
	FindByID(id uint64) (*models.User, error)
	FindByGoogleID(googleID string) (*models.User, error)
	FindByEmailOrGoogleID(email string, googleID string) (*models.User, error)
	FindByEmailOrFacebookID(email string, facebookID string) (*models.User, error)
	Update(user *models.User) error
	UpdateFields(id uint, fields map[string]interface{}) error
	Delete(user *models.User) error
	BulkDelete(ids []uint) error
	UpdateStatus(ids []uint, status string) error
	UpsertPushToken(pushToken *models.PushToken) error
	DeletePushToken(userID uint, token string) error
	HasAnyRole(userID uint) bool
	GetAllUserIDs(role string) ([]uint, error)
	GetUserIDsByPermissions(permissions []string) ([]uint, error)
	GetPushTokensByUserIDs(userIDs []uint) ([]models.PushToken, error)
}

type userRepository struct{}

// NewUserRepository creates a new UserRepository
func NewUserRepository() UserRepository {
	return &userRepository{}
}

func (r *userRepository) GetDB() *gorm.DB {
	return database.DB()
}

func (r *userRepository) CountByEmail(email string) (int64, error) {
	db := r.GetDB()
	var count int64
	err := db.Model(&models.User{}).Where("email = ?", email).Count(&count).Error
	return count, err
}

func (r *userRepository) List(search, status, role string, limit, offset int) ([]models.User, int64, error) {
	db := r.GetDB()
	var users []models.User
	var total int64

	query := db.Model(&models.User{}).Preload("Roles")

	if search != "" {
		query = query.Where("name LIKE ? OR email LIKE ?", "%"+search+"%", "%"+search+"%")
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if role != "" {
		query = query.Joins("JOIN model_has_roles ON model_has_roles.model_id = users.id").
			Joins("JOIN roles ON roles.id = model_has_roles.role_id").
			Where("roles.name = ?", role)
	}

	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	err = query.Order("created_at DESC").Limit(limit).Offset(offset).Find(&users).Error
	return users, total, err
}

func (r *userRepository) Search(query string, limit int) ([]models.User, error) {
	db := r.GetDB()
	var users []models.User
	err := db.Select("id, name, email, profile_photo_path").
		Where("name LIKE ? OR email LIKE ?", "%"+query+"%", "%"+query+"%").
		Limit(limit).Find(&users).Error
	return users, err
}

func (r *userRepository) Create(user *models.User) error {
	db := r.GetDB()
	return db.Create(user).Error
}

func (r *userRepository) FindByEmail(email string) (*models.User, error) {
	db := r.GetDB()
	var user models.User
	err := db.Preload("Roles.Permissions").Preload("Permissions").
		Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) FindByID(id uint64) (*models.User, error) {
	db := r.GetDB()
	var user models.User
	err := db.Preload("Roles.Permissions").Preload("Permissions").
		Where("id = ?", id).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) FindByGoogleID(googleID string) (*models.User, error) {
	db := r.GetDB()
	var user models.User
	err := db.Preload("Roles.Permissions").Preload("Permissions").
		Where("google_id = ?", googleID).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) FindByEmailOrGoogleID(email string, googleID string) (*models.User, error) {
	db := r.GetDB()
	var user models.User
	err := db.Preload("Roles.Permissions").Preload("Permissions").
		Where("email = ? OR google_id = ?", email, googleID).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) FindByEmailOrFacebookID(email string, facebookID string) (*models.User, error) {
	db := r.GetDB()
	var user models.User
	err := db.Preload("Roles.Permissions").Preload("Permissions").
		Where("email = ? OR facebook_id = ?", email, facebookID).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) Update(user *models.User) error {
	db := r.GetDB()
	return db.Save(user).Error
}

func (r *userRepository) UpdateFields(id uint, fields map[string]interface{}) error {
	if len(fields) == 0 {
		return nil
	}

	db := r.GetDB()
	return db.Model(&models.User{}).Where("id = ?", id).Updates(fields).Error
}

func (r *userRepository) Delete(user *models.User) error {
	db := r.GetDB()
	return db.Delete(user).Error
}

func (r *userRepository) BulkDelete(ids []uint) error {
	db := r.GetDB()
	return db.Where("id IN ?", ids).Delete(&models.User{}).Error
}

func (r *userRepository) UpdateStatus(ids []uint, status string) error {
	db := r.GetDB()
	return db.Model(&models.User{}).Where("id IN ?", ids).Update("status", status).Error
}

func (r *userRepository) UpsertPushToken(pushToken *models.PushToken) error {
	db := r.GetDB()
	return db.Where(models.PushToken{Token: pushToken.Token}).Assign(pushToken).FirstOrCreate(pushToken).Error
}

func (r *userRepository) DeletePushToken(userID uint, token string) error {
	db := r.GetDB()
	return db.Where("user_id = ? AND token = ?", userID, token).Delete(&models.PushToken{}).Error
}

func (r *userRepository) HasAnyRole(userID uint) bool {
	var count int64
	database.DB().Raw(
		"SELECT COUNT(*) FROM model_has_roles WHERE model_id = ? AND model_type = ?",
		userID, "App\\Models\\User",
	).Scan(&count)
	return count > 0
}

func (r *userRepository) GetAllUserIDs(role string) ([]uint, error) {
	db := r.GetDB()
	var ids []uint
	query := db.Model(&models.User{}).Where("status NOT IN ?", []string{"inactive", "banned"})
	if role != "" {
		query = query.Joins("JOIN model_has_roles ON model_has_roles.model_id = users.id").
			Joins("JOIN roles ON roles.id = model_has_roles.role_id").
			Where("roles.name = ?", role)
	}
	err := query.Pluck("users.id", &ids).Error
	return ids, err
}

func (r *userRepository) GetUserIDsByPermissions(permissions []string) ([]uint, error) {
	db := r.GetDB()
	ids := make([]uint, 0)

	cleaned := make([]string, 0, len(permissions))
	seen := make(map[string]bool, len(permissions))
	for _, permission := range permissions {
		permission = strings.TrimSpace(permission)
		if permission == "" || seen[permission] {
			continue
		}
		seen[permission] = true
		cleaned = append(cleaned, permission)
	}
	if len(cleaned) == 0 {
		return ids, nil
	}

	err := db.Table("users").
		Select("DISTINCT users.id").
		Joins("LEFT JOIN model_has_permissions mhp ON mhp.model_id = users.id AND mhp.model_type = ?", "App\\Models\\User").
		Joins("LEFT JOIN permissions direct_permissions ON direct_permissions.id = mhp.permission_id").
		Joins("LEFT JOIN model_has_roles mhr ON mhr.model_id = users.id AND mhr.model_type = ?", "App\\Models\\User").
		Joins("LEFT JOIN role_has_permissions rhp ON rhp.role_id = mhr.role_id").
		Joins("LEFT JOIN permissions role_permissions ON role_permissions.id = rhp.permission_id").
		Where("users.status NOT IN ?", []string{"inactive", "banned"}).
		Where("direct_permissions.name IN ? OR role_permissions.name IN ?", cleaned, cleaned).
		Pluck("users.id", &ids).Error

	return ids, err
}

func (r *userRepository) GetPushTokensByUserIDs(userIDs []uint) ([]models.PushToken, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	var tokens []models.PushToken
	err := r.GetDB().Where("user_id IN ?", userIDs).Find(&tokens).Error
	return tokens, err
}
