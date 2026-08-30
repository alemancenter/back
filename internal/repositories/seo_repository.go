package repositories

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SEOContent is the small, stable content projection needed by ImanSEO. It
// prevents the SEO layer from depending on either editor's full domain model.
type SEOContent struct {
	ContentType     string     `json:"content_type"`
	ContentID       uint       `json:"content_id"`
	Title           string     `json:"title"`
	Content         string     `json:"content,omitempty"`
	Description     string     `json:"description"`
	Keywords        string     `json:"keywords"`
	ImageURL        string     `json:"image_url"`
	AuthorID        *uint      `json:"author_id,omitempty"`
	Published       bool       `json:"published"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at"`
	SEOMetadataID   *uint      `json:"seo_metadata_id,omitempty"`
	SEOScore        int        `json:"seo_score"`
	RobotsIndex     *bool      `json:"robots_index,omitempty"`
	FocusKeyword    string     `json:"focus_keyword"`
	MetaDescription string     `json:"seo_meta_description"`
}

type SEORepository interface {
	FindMetadata(context.Context, database.CountryID, string, uint) (*models.SEOMetadata, error)
	SaveMetadata(context.Context, database.CountryID, *models.SEOMetadata) error
	NextRevisionVersion(context.Context, database.CountryID, uint) (int, error)
	CreateRevision(context.Context, database.CountryID, *models.SEORevision) error
	ListRevisions(context.Context, database.CountryID, uint, int) ([]models.SEORevision, error)
	GetRevision(context.Context, database.CountryID, uint, uint) (*models.SEORevision, error)
	GetContent(context.Context, database.CountryID, string, uint) (*SEOContent, error)
	ListContent(context.Context, database.CountryID, string, string, int, int) ([]SEOContent, int64, error)
	MetadataStats(context.Context, database.CountryID) (map[string]int64, error)

	CreateRedirect(context.Context, database.CountryID, *models.SEORedirect) error
	UpdateRedirect(context.Context, database.CountryID, *models.SEORedirect) error
	GetRedirect(context.Context, database.CountryID, uint) (*models.SEORedirect, error)
	FindExactRedirect(context.Context, database.CountryID, string) (*models.SEORedirect, error)
	ListCandidateRedirects(context.Context, database.CountryID) ([]models.SEORedirect, error)
	ListRedirects(context.Context, database.CountryID, string, int, int) ([]models.SEORedirect, int64, error)
	DeleteRedirect(context.Context, database.CountryID, uint) error
	RecordRedirectHit(context.Context, database.CountryID, uint) error

	Record404(context.Context, database.CountryID, *models.SEO404Log) error
	List404(context.Context, database.CountryID, bool, string, int, int) ([]models.SEO404Log, int64, error)
	Resolve404(context.Context, database.CountryID, uint, *uint) error
	Clear404(context.Context, database.CountryID, bool) error

	CreateAuditRun(context.Context, database.CountryID, *models.SEOAuditRun) error
	FindRunningAudit(context.Context, database.CountryID) (*models.SEOAuditRun, error)
	UpdateAuditRun(context.Context, database.CountryID, *models.SEOAuditRun) error
	ReplaceAuditIssues(context.Context, database.CountryID, uint, []models.SEOIssue) error
	GetAuditRun(context.Context, database.CountryID, uint) (*models.SEOAuditRun, error)
	ListAuditRuns(context.Context, database.CountryID, int) ([]models.SEOAuditRun, error)
	ListAuditIssues(context.Context, database.CountryID, uint, string, int, int) ([]models.SEOIssue, int64, error)

	UpsertAuthor(context.Context, *models.SEOAuthorProfile) error
	UserExists(context.Context, uint) (bool, error)
	GetAuthor(context.Context, uint) (*models.SEOAuthorProfile, error)
	ListAuthors(context.Context, bool) ([]models.SEOAuthorProfile, error)
	CreateIndexNowSubmission(context.Context, database.CountryID, *models.SEOIndexNowSubmission) error
	UpdateIndexNowSubmission(context.Context, database.CountryID, *models.SEOIndexNowSubmission) error
}

