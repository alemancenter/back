package repositories

import (
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
)

type SitemapRepository interface {
	GetSiteURL() (string, error)
	GetActiveArticles(dbCode string) ([]struct {
		ID        uint      `gorm:"column:id"`
		UpdatedAt time.Time `gorm:"column:updated_at"`
	}, error)
	GetActivePosts(dbCode string) ([]struct {
		ID        uint      `gorm:"column:id"`
		Slug      string    `gorm:"column:slug"`
		UpdatedAt time.Time `gorm:"column:updated_at"`
	}, error)
	GetActiveCategories(dbCode string) ([]models.Category, error)
	GetActiveSchoolClasses(dbCode string) ([]models.SchoolClass, error)
}

type sitemapRepository struct{}

func NewSitemapRepository() SitemapRepository {
	return &sitemapRepository{}
}

func (r *sitemapRepository) GetSiteURL() (string, error) {
	db := database.DB()
	var s models.Setting
	if err := db.Where("`key` = ?", "site_url").First(&s).Error; err != nil {
		return "", err
	}
	if s.Value != nil {
		return *s.Value, nil
	}
	return "", nil
}

func (r *sitemapRepository) GetActiveArticles(dbCode string) ([]struct {
	ID        uint      `gorm:"column:id"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}, error) {
	db := database.GetManager().GetByCode(dbCode)
	var rows []struct {
		ID        uint      `gorm:"column:id"`
		UpdatedAt time.Time `gorm:"column:updated_at"`
	}
	err := db.Raw("SELECT id, updated_at FROM articles WHERE status = 1").Scan(&rows).Error
	return rows, err
}

func (r *sitemapRepository) GetActivePosts(dbCode string) ([]struct {
	ID        uint      `gorm:"column:id"`
	Slug      string    `gorm:"column:slug"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}, error) {
	db := database.GetManager().GetByCode(dbCode)
	var rows []struct {
		ID        uint      `gorm:"column:id"`
		Slug      string    `gorm:"column:slug"`
		UpdatedAt time.Time `gorm:"column:updated_at"`
	}
	err := db.Raw("SELECT id, slug, updated_at FROM posts WHERE is_active = 1").Scan(&rows).Error
	return rows, err
}

func (r *sitemapRepository) GetActiveCategories(dbCode string) ([]models.Category, error) {
	db := database.GetManager().GetByCode(dbCode)
	var cats []models.Category
	err := db.Where("is_active = ?", true).Select("slug, updated_at").Find(&cats).Error
	return cats, err
}

func (r *sitemapRepository) GetActiveSchoolClasses(dbCode string) ([]models.SchoolClass, error) {
	db := database.GetManager().GetByCode(dbCode)
	var classes []models.SchoolClass
	err := db.Select("grade_level, updated_at").Find(&classes).Error
	return classes, err
}
