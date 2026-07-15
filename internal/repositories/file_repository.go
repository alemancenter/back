package repositories

import (
	"strings"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"gorm.io/gorm"
)

// FileListFilter holds the dashboard file-list filtering and sorting options.
type FileListFilter struct {
	FileType     string
	FileCategory string
	ArticleID    string
	PostID       string
	// Search matches the stored file name (LIKE).
	Search string
	// SortBy is one of: created_at, file_name, file_size, download_count, view_count.
	SortBy string
	// SortDir is "asc" or "desc" (defaults to desc).
	SortDir string
	Limit   int
	Offset  int
}

// allowedFileSortColumns whitelists sortable columns — never interpolate a
// user-supplied column name into SQL.
var allowedFileSortColumns = map[string]string{
	"created_at":     "created_at",
	"file_name":      "file_name",
	"file_size":      "file_size",
	"download_count": "download_count",
	"view_count":     "view_count",
}

type FileRepository interface {
	ListPaginated(countryID database.CountryID, filter FileListFilter) ([]models.File, int64, error)
	FindByID(countryID database.CountryID, id uint64) (*models.File, error)
	GetFileWithParent(countryID database.CountryID, id uint64) (*models.File, interface{}, string, error)
	IncrementView(countryID database.CountryID, id uint64) error
	IncrementDownload(countryID database.CountryID, id uint64) error
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

func (r *fileRepository) ListPaginated(countryID database.CountryID, filter FileListFilter) ([]models.File, int64, error) {
	db := r.GetDB(countryID)
	var fileList []models.File
	var total int64

	query := db.Model(&models.File{})
	if filter.FileType != "" {
		query = query.Where("file_type = ?", filter.FileType)
	}
	if filter.FileCategory != "" {
		query = query.Where("file_category = ?", filter.FileCategory)
	}
	if filter.ArticleID != "" {
		query = query.Where("article_id = ?", filter.ArticleID)
	}
	if filter.PostID != "" {
		query = query.Where("post_id = ?", filter.PostID)
	}
	if filter.Search != "" {
		query = query.Where("file_name LIKE ?", "%"+filter.Search+"%")
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	column, ok := allowedFileSortColumns[filter.SortBy]
	if !ok {
		column = "created_at"
	}
	direction := "DESC"
	if strings.EqualFold(filter.SortDir, "asc") {
		direction = "ASC"
	}

	err := query.
		Order(column + " " + direction).
		Limit(filter.Limit).
		Offset(filter.Offset).
		Find(&fileList).Error
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
	return incrementExistingFileCounterColumns(db, id, []string{"view_count", "views_count"})
}

func (r *fileRepository) IncrementDownload(countryID database.CountryID, id uint64) error {
	db := r.GetDB(countryID)
	return incrementExistingFileCounterColumns(db, id, []string{"download_count"})
}

func incrementExistingFileCounterColumns(db *gorm.DB, id uint64, columns []string) error {
	if db == nil {
		return nil
	}
	updated := false
	for _, col := range columns {
		if !db.Migrator().HasColumn(&models.File{}, col) {
			continue
		}
		if err := db.Exec("UPDATE files SET "+col+" = LEAST(COALESCE("+col+", 0) + 1, 2147483647) WHERE id = ?", id).Error; err != nil {
			return err
		}
		updated = true
	}
	if !updated {
		// Old installations may not have counters yet. Keep the request successful; the SQL migration can add them.
		return nil
	}
	return nil
}

func (r *fileRepository) Create(countryID database.CountryID, file *models.File) error {
	db := r.GetDB(countryID)
	return db.Omit("ViewCount", "ViewsCount", "DownloadCount").Create(file).Error
}

func (r *fileRepository) Update(countryID database.CountryID, file *models.File) error {
	db := r.GetDB(countryID)
	return db.Save(file).Error
}

func (r *fileRepository) Delete(countryID database.CountryID, file *models.File) error {
	db := r.GetDB(countryID)
	return db.Delete(file).Error
}