type seoRepository struct{}

func NewSEORepository() SEORepository { return &seoRepository{} }

func seoDB(countryID database.CountryID) *gorm.DB { return database.DBForCountry(countryID) }

func (r *seoRepository) FindMetadata(ctx context.Context, countryID database.CountryID, contentType string, contentID uint) (*models.SEOMetadata, error) {
	var item models.SEOMetadata
	err := seoDB(countryID).WithContext(ctx).Where("content_type = ? AND content_id = ?", contentType, contentID).First(&item).Error
	return &item, err
}

func (r *seoRepository) SaveMetadata(ctx context.Context, countryID database.CountryID, item *models.SEOMetadata) error {
	return seoDB(countryID).WithContext(ctx).Save(item).Error
}

func (r *seoRepository) NextRevisionVersion(ctx context.Context, countryID database.CountryID, metadataID uint) (int, error) {
	var version int
	err := seoDB(countryID).WithContext(ctx).Model(&models.SEORevision{}).
		Where("metadata_id = ?", metadataID).Select("COALESCE(MAX(version), 0) + 1").Scan(&version).Error
	return version, err
}

func (r *seoRepository) CreateRevision(ctx context.Context, countryID database.CountryID, item *models.SEORevision) error {
	return seoDB(countryID).WithContext(ctx).Create(item).Error
}

func (r *seoRepository) ListRevisions(ctx context.Context, countryID database.CountryID, metadataID uint, limit int) ([]models.SEORevision, error) {
	var rows []models.SEORevision
	err := seoDB(countryID).WithContext(ctx).Where("metadata_id = ?", metadataID).Order("version DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *seoRepository) GetRevision(ctx context.Context, countryID database.CountryID, metadataID, revisionID uint) (*models.SEORevision, error) {
	var row models.SEORevision
	err := seoDB(countryID).WithContext(ctx).Where("metadata_id = ? AND id = ?", metadataID, revisionID).First(&row).Error
	return &row, err
}

func (r *seoRepository) GetContent(ctx context.Context, countryID database.CountryID, contentType string, id uint) (*SEOContent, error) {
	var row SEOContent
	db := seoDB(countryID).WithContext(ctx)
	switch contentType {
	case models.SEOContentTypeArticle:
		err := db.Table("articles a").
			Select("'article' content_type, a.id content_id, a.title, a.content, COALESCE(a.meta_description, '') description, '' keywords, '' image_url, a.author_id, (a.status = 1) published, a.published_at, a.updated_at").
			Where("a.id = ?", id).Scan(&row).Error
		if err != nil {
			return nil, err
		}
	case models.SEOContentTypePost:
		err := db.Table("posts p").
			Select("'post' content_type, p.id content_id, p.title, p.content, COALESCE(p.meta_description, '') description, COALESCE(p.keywords, '') keywords, COALESCE(p.image, '') image_url, p.author_id, p.is_active published, p.created_at published_at, p.updated_at").
			Where("p.id = ?", id).Scan(&row).Error
		if err != nil {
			return nil, err
		}
	default:
		return nil, gorm.ErrRecordNotFound
	}
	if row.ContentID == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &row, nil
}

func (r *seoRepository) ListContent(ctx context.Context, countryID database.CountryID, contentType, search string, limit, offset int) ([]SEOContent, int64, error) {
	db := seoDB(countryID).WithContext(ctx)
	if contentType == models.SEOContentTypeArticle || contentType == models.SEOContentTypePost {
		return r.listOneContentType(db, contentType, search, limit, offset)
	}
	// Fetch enough rows from each independently sorted table to cover the
	// requested merged window, then sort once across both types. Fetching only
	// `limit` rows from each made page 3+ appear empty even when older content
	// still existed.
	window := offset + limit
	articleRows, articleTotal, err := r.listOneContentType(db, models.SEOContentTypeArticle, search, window, 0)
	if err != nil {
		return nil, 0, err
	}
	postRows, postTotal, err := r.listOneContentType(db, models.SEOContentTypePost, search, window, 0)
	if err != nil {
		return nil, 0, err
	}
	rows := append(articleRows, postRows...)
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].UpdatedAt.Equal(rows[j].UpdatedAt) {
			if rows[i].ContentType == rows[j].ContentType {
				return rows[i].ContentID > rows[j].ContentID
			}
			return rows[i].ContentType < rows[j].ContentType
		}
		return rows[i].UpdatedAt.After(rows[j].UpdatedAt)
	})
	if offset >= len(rows) {
		return []SEOContent{}, articleTotal + postTotal, nil
	}
	end := offset + limit
	if end > len(rows) {
		end = len(rows)
	}
	return rows[offset:end], articleTotal + postTotal, nil
}

