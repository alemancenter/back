package contentaudit

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/alemancenter/fiber-api/internal/models"
	auditservice "github.com/alemancenter/fiber-api/internal/services/contentaudit"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
)

// Content quality batch processing is intentionally preview-first:
// it may analyze and create fix previews, but it never applies generated text
// automatically. A human reviewer must approve every fix preview.
func (h *Handler) StartQualityBatch(c *fiber.Ctx) error {
	var req contentQualityBatchRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "invalid quality batch payload")
	}
	req = normalizeQualityBatchRequest(req)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	targets, err := h.selectQualityBatchTargets(ctx, req)
	if err != nil {
		return utils.InternalError(c, "failed to select quality batch targets")
	}
	if len(targets) == 0 {
		return utils.BadRequest(c, "لا توجد عناصر مطابقة للفلاتر المحددة")
	}

	jobID := nextQualityBatchID()
	items := make([]contentQualityBatchItem, 0, len(targets))
	for _, target := range targets {
		items = append(items, contentQualityBatchItem{
			ContentType: target.Type,
			ContentID:   target.ID,
			CountryCode: req.CountryCode,
			Title:       target.Title,
			URL:         target.URL,
			Status:      models.ContentAIJobItemStatusPending,
			ScoreBefore: target.Score,
		})
	}
	job := &contentQualityBatchJob{
		ID:              jobID,
		Status:          models.ContentAIJobStatusQueued,
		Mode:            req.Mode,
		ModelStrategy:   req.ModelStrategy,
		CountryCode:     req.CountryCode,
		ContentType:     req.ContentType,
		Level:           req.Level,
		Query:           req.Query,
		Source:          req.Source,
		Preset:          req.Preset,
		Limit:           req.Limit,
		Concurrency:     req.Concurrency,
		TotalItems:      len(items),
		PendingItems:    len(items),
		CreatedByUserID: currentUserID(c),
		CreatedAt:       time.Now(),
		Items:           items,
	}
	putQualityBatch(job)
	go h.runQualityBatch(job.ID)

	if snapshot, ok := qualityBatchSnapshot(job.ID); ok {
		return utils.Created(c, "quality batch started", snapshot)
	}
	return utils.Created(c, "quality batch started", job)
}

func (h *Handler) ListQualityBatches(c *fiber.Ctx) error {
	return utils.Success(c, "success", allQualityBatchSnapshots())
}

func (h *Handler) ShowQualityBatch(c *fiber.Ctx) error {
	job, ok := qualityBatchSnapshot(c.Params("id"))
	if !ok {
		return utils.NotFound(c)
	}
	return utils.Success(c, "success", job)
}

func (h *Handler) CancelQualityBatch(c *fiber.Ctx) error {
	jobID := c.Params("id")
	if _, ok := qualityBatchSnapshot(jobID); !ok {
		return utils.NotFound(c)
	}
	updateQualityBatch(jobID, func(job *contentQualityBatchJob) {
		if job.Status == models.ContentAIJobStatusCompleted || job.Status == models.ContentAIJobStatusFailed || job.Status == models.ContentAIJobStatusCancelled {
			return
		}
		job.CancelRequested = true
		job.Status = models.ContentAIJobStatusCancelling
	})
	job, _ := qualityBatchSnapshot(jobID)
	return utils.Success(c, "quality batch cancellation requested", job)
}

