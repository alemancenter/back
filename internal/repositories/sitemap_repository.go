package repositories

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/imanjo/fiber-api/internal/contentquality"
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
	GetCorruptedContentIDs(dbCode, contentType string) (map[uint]struct{}, error)
	GetIndexableDownloads(dbCode string) ([]struct {
		ID        uint      `gorm:"column:id"`
		UpdatedAt time.Time `gorm:"column:updated_at"`
	}, error)
	GetActiveCategories(dbCode string) ([]models.Category, error)
	GetActiveSchoolClasses(dbCode string) ([]models.SchoolClass, error)
	GetSitemapImages(dbCode string) ([]SitemapImage, error)
	GetSitemapVideos(dbCode string) ([]SitemapVideo, error)
	GetRecentNews(dbCode string, since time.Time) ([]SitemapNews, error)
	GetSitemapFeatures(dbCode string) (map[string]bool, error)
}

type SitemapImage struct {
	ContentType string `gorm:"column:content_type"`
	ContentID   uint   `gorm:"column:content_id"`
	Path        string `gorm:"column:path"`
	Title       string `gorm:"column:title"`
}

type SitemapVideo struct {
	ContentType string `gorm:"column:content_type"`
	ContentID   uint   `gorm:"column:content_id"`
	Path        string `gorm:"column:path"`
	Thumbnail   string `gorm:"column:thumbnail"`
	Title       string `gorm:"column:title"`
	Description string `gorm:"column:description"`
}