func (r *seoRepository) listOneContentType(db *gorm.DB, contentType, search string, limit, offset int) ([]SEOContent, int64, error) {
	var rows []SEOContent
	var total int64
	search = strings.TrimSpace(search)
	if contentType == models.SEOContentTypeArticle {
		base := db.Table("articles a")
		if search != "" {
			base = base.Where("a.title LIKE ?", "%"+search+"%")
		}
		if err := base.Count(&total).Error; err != nil {
			return nil, 0, err
		}
		err := base.Select("'article' content_type, a.id content_id, a.title, '' content, COALESCE(a.meta_description, '') description, '' keywords, '' image_url, a.author_id, (a.status = 1) published, a.published_at, a.updated_at, sm.id seo_metadata_id, COALESCE(sm.score, 0) seo_score, sm.robots_index, COALESCE(sm.focus_keyword, '') focus_keyword, COALESCE(sm.meta_description, '') seo_meta_description").
			Joins("LEFT JOIN seo_metadata sm ON sm.content_type = 'article' AND sm.content_id = a.id").Order("a.updated_at DESC").Limit(limit).Offset(offset).Scan(&rows).Error
		return rows, total, err
	}
	base := db.Table("posts p")
	if search != "" {
		base = base.Where("p.title LIKE ?", "%"+search+"%")
	}
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := base.Select("'post' content_type, p.id content_id, p.title, '' content, COALESCE(p.meta_description, '') description, COALESCE(p.keywords, '') keywords, COALESCE(p.image, '') image_url, p.author_id, p.is_active published, p.created_at published_at, p.updated_at, sm.id seo_metadata_id, COALESCE(sm.score, 0) seo_score, sm.robots_index, COALESCE(sm.focus_keyword, '') focus_keyword, COALESCE(sm.meta_description, '') seo_meta_description").
		Joins("LEFT JOIN seo_metadata sm ON sm.content_type = 'post' AND sm.content_id = p.id").Order("p.updated_at DESC").Limit(limit).Offset(offset).Scan(&rows).Error
	return rows, total, err
}

func (r *seoRepository) MetadataStats(ctx context.Context, countryID database.CountryID) (map[string]int64, error) {
	type stat struct {
		Key   string
		Count int64
	}
	var rows []stat
	err := seoDB(countryID).WithContext(ctx).Model(&models.SEOMetadata{}).
		Where("(content_type = 'article' AND EXISTS (SELECT 1 FROM articles a WHERE a.id = seo_metadata.content_id)) OR (content_type = 'post' AND EXISTS (SELECT 1 FROM posts p WHERE p.id = seo_metadata.content_id))").
		Select("CASE WHEN robots_index = 0 THEN 'noindex' WHEN score >= 80 THEN 'good' WHEN score >= 50 THEN 'needs_work' ELSE 'poor' END AS `key`, COUNT(*) AS count").
		Group("`key`").Scan(&rows).Error
	result := map[string]int64{"good": 0, "needs_work": 0, "poor": 0, "noindex": 0}
	for _, row := range rows {
		result[row.Key] = row.Count
	}
	return result, err
}