func (h *Handler) runQualityBatch(jobID string) {
	now := time.Now()
	updateQualityBatch(jobID, func(job *contentQualityBatchJob) {
		job.Status = models.ContentAIJobStatusRunning
		job.StartedAt = &now
	})

	jobSnapshot, ok := qualityBatchSnapshot(jobID)
	if !ok {
		return
	}
	sem := make(chan struct{}, jobSnapshot.Concurrency)
	var wg sync.WaitGroup
	for idx := range jobSnapshot.Items {
		if current, ok := qualityBatchSnapshot(jobID); ok && current.CancelRequested {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(itemIndex int) {
			defer wg.Done()
			defer func() { <-sem }()
			h.processQualityBatchItem(jobID, itemIndex)
		}(idx)
	}
	wg.Wait()

	finished := time.Now()
	updateQualityBatch(jobID, func(job *contentQualityBatchJob) {
		recalcQualityBatchProgress(job)
		job.FinishedAt = &finished
		if job.CancelRequested {
			for i := range job.Items {
				if job.Items[i].Status == models.ContentAIJobItemStatusPending {
					job.Items[i].Status = models.ContentAIJobItemStatusCancelled
					job.Items[i].Error = "تم إلغاء المهمة قبل بدء معالجة هذا العنصر"
				}
			}
			recalcQualityBatchProgress(job)
			job.Status = models.ContentAIJobStatusCancelled
			return
		}
		if job.FailedItems > 0 && job.SuccessfulItems == 0 {
			job.Status = models.ContentAIJobStatusFailed
			job.Error = "فشلت كل عناصر المعالجة"
			return
		}
		job.Status = models.ContentAIJobStatusCompleted
	})
}

func (h *Handler) processQualityBatchItem(jobID string, itemIndex int) {
	var item contentQualityBatchItem
	var mode string
	var modelStrategy string
	var userID *uint
	updateQualityBatch(jobID, func(job *contentQualityBatchJob) {
		if itemIndex < 0 || itemIndex >= len(job.Items) || job.CancelRequested {
			return
		}
		now := time.Now()
		job.Items[itemIndex].Status = models.ContentAIJobItemStatusRunning
		job.Items[itemIndex].StartedAt = &now
		item = job.Items[itemIndex]
		mode = job.Mode
		modelStrategy = job.ModelStrategy
		userID = job.CreatedByUserID
	})
	if item.Status == "" || item.Status == models.ContentAIJobItemStatusCancelled {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	ctx = auditservice.WithAIModelStrategy(ctx, modelStrategy)
	ctx = auditservice.WithAIJobContext(ctx, jobID)
	decision, err := h.svc.AnalyzeWithAI(ctx, auditservice.AIAnalyzeRequest{
		ModelStrategy: modelStrategy,
		ContentType:   item.ContentType,
		ContentID:     strconv.FormatUint(uint64(item.ContentID), 10),
		CountryCode:   item.CountryCode,
		Title:         item.Title,
		URL:           item.URL,
	}, userID)
	if err != nil {
		finishQualityBatchItem(jobID, itemIndex, models.ContentAIJobItemStatusFailed, fmt.Sprintf("فشل التحليل: %v", err), nil, nil, 0)
		return
	}
	decisionID := decision.ID
	previewID := uint(0)
	message := "تم إنشاء تحليل الجودة والسياسات"
	if mode == "fix_preview" || mode == "full_review" {
		preview, previewErr := h.svc.CreateFixPreview(ctx, uint64(decision.ID))
		if previewErr != nil {
			finishQualityBatchItem(jobID, itemIndex, models.ContentAIJobItemStatusFailed, fmt.Sprintf("تم التحليل لكن فشل إنشاء معاينة التحسين: %v", previewErr), &decisionID, nil, decision.Score)
			return
		}
		previewID = preview.ID
		message = "تم إنشاء تحليل ومعاينة تحسين بانتظار المراجعة البشرية"
	}
	finishQualityBatchItem(jobID, itemIndex, models.ContentAIJobItemStatusCompleted, message, &decisionID, optionalUint(previewID), decision.Score)
}

func optionalUint(value uint) *uint {
	if value == 0 {
		return nil
	}
	return &value
}

func finishQualityBatchItem(jobID string, itemIndex int, status, message string, decisionID, previewID *uint, scoreAfter int) {
	finished := time.Now()
	updateQualityBatch(jobID, func(job *contentQualityBatchJob) {
		if itemIndex < 0 || itemIndex >= len(job.Items) {
			return
		}
		job.Items[itemIndex].Status = status
		job.Items[itemIndex].FinishedAt = &finished
		if status == models.ContentAIJobItemStatusFailed {
			job.Items[itemIndex].Error = message
		} else {
			job.Items[itemIndex].Message = message
		}
		if decisionID != nil {
			job.Items[itemIndex].DecisionID = decisionID
		}
		if previewID != nil {
			job.Items[itemIndex].FixPreviewID = previewID
		}
		if scoreAfter > 0 {
			job.Items[itemIndex].ScoreAfter = scoreAfter
		}
		recalcQualityBatchProgress(job)
	})
}
