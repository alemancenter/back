package contentaudit

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/pkg/logger"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	qualityBatchSequence uint64
	qualityBatchDBMu     sync.Mutex
	qualityBatchOnce     sync.Once
	qualityBatchInitErr  error
)

func ensureQualityBatchTables() error {
	qualityBatchOnce.Do(func() {
		qualityBatchInitErr = database.DB().AutoMigrate(
			&models.ContentAIJob{},
			&models.ContentAIJobItem{},
			&models.ContentAIModelRun{},
		)
		if qualityBatchInitErr != nil {
			logger.Error("failed to migrate persistent content AI job tables", zap.Error(qualityBatchInitErr))
			return
		}
		markInterruptedQualityBatches()
	})
	return qualityBatchInitErr
}

func markInterruptedQualityBatches() {
	now := time.Now()
	result := database.DB().Model(&models.ContentAIJob{}).
		Where("status IN ?", []string{
			models.ContentAIJobStatusQueued,
			models.ContentAIJobStatusRunning,
			models.ContentAIJobStatusCancelling,
		}).
		Updates(map[string]any{
			"status":      models.ContentAIJobStatusFailed,
			"error":       "توقفت المهمة بسبب إعادة تشغيل الخادم قبل اكتمالها. يرجى إنشاء مهمة جديدة للعناصر غير المكتملة.",
			"finished_at": &now,
		})
	if result.Error != nil {
		logger.Error("failed to mark interrupted content AI jobs", zap.Error(result.Error))
	}
}

func nextQualityBatchID() string {
	seq := atomic.AddUint64(&qualityBatchSequence, 1)
	return fmt.Sprintf("cqj_%d_%d", time.Now().UnixNano(), seq)
}

func putQualityBatch(job *contentQualityBatchJob) {
	if err := ensureQualityBatchTables(); err != nil {
		logger.Error("content AI job tables unavailable", zap.Error(err))
		return
	}
	qualityBatchDBMu.Lock()
	defer qualityBatchDBMu.Unlock()
	modelJob := contentQualityBatchToModel(job, true)
	err := database.DB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("Items").Create(&modelJob).Error; err != nil {
			return err
		}
		if len(modelJob.Items) > 0 {
			return tx.CreateInBatches(modelJob.Items, 250).Error
		}
		return nil
	})
	if err != nil {
		logger.Error("failed to persist content AI job", zap.String("job_id", job.ID), zap.Error(err))
	}
}

func updateQualityBatch(jobID string, fn func(*contentQualityBatchJob)) {
	if err := ensureQualityBatchTables(); err != nil {
		logger.Error("content AI job tables unavailable", zap.Error(err))
		return
	}
	qualityBatchDBMu.Lock()
	defer qualityBatchDBMu.Unlock()

	db := database.DB()
	var modelJob models.ContentAIJob
	err := db.Preload("Items", func(tx *gorm.DB) *gorm.DB {
		return tx.Order("item_index ASC")
	}).First(&modelJob, "id = ?", jobID).Error
	if err != nil {
		if err != gorm.ErrRecordNotFound {
			logger.Error("failed to load content AI job for update", zap.String("job_id", jobID), zap.Error(err))
		}
		return
	}
	job := modelToContentQualityBatch(&modelJob, true)
	fn(job)
	persisted := contentQualityBatchToModel(job, true)
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("Items").Save(&persisted).Error; err != nil {
			return err
		}
		for i := range persisted.Items {
			item := persisted.Items[i]
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "job_id"}, {Name: "item_index"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"content_type", "content_id", "country_code", "title", "url", "status",
					"score_before", "score_after", "decision_id", "fix_preview_id", "message", "error",
					"started_at", "finished_at", "updated_at",
				}),
			}).Create(&item).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		logger.Error("failed to persist content AI job update", zap.String("job_id", jobID), zap.Error(err))
	}
}

func qualityBatchSnapshot(jobID string) (*contentQualityBatchJob, bool) {
	if err := ensureQualityBatchTables(); err != nil {
		logger.Error("content AI job tables unavailable", zap.Error(err))
		return nil, false
	}
	var job models.ContentAIJob
	err := database.DB().Preload("Items", func(tx *gorm.DB) *gorm.DB {
		return tx.Order("item_index ASC")
	}).First(&job, "id = ?", jobID).Error
	if err != nil {
		return nil, false
	}
	return modelToContentQualityBatch(&job, true), true
}

