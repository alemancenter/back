package repositories

import (
	"context"
	"time"

	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/models"
	"gorm.io/gorm/clause"
)

// GSCRepository stores Search Console integration state. Like
// ContentAuditRepository, it deliberately uses the single shared
// database.DB() connection with a country_code column, not a per-country
// physical database — this is governance/tracking data, not content, and
// follows the same pattern as PolicyAuditRun/ContentAIDecision.
type GSCRepository interface {
	ListProperties(ctx context.Context) ([]models.GSCProperty, error)
	GetProperty(ctx context.Context, countryCode string) (*models.GSCProperty, error)
	UpsertProperty(ctx context.Context, property *models.GSCProperty) error

	UpsertURLStatus(ctx context.Context, status *models.GSCURLStatus) error
	GetURLStatus(ctx context.Context, contentType string, contentID uint, countryCode string) (*models.GSCURLStatus, error)
	StaleURLStatuses(ctx context.Context, countryCode string, olderThan time.Time, limit int) ([]models.GSCURLStatus, error)

	UpsertAnalyticsRows(ctx context.Context, rows []models.GSCSearchAnalyticsDaily) error
	ListAnalytics(ctx context.Context, countryCode, url string, since time.Time) ([]models.GSCSearchAnalyticsDaily, error)
	UpsertQueryRows(ctx context.Context, rows []models.GSCSearchQueryDaily) error
	ListKeywordAnalytics(ctx context.Context, countryCode, search string, since time.Time, limit, offset int) ([]GSCKeywordSummary, int64, error)

	CreateSyncRun(ctx context.Context, run *models.GSCSyncRun) error
	UpdateSyncRun(ctx context.Context, run *models.GSCSyncRun) error
	LatestSyncRun(ctx context.Context, countryCode, kind string) (*models.GSCSyncRun, error)
}

type GSCKeywordSummary struct {
	Query       string    `json:"query"`
	Clicks      int64     `json:"clicks"`
	Impressions int64     `json:"impressions"`
	CTR         float64   `json:"ctr"`
	Position    float64   `json:"position"`
	Pages       int64     `json:"pages"`
	LastDate    time.Time `json:"last_date"`
}

type gscRepository struct{}

func NewGSCRepository() GSCRepository {
	return &gscRepository{}
}

func (r *gscRepository) ListProperties(ctx context.Context) ([]models.GSCProperty, error) {
	var properties []models.GSCProperty
	if err := database.DB().WithContext(ctx).Order("country_code").Find(&properties).Error; err != nil {
		return nil, err
	}
	return properties, nil
}

func (r *gscRepository) GetProperty(ctx context.Context, countryCode string) (*models.GSCProperty, error) {
	var property models.GSCProperty
	if err := database.DB().WithContext(ctx).Where("country_code = ? AND active = 1", countryCode).First(&property).Error; err != nil {
		return nil, err
	}
	return &property, nil
}

func (r *gscRepository) UpsertProperty(ctx context.Context, property *models.GSCProperty) error {
	return database.DB().WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "country_code"}},
		DoUpdates: clause.AssignmentColumns([]string{"site_url", "active", "verified_at", "updated_at"}),
	}).Create(property).Error
}

func (r *gscRepository) UpsertURLStatus(ctx context.Context, status *models.GSCURLStatus) error {
	return database.DB().WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "content_type"}, {Name: "content_id"}, {Name: "country_code"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"url", "index_status", "coverage_verdict", "google_canonical", "user_canonical",
			"robots_txt_state", "last_crawl_time", "raw_response", "checked_at", "updated_at",
		}),
	}).Create(status).Error
}

func (r *gscRepository) GetURLStatus(ctx context.Context, contentType string, contentID uint, countryCode string) (*models.GSCURLStatus, error) {
	var status models.GSCURLStatus
	err := database.DB().WithContext(ctx).
		Where("content_type = ? AND content_id = ? AND country_code = ?", contentType, contentID, countryCode).
		First(&status).Error
	if err != nil {
		return nil, err
	}
	return &status, nil
}

