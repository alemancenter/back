package comments

import (
	"strconv"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
)

// Handler contains comments and reactions route handlers
type Handler struct {
	svc services.CommentService
}

// New creates a new comments Handler
func New(svc services.CommentService) *Handler {
	return &Handler{svc: svc}
}

// List returns comments for a given country database
// GET /api/comments/:database
func (h *Handler) List(c *fiber.Ctx) error {
	dbCode := c.Params("database")
	pag := utils.GetPagination(c)

	commentableType := firstNonEmpty(c.Query("commentable_type"), c.Query("type"))
	commentableID := firstNonEmpty(c.Query("commentable_id"), c.Query("id"))

	commentList, total, err := h.svc.ListPublic(dbCode, commentableType, commentableID, pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Paginated(c, "success", commentList, pag.BuildMeta(total))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// Create creates a user-submitted comment in pending review state.
// POST /api/comments/:database
func (h *Handler) Create(c *fiber.Ctx) error {
	dbCode := c.Params("database")

	var req services.CreateCommentRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "Ø¨ÙŠØ§Ù†Ø§Øª ØºÙŠØ± ØµØ­ÙŠØ­Ø©")
	}

	if errs := utils.Validate(req); errs != nil {
		return utils.ValidationError(c, errs)
	}

	user, _ := c.Locals("user").(*models.User)
	if user == nil {
		return utils.Unauthorized(c)
	}

	comment, err := h.svc.Create(dbCode, user.ID, &req)
	if err != nil {
		return utils.InternalError(c, "ÙØ´Ù„ Ø¥Ù†Ø´Ø§Ø¡ Ø§Ù„ØªØ¹Ù„ÙŠÙ‚")
	}

	return utils.Created(c, "ØªÙ… Ø¥Ø±Ø³Ø§Ù„ Ø§Ù„ØªØ¹Ù„ÙŠÙ‚ ÙˆØ³ÙŠØ¸Ù‡Ø± Ø¨Ø¹Ø¯ Ù…Ø±Ø§Ø¬Ø¹ØªÙ‡", comment)
}

// CreateReaction creates an emoji reaction on a comment
// POST /api/reactions
func (h *Handler) CreateReaction(c *fiber.Ctx) error {
	var req services.ReactionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	if errs := utils.Validate(req); errs != nil {
		return utils.ValidationError(c, errs)
	}

	user, _ := c.Locals("user").(*models.User)
	if user == nil {
		return utils.Unauthorized(c)
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	reaction, err := h.svc.CreateReaction(countryID, user.ID, &req)
	if err != nil {
		if err == services.ErrForbidden {
			return utils.Forbidden(c, "comment is not approved")
		}
		return utils.InternalError(c, "فشل إضافة التفاعل")
	}

	return utils.Created(c, "تم إضافة التفاعل بنجاح", reaction)
}

// DeleteReaction removes a reaction from a comment
// DELETE /api/reactions/:comment_id
func (h *Handler) DeleteReaction(c *fiber.Ctx) error {
	commentID, err := strconv.ParseUint(c.Params("comment_id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	user, _ := c.Locals("user").(*models.User)
	if user == nil {
		return utils.Unauthorized(c)
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	if err := h.svc.DeleteReaction(countryID, commentID, user.ID); err != nil {
		return utils.InternalError(c, "فشل حذف التفاعل")
	}

	return utils.Success(c, "تم حذف التفاعل بنجاح", nil)
}

// GetReactions returns reactions for a comment
// GET /api/reactions/:comment_id
func (h *Handler) GetReactions(c *fiber.Ctx) error {
	commentID, err := strconv.ParseUint(c.Params("comment_id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	reactions, err := h.svc.GetReactions(countryID, commentID)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Success(c, "success", reactions)
}

// DashboardList returns comments for dashboard management
// GET /api/dashboard/comments/:database
func (h *Handler) DashboardList(c *fiber.Ctx) error {
	dbCode := c.Params("database")
	pag := utils.GetPagination(c)

	commentableType := firstNonEmpty(c.Query("commentable_type"), c.Query("type"))
	commentableID := firstNonEmpty(c.Query("commentable_id"), c.Query("id"))
	status := c.Query("status")
	search := c.Query("q")

	if status != "" && !models.IsValidCommentStatus(status) {
		return utils.BadRequest(c, "invalid comment status")
	}

	commentList, total, err := h.svc.List(dbCode, commentableType, commentableID, status, search, pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Paginated(c, "success", commentList, pag.BuildMeta(total))
}

// DashboardCreate creates a comment (dashboard)
// POST /api/dashboard/comments/:database
func (h *Handler) DashboardCreate(c *fiber.Ctx) error {
	dbCode := c.Params("database")

	var req services.CreateCommentRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	if errs := utils.Validate(req); errs != nil {
		return utils.ValidationError(c, errs)
	}

	user, _ := c.Locals("user").(*models.User)
	if user == nil {
		return utils.Unauthorized(c)
	}

	comment, err := h.svc.Create(dbCode, user.ID, &req)
	if err != nil {
		return utils.InternalError(c, "فشل إنشاء التعليق")
	}

	return utils.Created(c, "تم إنشاء التعليق بنجاح", comment)
}

// DashboardApprove approves a pending/rejected comment.
// POST /api/dashboard/comments/:database/:id/approve
func (h *Handler) DashboardApprove(c *fiber.Ctx) error {
	return h.updateStatus(c, true)
}

// DashboardReject rejects a pending/approved comment.
// POST /api/dashboard/comments/:database/:id/reject
func (h *Handler) DashboardReject(c *fiber.Ctx) error {
	return h.updateStatus(c, false)
}

func (h *Handler) updateStatus(c *fiber.Ctx, approve bool) error {
	dbCode := c.Params("database")
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "Ù…Ø¹Ø±Ù ØºÙŠØ± ØµØ­ÙŠØ­")
	}

	var comment *models.Comment
	if approve {
		comment, err = h.svc.Approve(dbCode, id)
	} else {
		comment, err = h.svc.Reject(dbCode, id)
	}
	if err != nil {
		if err == services.ErrNotFound {
			return utils.NotFound(c)
		}
		return utils.InternalError(c, "ÙØ´Ù„ ØªØ­Ø¯ÙŠØ« Ø­Ø§Ù„Ø© Ø§Ù„ØªØ¹Ù„ÙŠÙ‚")
	}

	if approve {
		return utils.Success(c, "ØªÙ…Øª Ø§Ù„Ù…ÙˆØ§ÙÙ‚Ø© Ø¹Ù„Ù‰ Ø§Ù„ØªØ¹Ù„ÙŠÙ‚", comment)
	}
	return utils.Success(c, "ØªÙ… Ø±ÙØ¶ Ø§Ù„ØªØ¹Ù„ÙŠÙ‚", comment)
}

// DashboardDelete deletes a comment (dashboard)
// DELETE /api/dashboard/comments/:database/:id
func (h *Handler) DashboardDelete(c *fiber.Ctx) error {
	dbCode := c.Params("database")
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	if err := h.svc.Delete(dbCode, id); err != nil {
		return utils.InternalError(c, "فشل حذف التعليق")
	}

	return utils.Success(c, "تم حذف التعليق بنجاح", nil)
}
