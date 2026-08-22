package contentaudit

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/imanjo/fiber-api/internal/models"
	auditservice "github.com/imanjo/fiber-api/internal/services/contentaudit"
	"github.com/imanjo/fiber-api/internal/utils"
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

	// Create the job immediately (empty), then select targets + process in the
	// background. Target selection can scan a lot of content, so doing it in the
	// request handler made the "start" button block and time out on large sites.
	jobID := nextQualityBatchID()
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
		TotalItems:      0,
		PendingItems:    0,
		CreatedByUserID: currentUserID(c),
		CreatedAt:       time.Now(),
		Items:           nil,
	}
	putQualityBatch(job)
	go h.prepareAndRunQualityBatch(jobID, req)

	if snapshot, ok := qualityBatchSnapshot(jobID); ok {
		return utils.Created(c, "quality batch started", snapshot)
	}
	return utils.Created(c, "quality batch started", job)
}

// prepareAndRunQualityBatch selects the batch targets (bounded by a generous
// timeout) then runs the batch. It runs in a goroutine so StartQualityBatch
// returns instantly.
func (h *Handler) prepareAndRunQualityBatch(jobID string, req contentQualityBatchRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	markFailed := func(message string) {
		finished := time.Now()
		updateQualityBatch(jobID, func(job *contentQualityBatchJob) {
			job.Status = models.ContentAIJobStatusFailed
			job.Error = message
			job.FinishedAt = &finished
		})
	}

	targets, err := h.selectQualityBatchTargets(ctx, req)
	if err != nil {
		markFailed("تعذّر تحديد عناصر الدفعة (قد يكون المحتوى كبيرًا أو انتهت المهلة). جرّب تضييق النطاق أو تقليل العدد.")
		return
	}
	if len(targets) == 0 {
		markFailed("لا توجد عناصر مطابقة للفلاتر المحددة")
		return
	}

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
	updateQualityBatch(jobID, func(job *contentQualityBatchJob) {
		job.Items = items
		job.TotalItems = len(items)
		job.PendingItems = len(items)
	})

	h.runQualityBatch(jobID)
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
		preview, previewErr := h.svc.CreateGroundedFixPreview(ctx, uint64(decision.ID))
		if previewErr != nil {
			finishQualityBatchItem(jobID, itemIndex, models.ContentAIJobItemStatusFailed, fmt.Sprintf("تم التحليل لكن فشل إنشاء معاينة التحسين الموثق: %v", previewErr), &decisionID, nil, decision.Score)
			return
		}
		previewID = preview.ID
		message = "تم إنشاء تحليل ومعاينة تحسين موثقة بالأدلة بانتظار المراجعة البشرية"
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
