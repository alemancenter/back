// Package humanreview is the dashboard API for the AI policy-fix approval queue: queueing one
// article/post's AI fix for review (instead of handing it straight to the edit page for a
// manual save), listing pending reviews, and approving (applies it) or rejecting (discards it)
// one. See services.HumanReviewService for the actual logic — this package only parses
// requests and maps errors to HTTP responses.
package humanreview

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/models"
	"github.com/imanjo/fiber-api/internal/services"
	"github.com/imanjo/fiber-api/internal/utils"
)

type Handler struct {
	svc services.HumanReviewService
}

func New(svc services.HumanReviewService) *Handler {
	return &Handler{svc: svc}
}

func countryIDFromLocals(c *fiber.Ctx) database.CountryID {
	countryID, _ := c.Locals("country_id").(database.CountryID)
	if countryID == 0 {
		return database.CountryJordan
	}
	return countryID
}

func callerIDFromLocals(c *fiber.Ctx) uint {
	caller, _ := c.Locals("user").(*models.User)
	if caller == nil {
		return 0
	}
	return caller.ID
}

type fixRequest struct {
	ContentType string `json:"content_type"`
	ID          uint64 `json:"id"`
}

// QueueFix runs the AI policy fix for one article/post (same fix the adsense-policy page's
// "إصلاح بالذكاء الاصطناعي" button already triggers) and queues the result as a pending
// human-review item instead of returning a draft for the admin to apply themselves. The admin
// reviews it — old vs new side by side — and approves or rejects it from the
// /dashboard/human-review page. Called once per selected item by the frontend for a "batch"
// fix; each call is independent so one slow/failing item never blocks the rest.
// @Summary Queue an AI policy fix for human review
// @Tags Human Review
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Router /dashboard/human-review/fix [post]
func (h *Handler) QueueFix(c *fiber.Ctx) error {
	var req fixRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}
	req.ContentType = strings.ToLower(strings.TrimSpace(req.ContentType))
	if req.ContentType != "article" && req.ContentType != "post" {
		return utils.BadRequest(c, "نوع المحتوى غير صحيح")
	}
	if req.ID == 0 {
		return utils.BadRequest(c, "معرف العنصر مطلوب")
	}

	item, err := h.svc.QueueFix(c.Context(), countryIDFromLocals(c), req.ContentType, req.ID)
	if err != nil {
		return utils.BadRequest(c, err.Error())
	}
	return utils.Success(c, "success", item)
}

// List returns every AI policy fix currently awaiting a human decision for this country.
// @Summary List pending human-review items
// @Tags Human Review
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Success 200 {object} utils.APIResponse
// @Router /dashboard/human-review [get]
func (h *Handler) List(c *fiber.Ctx) error {
	items, err := h.svc.List(c.Context(), countryIDFromLocals(c))
	if err != nil {
		return utils.InternalError(c, "تعذر تحميل عناصر المراجعة البشرية")
	}
	return utils.Success(c, "success", items)
}

func reviewIDParam(c *fiber.Ctx) (string, error) {
	id := strings.TrimSpace(c.Params("id"))
	if id == "" {
		return "", fiber.NewError(fiber.StatusBadRequest, "معرف غير صحيح")
	}
	return id, nil
}

// Approve applies one pending review's AI-fixed title/content/meta description to the real
// article/post (through the same update path a manual edit-and-save uses) and removes it from
// the pending queue.
// @Summary Approve a pending human-review item
// @Tags Human Review
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path string true "Review ID"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Router /dashboard/human-review/{id}/approve [post]
func (h *Handler) Approve(c *fiber.Ctx) error {
	id, err := reviewIDParam(c)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}
	item, err := h.svc.Approve(c.Context(), countryIDFromLocals(c), id, callerIDFromLocals(c))
	if err != nil {
		return utils.BadRequest(c, err.Error())
	}
	return utils.Success(c, "success", item)
}

// Reject discards one pending review — the article/post is left exactly as it already was.
// @Summary Reject a pending human-review item
// @Tags Human Review
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path string true "Review ID"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Router /dashboard/human-review/{id}/reject [post]
func (h *Handler) Reject(c *fiber.Ctx) error {
	id, err := reviewIDParam(c)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}
	item, err := h.svc.Reject(c.Context(), countryIDFromLocals(c), id)
	if err != nil {
		return utils.BadRequest(c, err.Error())
	}
	return utils.Success(c, "success", item)
}