func (r *seoRepository) CreateRedirect(ctx context.Context, countryID database.CountryID, item *models.SEORedirect) error {
	return seoDB(countryID).WithContext(ctx).Create(item).Error
}
func (r *seoRepository) UpdateRedirect(ctx context.Context, countryID database.CountryID, item *models.SEORedirect) error {
	return seoDB(countryID).WithContext(ctx).Save(item).Error
}
func (r *seoRepository) GetRedirect(ctx context.Context, countryID database.CountryID, id uint) (*models.SEORedirect, error) {
	var item models.SEORedirect
	err := seoDB(countryID).WithContext(ctx).First(&item, id).Error
	return &item, err
}
func (r *seoRepository) FindExactRedirect(ctx context.Context, countryID database.CountryID, hash string) (*models.SEORedirect, error) {
	var item models.SEORedirect
	err := seoDB(countryID).WithContext(ctx).Where("source_hash = ? AND match_type = ? AND active = 1", hash, models.SEORedirectMatchExact).First(&item).Error
	return &item, err
}
func (r *seoRepository) ListCandidateRedirects(ctx context.Context, countryID database.CountryID) ([]models.SEORedirect, error) {
	var rows []models.SEORedirect
	err := seoDB(countryID).WithContext(ctx).Where("active = 1 AND match_type <> ?", models.SEORedirectMatchExact).Order("LENGTH(source_path) DESC").Limit(500).Find(&rows).Error
	return rows, err
}
func (r *seoRepository) ListRedirects(ctx context.Context, countryID database.CountryID, search string, limit, offset int) ([]models.SEORedirect, int64, error) {
	var rows []models.SEORedirect
	var total int64
	q := seoDB(countryID).WithContext(ctx).Model(&models.SEORedirect{})
	if strings.TrimSpace(search) != "" {
		q = q.Where("source_path LIKE ? OR target_url LIKE ?", "%"+search+"%", "%"+search+"%")
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("updated_at DESC").Limit(limit).Offset(offset).Find(&rows).Error
	return rows, total, err
}
func (r *seoRepository) DeleteRedirect(ctx context.Context, countryID database.CountryID, id uint) error {
	return seoDB(countryID).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Delete(&models.SEORedirect{}, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return tx.Model(&models.SEO404Log{}).Where("redirect_id = ?", id).
			Updates(map[string]interface{}{"resolved": false, "redirect_id": nil}).Error
	})
}
func (r *seoRepository) RecordRedirectHit(ctx context.Context, countryID database.CountryID, id uint) error {
	return seoDB(countryID).WithContext(ctx).Model(&models.SEORedirect{}).Where("id = ?", id).
		Updates(map[string]interface{}{"hit_count": gorm.Expr("hit_count + 1"), "last_hit_at": time.Now().UTC()}).Error
}