type SitemapNews struct {
	ID          uint      `gorm:"column:id"`
	Title       string    `gorm:"column:title"`
	PublishedAt time.Time `gorm:"column:published_at"`
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
	if err := db.Raw(`SELECT a.id, a.updated_at FROM articles a
		LEFT JOIN seo_metadata sm ON sm.content_type = 'article' AND sm.content_id = a.id
		WHERE a.status = 1 AND (sm.id IS NULL OR sm.robots_index = 1)`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	manualNoindex, err := editorialNoindexIDs(dbCode, "article")
	if err != nil {
		return nil, err
	}
	if len(manualNoindex) == 0 {
		return rows, nil
	}
	filtered := rows[:0]
	for _, row := range rows {
		if _, blocked := manualNoindex[row.ID]; !blocked {
			filtered = append(filtered, row)
		}
	}
	return filtered, nil
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
	if err := db.Raw(`SELECT p.id, p.slug, p.updated_at FROM posts p
		LEFT JOIN seo_metadata sm ON sm.content_type = 'post' AND sm.content_id = p.id
		WHERE p.is_active = 1 AND (sm.id IS NULL OR sm.robots_index = 1)`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	manualNoindex, err := editorialNoindexIDs(dbCode, "post")
	if err != nil {
		return nil, err
	}
	if len(manualNoindex) == 0 {
		return rows, nil
	}
	filtered := rows[:0]
	for _, row := range rows {
		if _, blocked := manualNoindex[row.ID]; !blocked {
			filtered = append(filtered, row)
		}
	}
	return filtered, nil
}

// GetLatestQualityDecisions returns at most one saved audit decision per content ID.
// Rows are ordered newest-first with an ID tie-breaker, then collapsed in memory
// to avoid DB-specific window-function SQL in sitemap generation.
func (r *sitemapRepository) GetLatestQualityDecisions(dbCode, contentType string) (map[uint]models.ContentAIDecision, error) {
	var rows []models.ContentAIDecision
	err := database.DB().
		// Column is ad_sense_risk (GORM's snake_case of AdSenseRisk), not adsense_risk.
		Select("id, content_type, content_id, country_code, decision, ad_sense_risk, score, created_at").
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

// GetCorruptedContentIDs returns published content whose current source still
// contains an unresolved replacement artifact. The SQL only prefilters rows
// containing '$'; the canonical detector makes the final decision so sitemap,
// public quality status and the dashboard cannot drift apart.
func (r *sitemapRepository) GetCorruptedContentIDs(dbCode, contentType string) (map[uint]struct{}, error) {
	db := database.GetManager().GetByCode(dbCode)
	type candidate struct {
		ID              uint   `gorm:"column:id"`
		Title           string `gorm:"column:title"`
		Content         string `gorm:"column:content"`
		MetaDescription string `gorm:"column:meta_description"`
		Keywords        string `gorm:"column:keywords"`
	}
	var rows []candidate
	var err error
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "article":
		err = db.Raw(`SELECT id, title, content, COALESCE(meta_description, '') AS meta_description, '' AS keywords
			FROM articles
			WHERE status = 1 AND (title LIKE '%$%' OR content LIKE '%$%' OR COALESCE(meta_description, '') LIKE '%$%')`).Scan(&rows).Error
	case "post":
		err = db.Raw(`SELECT id, title, content, COALESCE(meta_description, '') AS meta_description, COALESCE(keywords, '') AS keywords
			FROM posts
			WHERE is_active = 1 AND (title LIKE '%$%' OR content LIKE '%$%' OR COALESCE(meta_description, '') LIKE '%$%' OR COALESCE(keywords, '') LIKE '%$%')`).Scan(&rows).Error
	default:
		return nil, fmt.Errorf("unsupported sitemap content type: %s", contentType)
	}
	if err != nil {
		return nil, err
	}

	ids := make(map[uint]struct{})
	for _, row := range rows {
		artifacts := contentquality.DetectReplacementArtifacts(
			contentquality.TextField{Name: "title", Value: row.Title},
			contentquality.TextField{Name: "content", Value: row.Content},
			contentquality.TextField{Name: "meta_description", Value: row.MetaDescription},
			contentquality.TextField{Name: "keywords", Value: row.Keywords},
		)
		if len(artifacts) > 0 {
			ids[row.ID] = struct{}{}
		}
	}
	return ids, nil
}

// editorialNoindexIDs returns content whose latest human decision is NOINDEX.
// A later classification supersedes it without deleting history.
func editorialNoindexIDs(dbCode, contentType string) (map[uint]struct{}, error) {
	var rows []models.ContentEditorialDecision
	if err := database.DB().
		Where("country_code = ? AND content_type = ?", dbCode, contentType).
		Order("created_at DESC, id DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	seen := make(map[uint]struct{}, len(rows))
	noindex := make(map[uint]struct{})
	for _, row := range rows {
		if row.ContentID == 0 {
			continue
		}
		if _, exists := seen[row.ContentID]; exists {
			continue
		}
		seen[row.ContentID] = struct{}{}
		if strings.EqualFold(strings.TrimSpace(row.Decision), models.EditorialDecisionNoindex) {
			noindex[row.ContentID] = struct{}{}
		}
	}
	return noindex, nil
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

func (r *sitemapRepository) GetSitemapImages(dbCode string) ([]SitemapImage, error) {
	db := database.GetManager().GetByCode(dbCode)
	var rows []SitemapImage
	err := db.Raw(`
		SELECT 'post' content_type, p.id content_id, p.image path, COALESCE(NULLIF(p.alt, ''), p.title) title
		FROM posts p
		LEFT JOIN seo_metadata sm ON sm.content_type = 'post' AND sm.content_id = p.id
		WHERE p.is_active = 1 AND p.image IS NOT NULL AND p.image <> '' AND (sm.id IS NULL OR sm.robots_index = 1)
		UNION ALL
		SELECT CASE WHEN f.article_id IS NOT NULL THEN 'article' ELSE 'post' END content_type,
			COALESCE(f.article_id, f.post_id) content_id, f.file_path path, COALESCE(NULLIF(f.file_name, ''), 'صورة المحتوى') title
		FROM files f
		LEFT JOIN articles a ON a.id = f.article_id
		LEFT JOIN posts p ON p.id = f.post_id
		LEFT JOIN seo_metadata sm ON sm.content_type = CASE WHEN f.article_id IS NOT NULL THEN 'article' ELSE 'post' END
			AND sm.content_id = COALESCE(f.article_id, f.post_id)
		WHERE f.mime_type LIKE 'image/%'
			AND ((f.article_id IS NOT NULL AND a.status = 1) OR (f.article_id IS NULL AND f.post_id IS NOT NULL AND p.is_active = 1))
			AND (sm.id IS NULL OR sm.robots_index = 1)
	`).Scan(&rows).Error
	return rows, err
}

func (r *sitemapRepository) GetSitemapVideos(dbCode string) ([]SitemapVideo, error) {
	db := database.GetManager().GetByCode(dbCode)
	var rows []SitemapVideo
	err := db.Raw(`
		SELECT CASE WHEN f.article_id IS NOT NULL THEN 'article' ELSE 'post' END content_type,
			COALESCE(f.article_id, f.post_id) content_id, f.file_path path,
			COALESCE(
				NULLIF(CASE WHEN f.post_id IS NOT NULL THEN p.image ELSE '' END, ''),
				(SELECT fi.file_path FROM files fi
				 WHERE fi.mime_type LIKE 'image/%'
				   AND ((f.article_id IS NOT NULL AND fi.article_id = f.article_id) OR (f.post_id IS NOT NULL AND fi.post_id = f.post_id))
				 ORDER BY fi.id LIMIT 1), ''
			) thumbnail,
			COALESCE(NULLIF(f.file_name, ''), CASE WHEN f.article_id IS NOT NULL THEN a.title ELSE p.title END) title,
			COALESCE(CASE WHEN f.article_id IS NOT NULL THEN a.meta_description ELSE p.meta_description END, '') description
		FROM files f
		LEFT JOIN articles a ON a.id = f.article_id
		LEFT JOIN posts p ON p.id = f.post_id
		LEFT JOIN seo_metadata sm ON sm.content_type = CASE WHEN f.article_id IS NOT NULL THEN 'article' ELSE 'post' END
			AND sm.content_id = COALESCE(f.article_id, f.post_id)
		WHERE f.mime_type LIKE 'video/%'
			AND ((f.article_id IS NOT NULL AND a.status = 1) OR (f.article_id IS NULL AND f.post_id IS NOT NULL AND p.is_active = 1))
			AND (sm.id IS NULL OR sm.robots_index = 1)
	`).Scan(&rows).Error
	return rows, err
}

func (r *sitemapRepository) GetRecentNews(dbCode string, since time.Time) ([]SitemapNews, error) {
	db := database.GetManager().GetByCode(dbCode)
	var rows []SitemapNews
	err := db.Raw(`SELECT p.id, p.title, p.created_at published_at
		FROM posts p
		LEFT JOIN seo_metadata sm ON sm.content_type = 'post' AND sm.content_id = p.id
		WHERE p.is_active = 1 AND p.created_at >= ? AND (sm.id IS NULL OR sm.robots_index = 1)
		ORDER BY p.created_at DESC`, since.UTC()).Scan(&rows).Error
	return rows, err
}

func (r *sitemapRepository) GetSitemapFeatures(dbCode string) (map[string]bool, error) {
	db := database.GetManager().GetByCode(dbCode)
	var rows []models.Setting
	err := db.Where("`key` IN ?", []string{"image_sitemap_enabled", "video_sitemap_enabled", "news_sitemap_enabled"}).Find(&rows).Error
	features := map[string]bool{"images": true, "videos": true, "news": false}
	for _, row := range rows {
		value := row.Value != nil && (*row.Value == "1" || strings.EqualFold(*row.Value, "true") || strings.EqualFold(*row.Value, "on"))
		switch row.Key {
		case "image_sitemap_enabled":
			features["images"] = value
		case "video_sitemap_enabled":
			features["videos"] = value
		case "news_sitemap_enabled":
			features["news"] = value
		}
	}
	return features, err
}
