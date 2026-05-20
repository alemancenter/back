package repositories

import (
	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"gorm.io/gorm"
)

type CategoryRepository interface {
	FindAllActive(countryID database.CountryID) ([]models.Category, error)
	FindByID(countryID database.CountryID, id uint64) (*models.Category, error)
	ListPaginated(countryID database.CountryID, search string, isActive *bool, limit, offset int) ([]models.Category, int64, error)
	Create(countryID database.CountryID, category *models.Category) error
	Update(countryID database.CountryID, category *models.Category) error
	Delete(countryID database.CountryID, id uint64) error
	BulkDelete(countryID database.CountryID, ids []uint) error
	UpdateStatus(countryID database.CountryID, ids []uint, isActive bool) error
}

type categoryRepository struct{}

func NewCategoryRepository() CategoryRepository {
	return &categoryRepository{}
}

func (r *categoryRepository) getDB(countryID database.CountryID) *gorm.DB {
	return database.DBForCountry(countryID)
}

func (r *categoryRepository) FindAllActive(countryID database.CountryID) ([]models.Category, error) {
	db := r.getDB(countryID)
	var list []models.Category
	err := db.Where("is_active = ?", true).Order("name ASC").Find(&list).Error
	return list, err
}

func (r *categoryRepository) FindByID(countryID database.CountryID, id uint64) (*models.Category, error) {
	db := r.getDB(countryID)
	var category models.Category
	err := db.First(&category, id).Error
	if err != nil {
		return nil, err
	}
	return &category, nil
}

func (r *categoryRepository) ListPaginated(countryID database.CountryID, search string, isActive *bool, limit, offset int) ([]models.Category, int64, error) {
	db := r.getDB(countryID)
	var list []models.Category
	var total int64

	query := db.Model(&models.Category{})
	if search != "" {
		query = query.Where("name LIKE ?", "%"+search+"%")
	}
	if isActive != nil {
		query = query.Where("is_active = ?", *isActive)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.Order("name ASC").Limit(limit).Offset(offset).Find(&list).Error
	return list, total, err
}

func (r *categoryRepository) Create(countryID database.CountryID, category *models.Category) error {
	db := r.getDB(countryID)
	return db.Create(category).Error
}

func (r *categoryRepository) Update(countryID database.CountryID, category *models.Category) error {
	db := r.getDB(countryID)
	return db.Save(category).Error
}

func (r *categoryRepository) Delete(countryID database.CountryID, id uint64) error {
	db := r.getDB(countryID)
	return db.Delete(&models.Category{}, id).Error
}

func (r *categoryRepository) BulkDelete(countryID database.CountryID, ids []uint) error {
	db := r.getDB(countryID)
	return db.Where("id IN ?", ids).Delete(&models.Category{}).Error
}

func (r *categoryRepository) UpdateStatus(countryID database.CountryID, ids []uint, isActive bool) error {
	db := r.getDB(countryID)
	return db.Model(&models.Category{}).Where("id IN ?", ids).Update("is_active", isActive).Error
}