func (r *seoRepository) Record404(ctx context.Context, countryID database.CountryID, item *models.SEO404Log) error {
	return seoDB(countryID).WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "country_code"}, {Name: "path_hash"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"path": item.Path, "last_query": item.LastQuery, "last_referrer": item.LastReferrer,
			"last_user_agent": item.LastUserAgent, "last_seen_at": item.LastSeenAt,
			"hit_count": gorm.Expr("hit_count + 1"),
		}),
	}).Create(item).Error
}
func (r *seoRepository) List404(ctx context.Context, countryID database.CountryID, resolved bool, search string, limit, offset int) ([]models.SEO404Log, int64, error) {
	var rows []models.SEO404Log
	var total int64
	q := seoDB(countryID).WithContext(ctx).Model(&models.SEO404Log{}).Where("resolved = ?", resolved)
	if strings.TrimSpace(search) != "" {
		q = q.Where("path LIKE ?", "%"+search+"%")
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("last_seen_at DESC").Limit(limit).Offset(offset).Find(&rows).Error
	return rows, total, err
}
func (r *seoRepository) Resolve404(ctx context.Context, countryID database.CountryID, id uint, redirectID *uint) error {
	result := seoDB(countryID).WithContext(ctx).Model(&models.SEO404Log{}).Where("id = ?", id).Updates(map[string]interface{}{"resolved": true, "redirect_id": redirectID})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
func (r *seoRepository) Clear404(ctx context.Context, countryID database.CountryID, resolved bool) error {
	return seoDB(countryID).WithContext(ctx).Where("resolved = ?", resolved).Delete(&models.SEO404Log{}).Error
}

func (r *seoRepository) CreateAuditRun(ctx context.Context, countryID database.CountryID, run *models.SEOAuditRun) error {
	return seoDB(countryID).WithContext(ctx).Create(run).Error
}
func (r *seoRepository) FindRunningAudit(ctx context.Context, countryID database.CountryID) (*models.SEOAuditRun, error) {
	var run models.SEOAuditRun
	err := seoDB(countryID).WithContext(ctx).Where("status = ?", models.SEOAuditStatusRunning).Order("started_at DESC").First(&run).Error
	return &run, err
}
func (r *seoRepository) UpdateAuditRun(ctx context.Context, countryID database.CountryID, run *models.SEOAuditRun) error {
	return seoDB(countryID).WithContext(ctx).Save(run).Error
}
func (r *seoRepository) ReplaceAuditIssues(ctx context.Context, countryID database.CountryID, runID uint, issues []models.SEOIssue) error {
	return seoDB(countryID).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("run_id = ?", runID).Delete(&models.SEOIssue{}).Error; err != nil {
			return err
		}
		if len(issues) == 0 {
			return nil
		}
		return tx.CreateInBatches(issues, 100).Error
	})
}
func (r *seoRepository) GetAuditRun(ctx context.Context, countryID database.CountryID, id uint) (*models.SEOAuditRun, error) {
	var row models.SEOAuditRun
	err := seoDB(countryID).WithContext(ctx).First(&row, id).Error
	return &row, err
}
func (r *seoRepository) ListAuditRuns(ctx context.Context, countryID database.CountryID, limit int) ([]models.SEOAuditRun, error) {
	var rows []models.SEOAuditRun
	err := seoDB(countryID).WithContext(ctx).Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}
func (r *seoRepository) ListAuditIssues(ctx context.Context, countryID database.CountryID, runID uint, severity string, limit, offset int) ([]models.SEOIssue, int64, error) {
	var rows []models.SEOIssue
	var total int64
	q := seoDB(countryID).WithContext(ctx).Model(&models.SEOIssue{}).Where("run_id = ?", runID)
	if severity != "" {
		q = q.Where("severity = ?", severity)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("FIELD(severity, 'error', 'warning', 'notice'), id").Limit(limit).Offset(offset).Find(&rows).Error
	return rows, total, err
}

func (r *seoRepository) UpsertAuthor(ctx context.Context, item *models.SEOAuthorProfile) error {
	return database.DB().WithContext(ctx).Where("user_id = ?", item.UserID).Assign(item).FirstOrCreate(item).Error
}
func (r *seoRepository) UserExists(ctx context.Context, userID uint) (bool, error) {
	var count int64
	err := database.DB().WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Count(&count).Error
	return count > 0, err
}
func (r *seoRepository) GetAuthor(ctx context.Context, userID uint) (*models.SEOAuthorProfile, error) {
	var row models.SEOAuthorProfile
	err := database.DB().WithContext(ctx).Where("user_id = ?", userID).First(&row).Error
	return &row, err
}
func (r *seoRepository) ListAuthors(ctx context.Context, onlyActive bool) ([]models.SEOAuthorProfile, error) {
	var rows []models.SEOAuthorProfile
	q := database.DB().WithContext(ctx)
	if onlyActive {
		q = q.Where("active = 1")
	}
	err := q.Order("public_name").Find(&rows).Error
	return rows, err
}
func (r *seoRepository) CreateIndexNowSubmission(ctx context.Context, countryID database.CountryID, item *models.SEOIndexNowSubmission) error {
	return seoDB(countryID).WithContext(ctx).Create(item).Error
}
func (r *seoRepository) UpdateIndexNowSubmission(ctx context.Context, countryID database.CountryID, item *models.SEOIndexNowSubmission) error {
	return seoDB(countryID).WithContext(ctx).Save(item).Error
}

var _ SEORepository = (*seoRepository)(nil)