// StaleURLStatuses returns rows last checked before olderThan, oldest first —
// used by the sync loop's slow round-robin refresh (see plan §4.3) so every
// tracked URL eventually gets re-checked without a full re-scan each run.
func (r *gscRepository) StaleURLStatuses(ctx context.Context, countryCode string, olderThan time.Time, limit int) ([]models.GSCURLStatus, error) {
	var rows []models.GSCURLStatus
	err := database.DB().WithContext(ctx).
		Where("country_code = ? AND checked_at < ?", countryCode, olderThan).
		Order("checked_at ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *gscRepository) UpsertAnalyticsRows(ctx context.Context, rows []models.GSCSearchAnalyticsDaily) error {
	if len(rows) == 0 {
		return nil
	}
	return database.DB().WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "country_code"}, {Name: "url"}, {Name: "date"}},
		DoUpdates: clause.AssignmentColumns([]string{"clicks", "impressions", "ctr", "position", "updated_at"}),
	}).CreateInBatches(&rows, 200).Error
}

// ListAnalytics returns rows since a given date for one country, optionally
// filtered to a single URL. Ordered oldest-first so callers can chart a trend
// directly without re-sorting.
func (r *gscRepository) ListAnalytics(ctx context.Context, countryCode, url string, since time.Time) ([]models.GSCSearchAnalyticsDaily, error) {
	query := database.DB().WithContext(ctx).
		Where("country_code = ? AND date >= ?", countryCode, since)
	if url != "" {
		query = query.Where("url = ?", url)
	}
	var rows []models.GSCSearchAnalyticsDaily
	if err := query.Order("date ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *gscRepository) UpsertQueryRows(ctx context.Context, rows []models.GSCSearchQueryDaily) error {
	if len(rows) == 0 {
		return nil
	}
	return database.DB().WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "country_code"}, {Name: "query_hash"}, {Name: "url_hash"}, {Name: "date"}},
		DoUpdates: clause.AssignmentColumns([]string{"query", "url", "clicks", "impressions", "ctr", "position", "updated_at"}),
	}).CreateInBatches(&rows, 200).Error
}

func (r *gscRepository) ListKeywordAnalytics(ctx context.Context, countryCode, search string, since time.Time, limit, offset int) ([]GSCKeywordSummary, int64, error) {
	db := database.DB().WithContext(ctx)
	base := db.Model(&models.GSCSearchQueryDaily{}).Where("country_code = ? AND date >= ?", countryCode, since)
	if search != "" {
		base = base.Where("query LIKE ?", "%"+search+"%")
	}
	var total int64
	if err := base.Distinct("query_hash").Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []GSCKeywordSummary
	err := base.Select(`query, SUM(clicks) clicks, SUM(impressions) impressions,
		CASE WHEN SUM(impressions) > 0 THEN SUM(clicks) / SUM(impressions) ELSE 0 END ctr,
		CASE WHEN SUM(impressions) > 0 THEN SUM(position * impressions) / SUM(impressions) ELSE AVG(position) END position,
		COUNT(DISTINCT url_hash) pages, MAX(date) last_date`).
		Group("query_hash, query").Order("impressions DESC, clicks DESC").Limit(limit).Offset(offset).Scan(&rows).Error
	return rows, total, err
}

func (r *gscRepository) CreateSyncRun(ctx context.Context, run *models.GSCSyncRun) error {
	return database.DB().WithContext(ctx).Create(run).Error
}

func (r *gscRepository) UpdateSyncRun(ctx context.Context, run *models.GSCSyncRun) error {
	return database.DB().WithContext(ctx).Save(run).Error
}

func (r *gscRepository) LatestSyncRun(ctx context.Context, countryCode, kind string) (*models.GSCSyncRun, error) {
	var run models.GSCSyncRun
	err := database.DB().WithContext(ctx).
		Where("country_code = ? AND kind = ?", countryCode, kind).
		Order("started_at DESC").
		First(&run).Error
	if err != nil {
		return nil, err
	}
	return &run, nil
}
