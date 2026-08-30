package searchconsole

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/imanjo/fiber-api/internal/models"
	"github.com/imanjo/fiber-api/internal/repositories"
	"github.com/imanjo/fiber-api/pkg/logger"
	"go.uber.org/zap"
)

var ErrAlreadyRunning = errors.New("a search console sync is already running for this country")

// URLInspectionMaxPerRun caps a single bulk sync so one run can never come close
// to the URL Inspection API's daily quota (≈2,000/property/day) — see plan §4.3.
const URLInspectionMaxPerRun = 150

// throttleDelay keeps calls comfortably under the ≈600/min per-property quota.
const throttleDelay = 500 * time.Millisecond

// Target is one URL to inspect, supplied by the caller (e.g. the readiness
// dashboard's selection) rather than discovered by this service — keeps
// prioritization logic (newest, newly-ready, stale round-robin) in the layer
// that already has that context instead of duplicating it here.
type Target struct {
	ContentType string
	ContentID   uint
	CountryCode string
	URL         string
}

type Service struct {
	repo   repositories.GSCRepository
	client *Client

	runningMu sync.Mutex
	running   map[string]bool // key: countryCode + ":" + kind
}

// NewService builds a Service from a client that is already authenticated
// (see NewClient). Returns (nil, ErrNotConfigured)-equivalent behavior via the
// client constructor if serviceAccountJSON is empty — callers should check that
// before wiring this into routes so the feature is cleanly absent rather than
// erroring on every request when GSC isn't configured yet.
func NewService(repo repositories.GSCRepository, client *Client) *Service {
	return &Service{repo: repo, client: client, running: make(map[string]bool)}
}

func (s *Service) tryLock(key string) bool {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	if s.running[key] {
		return false
	}
	s.running[key] = true
	return true
}

func (s *Service) unlock(key string) {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	delete(s.running, key)
}

// InspectAndStore runs one URL through the URL Inspection API and persists the
// mapped status. Synchronous and cheap enough to use directly for the
// single-item "GSC status" badge lookup, and reused by SyncBatch for bulk runs.
func (s *Service) InspectAndStore(ctx context.Context, siteURL string, target Target) (*models.GSCURLStatus, error) {
	result, err := s.client.InspectURL(ctx, siteURL, target.URL)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	status := &models.GSCURLStatus{
		ContentType:     target.ContentType,
		ContentID:       target.ContentID,
		CountryCode:     target.CountryCode,
		URL:             target.URL,
		IndexStatus:     result.IndexStatus,
		CoverageVerdict: result.Verdict,
		GoogleCanonical: result.GoogleCanonical,
		UserCanonical:   result.UserCanonical,
		RobotsTxtState:  result.RobotsTxtState,
		LastCrawlTime:   result.LastCrawlTime,
		RawResponse:     result.Raw,
		CheckedAt:       now,
	}
	if err := s.repo.UpsertURLStatus(ctx, status); err != nil {
		return nil, err
	}
	return status, nil
}

// SyncBatch inspects up to URLInspectionMaxPerRun targets in the background,
// throttled to stay under Google's per-minute quota, and records progress on a
// GSCSyncRun row (mirrors contentaudit.Service's Start/execute pattern instead
// of introducing a new job abstraction).
func (s *Service) SyncBatch(ctx context.Context, countryCode string, targets []Target, triggeredBy string) (*models.GSCSyncRun, error) {
	if s.client == nil {
		return nil, ErrNotConfigured
	}
	lockKey := countryCode + ":" + models.GSCSyncKindURLInspection
	if !s.tryLock(lockKey) {
		return nil, ErrAlreadyRunning
	}

	if triggeredBy == "" {
		triggeredBy = models.GSCSyncTriggerManual
	}
	if len(targets) > URLInspectionMaxPerRun {
		targets = targets[:URLInspectionMaxPerRun]
	}

	run := &models.GSCSyncRun{
		CountryCode: countryCode,
		Kind:        models.GSCSyncKindURLInspection,
		Status:      models.GSCSyncStatusRunning,
		TriggeredBy: triggeredBy,
		StartedAt:   time.Now(),
	}
	if err := s.repo.CreateSyncRun(ctx, run); err != nil {
		s.unlock(lockKey)
		return nil, err
	}

	property, err := s.repo.GetProperty(context.Background(), countryCode)
	if err != nil {
		s.finishSyncRun(run, 0, fmt.Errorf("no GSC property configured for country %q: %w", countryCode, err))
		s.unlock(lockKey)
		return run, nil
	}

	go s.executeBatch(run, property.SiteURL, targets, lockKey)
	return run, nil
}

func (s *Service) executeBatch(run *models.GSCSyncRun, siteURL string, targets []Target, lockKey string) {
	defer s.unlock(lockKey)
	ctx := context.Background()

	checked := 0
	for i, target := range targets {
		if _, err := s.InspectAndStore(ctx, siteURL, target); err != nil {
			logger.Warn("gsc url inspection failed",
				zap.String("country", run.CountryCode), zap.String("url", target.URL), zap.Error(err))
			continue
		}
		checked++
		if i < len(targets)-1 {
			time.Sleep(throttleDelay)
		}
	}

	s.finishSyncRun(run, checked, nil)
}

func (s *Service) finishSyncRun(run *models.GSCSyncRun, checked int, runErr error) {
	now := time.Now()
	run.FinishedAt = &now
	run.URLsChecked = checked
	if runErr != nil {
		run.Status = models.GSCSyncStatusFailed
		message := runErr.Error()
		run.ErrorMessage = &message
	} else {
		run.Status = models.GSCSyncStatusCompleted
	}
	if err := s.repo.UpdateSyncRun(context.Background(), run); err != nil {
		logger.Error("failed to record gsc sync run result", zap.Uint("run_id", run.ID), zap.Error(err))
	}
}

