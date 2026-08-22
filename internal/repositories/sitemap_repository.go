package repositories

import (
	"strconv"
	"strings"
	"time"

	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/models"
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
	GetLatestQualityDecisions(dbCode, contentType string) (map[uint]models.ContentAIDecision, error)
	GetIndexableDownloads(dbCode string) ([]struct {
		ID        uint      `gorm:"column:id"`
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

// GetLatestQualityDecisions returns at most one saved audit decision per content ID.
// Rows are ordered newest-first with an ID tie-breaker, then collapsed in memory
// to avoid DB-specific window-function SQL in sitemap generation.
func (r *sitemapRepository) GetLatestQualityDecisions(dbCode, contentType string) (map[uint]models.ContentAIDecision, error) {
	var rows []models.ContentAIDecision
	err := database.DB().
		Select("id, content_type, content_id, country_code, decision, adsense_risk, score, created_at").
		Where("content_type = ? AND country_code = ?", contentType, dbCode).
		Order("created_at DESC, id DESC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}

	latest := make(map[uint]models.ContentAIDecision, len(rows))
	for _, row := range rows {
		rawID := strings.TrimSpace(row.ContentID)
		if strings.Contains(rawID, ":") {
			parts := strings.Split(rawID, ":")
			rawID = parts[len(parts)-1]
		}
		parsed, parseErr := strconv.ParseUint(rawID, 10, 64)
		if parseErr != nil || parsed == 0 {
			continue
		}
		id := uint(parsed)
		if _, exists := latest[id]; exists {
			continue
		}
		latest[id] = row
	}
	return latest, nil
}

func (r *sitemapRepository) GetIndexableDownloads(dbCode string) ([]struct {
	ID        uint      `gorm:"column:id"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}, error) {
	db := database.GetManager().GetByCode(dbCode)

	var rows []struct {
		ID        uint      `gorm:"column:id"`
		UpdatedAt time.Time `gorm:"column:updated_at"`
	}

	err := db.Raw(`
		SELECT DISTINCT
			f.id,
			f.updated_at
		FROM files AS f
		LEFT JOIN articles AS a
			ON a.id = f.article_id
		LEFT JOIN posts AS p
			ON p.id = f.post_id
		WHERE
			(f.article_id IS NOT NULL AND a.status = 1)
			OR
			(f.article_id IS NULL AND f.post_id IS NOT NULL AND p.is_active = 1)
		ORDER BY f.id
	`).Scan(&rows).Error

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
