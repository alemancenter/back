package repositories

import (
	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"gorm.io/gorm"
)

type FileRepository interface {
	ListPaginated(countryID database.CountryID, fileType string, articleID string, limit, offset int) ([]models.File, int64, error)
	FindByID(countryID database.CountryID, id uint64) (*models.File, error)
	GetFileWithParent(countryID database.CountryID, id uint64) (*models.File, interface{}, string, error)
	IncrementView(countryID database.CountryID, id uint64) error
	Create(countryID database.CountryID, file *models.File) error
	Update(countryID database.CountryID, file *models.File) error
	Delete(countryID database.CountryID, file *models.File) error
}

type fileRepository struct{}

func NewFileRepository() FileRepository {
	return &fileRepository{}
}

func (r *fileRepository) GetDB(countryID database.CountryID) *gorm.DB {
	return database.DBForCountry(countryID)
}

func (r *fileRepository) ListPaginated(countryID database.CountryID, fileType string, articleID string, limit, offset int) ([]models.File, int64, error) {
	db := r.GetDB(countryID)
	var fileList []models.File
	var total int64

	query := db.Model(&models.File{})
	if fileType != "" {
		query = query.Where("file_type = ?", fileType)
	}
	if articleID != "" {
		query = query.Where("article_id = ?", articleID)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.Order("created_at DESC").Limit(limit).Offset(offset).Find(&fileList).Error
	return fileList, total, err
}

func (r *fileRepository) FindByID(countryID database.CountryID, id uint64) (*models.File, error) {
	db := r.GetDB(countryID)
	var file models.File
	err := db.First(&file, id).Error
	return &file, err
}

func (r *fileRepository) GetFileWithParent(countryID database.CountryID, id uint64) (*models.File, interface{}, string, error) {
	file, err := r.FindByID(countryID, id)
	if err != nil {
		return nil, nil, "", err
	}

	db := r.GetDB(countryID)
	var item interface{}
	var itemType string

	if file.ArticleID != nil {
		var article models.Article
		if err := db.Preload("Subject").Preload("Semester").
			First(&article, *file.ArticleID).Error; err == nil {
			item = &article
			itemType = "article"
		}
	} else if file.PostID != nil {
		var post models.Post
		if err := db.Preload("Category").
			First(&post, *file.PostID).Error; err == nil {
			item = &post
			itemType = "post"
		}
	}

	return file, item, itemType, nil
}

func (r *fileRepository) IncrementView(countryID database.CountryID, id uint64) error {
	db := r.GetDB(countryID)
	return db.Model(&models.File{}).Where("id = ?", id).
		UpdateColumn("view_count", gorm.Expr("view_count + 1")).Error
}

func (r *fileRepository) Create(countryID database.CountryID, file *models.File) error {
	db := r.GetDB(countryID)
	return db.Omit("ViewCount").Create(file).Error
}

func (r *fileRepository) Update(countryID database.CountryID, file *models.File) error {
	db := r.GetDB(countryID)
	return db.Save(file).Error
}

func (r *fileRepository) Delete(countryID database.CountryID, file *models.File) error {
	db := r.GetDB(countryID)
	return db.Delete(file).Error
}