// SyncSearchAnalytics pulls the trailing `days` of performance data for one
// property. Cheap relative to URL Inspection's quota, but still tracked as a
// GSCSyncRun for a consistent operational history.
func (s *Service) SyncSearchAnalytics(ctx context.Context, countryCode string, days int) (*models.GSCSyncRun, error) {
	if s.client == nil {
		return nil, ErrNotConfigured
	}
	if days <= 0 || days > 400 {
		days = 90
	}
	lockKey := countryCode + ":" + models.GSCSyncKindSearchAnalytics
	if !s.tryLock(lockKey) {
		return nil, ErrAlreadyRunning
	}

	run := &models.GSCSyncRun{
		CountryCode: countryCode,
		Kind:        models.GSCSyncKindSearchAnalytics,
		Status:      models.GSCSyncStatusRunning,
		TriggeredBy: models.GSCSyncTriggerManual,
		StartedAt:   time.Now(),
	}
	if err := s.repo.CreateSyncRun(ctx, run); err != nil {
		s.unlock(lockKey)
		return nil, err
	}

	property, err := s.repo.GetProperty(ctx, countryCode)
	if err != nil {
		s.finishSyncRun(run, 0, fmt.Errorf("no GSC property configured for country %q: %w", countryCode, err))
		s.unlock(lockKey)
		return run, nil
	}

	go s.executeAnalyticsSync(run, property.SiteURL, days, lockKey)
	return run, nil
}

func (s *Service) executeAnalyticsSync(run *models.GSCSyncRun, siteURL string, days int, lockKey string) {
	defer s.unlock(lockKey)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// Search Analytics data lags 2-3 days behind real time (see client.go), so
	// the window ends 3 days ago rather than today.
	end := time.Now().AddDate(0, 0, -3)
	start := end.AddDate(0, 0, -(days - 1))

	rows, err := s.client.QuerySearchAnalytics(ctx, siteURL, start, end)
	if err != nil {
		s.finishSyncRun(run, 0, err)
		return
	}

	type aggregate struct {
		URL              string
		Date             time.Time
		Clicks           int
		Impressions      int
		WeightedPosition float64
	}
	aggregates := make(map[string]*aggregate)
	queryRows := make([]models.GSCSearchQueryDaily, 0, len(rows))
	for _, row := range rows {
		queryHash := sha256.Sum256([]byte(row.Query))
		urlHash := sha256.Sum256([]byte(row.URL))
		queryRows = append(queryRows, models.GSCSearchQueryDaily{CountryCode: run.CountryCode, Query: row.Query, QueryHash: hex.EncodeToString(queryHash[:]), URL: row.URL, URLHash: hex.EncodeToString(urlHash[:]), Date: row.Date, Clicks: row.Clicks, Impressions: row.Impressions, CTR: row.CTR, Position: row.Position})
		key := row.URL + "\x00" + row.Date.Format("2006-01-02")
		item := aggregates[key]
		if item == nil {
			item = &aggregate{URL: row.URL, Date: row.Date}
			aggregates[key] = item
		}
		item.Clicks += row.Clicks
		item.Impressions += row.Impressions
		item.WeightedPosition += row.Position * float64(row.Impressions)
	}
	if err := s.repo.UpsertQueryRows(ctx, queryRows); err != nil {
		s.finishSyncRun(run, 0, err)
		return
	}
	dbRows := make([]models.GSCSearchAnalyticsDaily, 0, len(aggregates))
	for _, item := range aggregates {
		ctr, position := 0.0, 0.0
		if item.Impressions > 0 {
			ctr = float64(item.Clicks) / float64(item.Impressions)
			position = item.WeightedPosition / float64(item.Impressions)
		}
		dbRows = append(dbRows, models.GSCSearchAnalyticsDaily{CountryCode: run.CountryCode, URL: item.URL, Date: item.Date, Clicks: item.Clicks, Impressions: item.Impressions, CTR: ctr, Position: position})
	}
	if err := s.repo.UpsertAnalyticsRows(ctx, dbRows); err != nil {
		s.finishSyncRun(run, 0, err)
		return
	}
	s.finishSyncRun(run, len(queryRows), nil)
}

// TestInspect makes one live, synchronous URL Inspection call and returns the
// raw mapped result without persisting anything — for a "test my connection"
// action in the dashboard, not for real syncing (see SyncBatch for that).
func (s *Service) TestInspect(ctx context.Context, countryCode, url string) (*URLInspectionResult, error) {
	if s.client == nil {
		return nil, ErrNotConfigured
	}
	property, err := s.repo.GetProperty(ctx, countryCode)
	if err != nil {
		return nil, fmt.Errorf("no GSC property configured for country %q: %w", countryCode, err)
	}
	return s.client.InspectURL(ctx, property.SiteURL, url)
}

// SubmitSitemapForCountry pings the Sitemaps API for one property — call this
// from the existing sitemap-generation publish hook, not on a schedule.
func (s *Service) SubmitSitemapForCountry(ctx context.Context, countryCode, sitemapURL string) error {
	if s.client == nil {
		return ErrNotConfigured
	}
	property, err := s.repo.GetProperty(ctx, countryCode)
	if err != nil {
		return fmt.Errorf("no GSC property configured for country %q: %w", countryCode, err)
	}
	return s.client.SubmitSitemap(ctx, property.SiteURL, sitemapURL)
}