func allQualityBatchSnapshots() []contentQualityBatchJob {
	if err := ensureQualityBatchTables(); err != nil {
		logger.Error("content AI job tables unavailable", zap.Error(err))
		return []contentQualityBatchJob{}
	}
	var jobs []models.ContentAIJob
	if err := database.DB().Order("created_at DESC").Limit(100).Find(&jobs).Error; err != nil {
		logger.Error("failed to list content AI jobs", zap.Error(err))
		return []contentQualityBatchJob{}
	}
	items := make([]contentQualityBatchJob, 0, len(jobs))
	for i := range jobs {
		items = append(items, *modelToContentQualityBatch(&jobs[i], false))
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items
}

func recalcQualityBatchProgress(job *contentQualityBatchJob) {
	processed := 0
	successful := 0
	failed := 0
	for _, item := range job.Items {
		switch item.Status {
		case models.ContentAIJobItemStatusCompleted:
			processed++
			successful++
		case models.ContentAIJobItemStatusFailed, models.ContentAIJobItemStatusCancelled:
			processed++
			failed++
		}
	}
	job.ProcessedItems = processed
	job.SuccessfulItems = successful
	job.FailedItems = failed
	job.PendingItems = job.TotalItems - processed
	if job.PendingItems < 0 {
		job.PendingItems = 0
	}
	if job.TotalItems > 0 {
		job.Progress = int(float64(processed) / float64(job.TotalItems) * 100)
	}
}

func contentQualityBatchToModel(job *contentQualityBatchJob, includeItems bool) models.ContentAIJob {
	modelJob := models.ContentAIJob{
		ID:              job.ID,
		Status:          job.Status,
		Mode:            job.Mode,
		ModelStrategy:   job.ModelStrategy,
		CountryCode:     job.CountryCode,
		ContentType:     job.ContentType,
		Level:           job.Level,
		Query:           job.Query,
		Source:          job.Source,
		Preset:          job.Preset,
		Limit:           job.Limit,
		Concurrency:     job.Concurrency,
		TotalItems:      job.TotalItems,
		ProcessedItems:  job.ProcessedItems,
		SuccessfulItems: job.SuccessfulItems,
		FailedItems:     job.FailedItems,
		PendingItems:    job.PendingItems,
		Progress:        job.Progress,
		CancelRequested: job.CancelRequested,
		Error:           job.Error,
		CreatedByUserID: job.CreatedByUserID,
		CreatedAt:       job.CreatedAt,
		StartedAt:       job.StartedAt,
		FinishedAt:      job.FinishedAt,
	}
	if includeItems {
		modelJob.Items = make([]models.ContentAIJobItem, 0, len(job.Items))
		for idx, item := range job.Items {
			modelJob.Items = append(modelJob.Items, models.ContentAIJobItem{
				JobID:        job.ID,
				ItemIndex:    idx,
				ContentType:  item.ContentType,
				ContentID:    item.ContentID,
				CountryCode:  item.CountryCode,
				Title:        item.Title,
				URL:          item.URL,
				Status:       item.Status,
				ScoreBefore:  item.ScoreBefore,
				ScoreAfter:   item.ScoreAfter,
				DecisionID:   item.DecisionID,
				FixPreviewID: item.FixPreviewID,
				Message:      item.Message,
				Error:        item.Error,
				StartedAt:    item.StartedAt,
				FinishedAt:   item.FinishedAt,
			})
		}
	}
	return modelJob
}

func modelToContentQualityBatch(job *models.ContentAIJob, includeItems bool) *contentQualityBatchJob {
	batch := &contentQualityBatchJob{
		ID:              job.ID,
		Status:          job.Status,
		Mode:            job.Mode,
		ModelStrategy:   job.ModelStrategy,
		CountryCode:     job.CountryCode,
		ContentType:     job.ContentType,
		Level:           job.Level,
		Query:           job.Query,
		Source:          job.Source,
		Preset:          job.Preset,
		Limit:           job.Limit,
		Concurrency:     job.Concurrency,
		TotalItems:      job.TotalItems,
		ProcessedItems:  job.ProcessedItems,
		SuccessfulItems: job.SuccessfulItems,
		FailedItems:     job.FailedItems,
		PendingItems:    job.PendingItems,
		Progress:        job.Progress,
		CancelRequested: job.CancelRequested,
		Error:           job.Error,
		CreatedByUserID: job.CreatedByUserID,
		CreatedAt:       job.CreatedAt,
		StartedAt:       job.StartedAt,
		FinishedAt:      job.FinishedAt,
	}
	if includeItems {
		batch.Items = make([]contentQualityBatchItem, 0, len(job.Items))
		for _, item := range job.Items {
			batch.Items = append(batch.Items, contentQualityBatchItem{
				ContentType:  item.ContentType,
				ContentID:    item.ContentID,
				CountryCode:  item.CountryCode,
				Title:        item.Title,
				URL:          item.URL,
				Status:       item.Status,
				ScoreBefore:  item.ScoreBefore,
				ScoreAfter:   item.ScoreAfter,
				DecisionID:   item.DecisionID,
				FixPreviewID: item.FixPreviewID,
				Message:      item.Message,
				Error:        item.Error,
				StartedAt:    item.StartedAt,
				FinishedAt:   item.FinishedAt,
			})
		}
	}
	return batch
}

func createQualityBatchRunContext(parent context.Context) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	return parent
}
