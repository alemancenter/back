package contentaudit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/imanjo/fiber-api/internal/models"
	auditservice "github.com/imanjo/fiber-api/internal/services/contentaudit"
	"github.com/imanjo/fiber-api/internal/utils"
	"github.com/imanjo/fiber-api/pkg/logger"
	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const maxBulkFixReviews = 100

type bulkFixReviewRequest struct {
	Action        string   `json:"action"`
	FixPreviewIDs []uint64 `json:"fix_preview_ids"`
	Note          string   `json:"note"`
}

type bulkFixReviewItemResult struct {
	FixPreviewID uint64 `json:"fix_preview_id"`
	Success      bool   `json:"success"`
	Status       string `json:"status,omitempty"`
	Message      string `json:"message"`
}

type bulkFixReviewResult struct {
	Action    string                    `json:"action"`
	Requested int                       `json:"requested"`
	Succeeded int                       `json:"succeeded"`
	Failed    int                       `json:"failed"`
	Results   []bulkFixReviewItemResult `json:"results"`
}

func normalizeBulkFixReviewRequest(req bulkFixReviewRequest) (bulkFixReviewRequest, error) {
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))
	if req.Action != "apply" && req.Action != "reject" {
		return bulkFixReviewRequest{}, errors.New("يجب اختيار قبول المعاينات أو رفضها")
	}
	if len(req.FixPreviewIDs) == 0 {
		return bulkFixReviewRequest{}, errors.New("حدد معاينة واحدة على الأقل")
	}
	if len(req.FixPreviewIDs) > maxBulkFixReviews {
		return bulkFixReviewRequest{}, fmt.Errorf("يمكن اتخاذ قرار بشأن %d معاينة كحد أقصى في العملية الواحدة", maxBulkFixReviews)
	}

	seen := make(map[uint64]struct{}, len(req.FixPreviewIDs))
	ids := make([]uint64, 0, len(req.FixPreviewIDs))
	for _, id := range req.FixPreviewIDs {
		if id == 0 {
			return bulkFixReviewRequest{}, errors.New("تتضمن القائمة معرّف معاينة غير صالح")
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	req.FixPreviewIDs = ids
	req.Note = strings.TrimSpace(req.Note)
	return req, nil
}

func bulkFixReviewTargetKey(preview *models.ContentAIFixPreview) string {
	if preview == nil {
		return ""
	}
	return strings.Join([]string{
		strings.ToLower(strings.TrimSpace(preview.CountryCode)),
		strings.ToLower(strings.TrimSpace(preview.ContentType)),
		strings.TrimSpace(preview.ContentID),
	}, ":")
}

func bulkFixReviewFailureMessage(err error) string {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return "المعاينة غير موجودة"
	case errors.Is(err, auditservice.ErrFixAlreadyClosed):
		return "المعاينة لم تعد بانتظار المراجعة"
	case errors.Is(err, auditservice.ErrUnsupportedContentType):
		return "نوع المحتوى غير مدعوم"
	case errors.Is(err, auditservice.ErrUngroundedFixPreview), errors.Is(err, auditservice.ErrGroundedValidationFailed):
		return err.Error()
	default:
		return "تعذّر تنفيذ القرار على هذه المعاينة"
	}
}

// BulkReviewFixes applies one explicit human decision to the selected preview IDs.
// Each preview is processed independently: a closed or invalid item cannot hide successful
// decisions for the rest of the selection, and the response reports every outcome.
func (h *Handler) BulkReviewFixes(c *fiber.Ctx) error {
	var req bulkFixReviewRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات القرار الجماعي غير صالحة")
	}
	var err error
	req, err = normalizeBulkFixReviewRequest(req)
	if err != nil {
		return utils.BadRequest(c, err.Error())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result := bulkFixReviewResult{
		Action:    req.Action,
		Requested: len(req.FixPreviewIDs),
		Results:   make([]bulkFixReviewItemResult, 0, len(req.FixPreviewIDs)),
	}

	// Applying two drafts for the same content in one click is ambiguous: the second draft
	// could overwrite the first. Rejecting multiple drafts is safe, so this guard is apply-only.
	duplicateTargets := make(map[string]int)
	previews := make(map[uint64]*models.ContentAIFixPreview, len(req.FixPreviewIDs))
	if req.Action == "apply" {
		for _, id := range req.FixPreviewIDs {
			preview, loadErr := h.svc.GetFixPreview(ctx, id)
			if loadErr != nil {
				continue
			}
			previews[id] = preview
			duplicateTargets[bulkFixReviewTargetKey(preview)]++
		}
	}

	userID := currentUserID(c)
	for _, id := range req.FixPreviewIDs {
		item := bulkFixReviewItemResult{FixPreviewID: id}
		if preview := previews[id]; req.Action == "apply" && preview != nil && duplicateTargets[bulkFixReviewTargetKey(preview)] > 1 {
			item.Message = "تم تحديد أكثر من معاينة للمحتوى نفسه؛ راجع واختر معاينة واحدة فقط"
			result.Failed++
			result.Results = append(result.Results, item)
			continue
		}

		var preview *models.ContentAIFixPreview
		if req.Action == "apply" {
			preview, err = h.svc.ApplyGroundedFix(ctx, id, userID, req.Note)
		} else {
			preview, err = h.svc.RejectFix(ctx, id, userID, req.Note)
		}
		if err != nil {
			item.Message = bulkFixReviewFailureMessage(err)
			result.Failed++
			result.Results = append(result.Results, item)
			if item.Message == "تعذّر تنفيذ القرار على هذه المعاينة" {
				logger.Error("failed to process bulk AI fix review item",
					zap.Uint64("fix_preview_id", id),
					zap.String("action", req.Action),
					zap.Error(err),
				)
			}
			continue
		}

		item.Success = true
		item.Status = preview.Status
		if req.Action == "apply" {
			item.Message = "تم قبول المعاينة وتطبيق الإصلاح"
		} else {
			item.Message = "تم رفض المعاينة دون تغيير المحتوى"
		}
		result.Succeeded++
		result.Results = append(result.Results, item)
	}

	message := "اكتملت مراجعة المعاينات المحددة"
	if result.Failed > 0 {
		message = "اكتملت المراجعة مع تعذّر تنفيذ بعض القرارات"
	}
	return utils.Success(c, message, result)
}
