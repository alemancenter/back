package contact_messages

import (
	"strconv"

	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
)

type Handler struct {
	svc services.ContactMessageService
}

func New(svc services.ContactMessageService) *Handler {
	return &Handler{svc: svc}
}

func getUser(c *fiber.Ctx) *models.User {
	user, _ := c.Locals("user").(*models.User)
	return user
}

func (h *Handler) List(c *fiber.Ctx) error {
	if getUser(c) == nil {
		return utils.Unauthorized(c)
	}
	pag := utils.GetPagination(c)
	msgs, total, err := h.svc.List(pag.Offset, pag.PerPage)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Paginated(c, "success", msgs, pag.BuildMeta(total))
}

func (h *Handler) Get(c *fiber.Ctx) error {
	if getUser(c) == nil {
		return utils.Unauthorized(c)
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}
	msg, err := h.svc.Get(uint(id))
	if err != nil {
		return utils.NotFound(c)
	}
	// Auto-mark as read on open
	if !msg.Read {
		_ = h.svc.MarkAsRead(msg.ID)
		msg.Read = true
	}
	return utils.Success(c, "success", msg)
}

func (h *Handler) MarkAsRead(c *fiber.Ctx) error {
	if getUser(c) == nil {
		return utils.Unauthorized(c)
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}
	if err := h.svc.MarkAsRead(uint(id)); err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "تم تعليم الرسالة كمقروءة", nil)
}

func (h *Handler) Delete(c *fiber.Ctx) error {
	if getUser(c) == nil {
		return utils.Unauthorized(c)
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}
	if err := h.svc.Delete(uint(id)); err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "تم حذف الرسالة", nil)
}
