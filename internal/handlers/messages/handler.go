package messages

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
)

// Handler contains messages route handlers
type Handler struct {
	svc          services.MessageService
	notification services.NotificationService
}

// New creates a new messages Handler
func New(svc services.MessageService, notificationSvc services.NotificationService) *Handler {
	return &Handler{
		svc:          svc,
		notification: notificationSvc,
	}
}

func getUser(c *fiber.Ctx) *models.User {
	user, _ := c.Locals("user").(*models.User)
	return user
}

// Inbox returns received messages for the current user.
// @Summary Get Inbox Messages
// @Description Returns a paginated list of received messages for the authenticated user
// @Tags Messages
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param page query int false "Page number"
// @Param limit query int false "Items per page"
// @Success 200 {object} utils.APIResponse{data=[]models.Message}
// @Failure 401 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/messages/inbox [get]
func (h *Handler) Inbox(c *fiber.Ctx) error {
	user := getUser(c)
	if user == nil {
		return utils.Unauthorized(c)
	}

	pag := utils.GetPagination(c)
	msgs, total, err := h.svc.ListInbox(user.ID, pag.Offset, pag.PerPage)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Paginated(c, "success", msgs, pag.BuildMeta(total))
}

// Sent returns messages sent by the current user.
// @Summary Get Sent Messages
// @Description Returns a paginated list of messages sent by the authenticated user
// @Tags Messages
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param page query int false "Page number"
// @Param limit query int false "Items per page"
// @Success 200 {object} utils.APIResponse{data=[]models.Message}
// @Failure 401 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/messages/sent [get]
func (h *Handler) Sent(c *fiber.Ctx) error {
	user := getUser(c)
	if user == nil {
		return utils.Unauthorized(c)
	}

	pag := utils.GetPagination(c)
	msgs, total, err := h.svc.ListSent(user.ID, pag.Offset, pag.PerPage)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Paginated(c, "success", msgs, pag.BuildMeta(total))
}

// Drafts returns draft messages for the current user.
// @Summary Get Draft Messages
// @Description Returns a paginated list of draft messages for the authenticated user
// @Tags Messages
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param page query int false "Page number"
// @Param limit query int false "Items per page"
// @Success 200 {object} utils.APIResponse{data=[]models.Message}
// @Failure 401 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/messages/drafts [get]
func (h *Handler) Drafts(c *fiber.Ctx) error {
	user := getUser(c)
	if user == nil {
		return utils.Unauthorized(c)
	}

	pag := utils.GetPagination(c)
	msgs, total, err := h.svc.ListDrafts(user.ID, pag.Offset, pag.PerPage)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Paginated(c, "success", msgs, pag.BuildMeta(total))
}

type SendMessageRequest struct {
	RecipientID uint   `json:"recipient_id" validate:"required"`
	Subject     string `json:"subject"`
	Body        string `json:"body" validate:"required,min=1"`
}

// Send sends a new message.
// @Summary Send Message
// @Description Send a new message to another user
// @Tags Messages
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param request body SendMessageRequest true "Message payload"
// @Success 201 {object} utils.APIResponse{data=models.Message}
// @Failure 400 {object} utils.APIResponse
// @Failure 401 {object} utils.APIResponse
// @Failure 422 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/messages/send [post]
func (h *Handler) Send(c *fiber.Ctx) error {
	user := getUser(c)
	if user == nil {
		return utils.Unauthorized(c)
	}

	var req SendMessageRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}
	if errs := utils.Validate(req); errs != nil {
		return utils.ValidationError(c, errs)
	}

	msg, err := h.svc.SendMessage(user.ID, req.RecipientID, req.Subject, req.Body)
	if err != nil {
		return utils.InternalError(c, "فشل إرسال الرسالة")
	}

	// Notify the recipient
	go func() {
		subject := req.Subject
		if subject == "" {
			subject = "رسالة جديدة"
		}
		notifData, _ := json.Marshal(map[string]string{
			"title":      fmt.Sprintf("رسالة من %s", user.Name),
			"message":    subject,
			"action_url": "/dashboard/messages",
		})
		_, _ = h.notification.Create(
			"App\\Notifications\\NewMessage",
			req.RecipientID,
			string(notifData),
		)
	}()

	return utils.Created(c, "تم إرسال الرسالة بنجاح", msg)
}

type SaveDraftRequest struct {
	RecipientID uint   `json:"recipient_id"`
	Subject     string `json:"subject"`
	Body        string `json:"body" validate:"required"`
}

// Draft saves a draft message.
// @Summary Save Draft Message
// @Description Save a message as a draft for later sending
// @Tags Messages
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param request body SaveDraftRequest true "Draft message payload"
// @Success 201 {object} utils.APIResponse{data=models.Message}
// @Failure 400 {object} utils.APIResponse
// @Failure 401 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/messages/draft [post]
func (h *Handler) Draft(c *fiber.Ctx) error {
	user := getUser(c)
	if user == nil {
		return utils.Unauthorized(c)
	}

	var req SaveDraftRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	msg, err := h.svc.SaveDraft(user.ID, req.RecipientID, req.Subject, req.Body)
	if err != nil {
		return utils.InternalError(c, "فشل حفظ المسودة")
	}

	return utils.Created(c, "تم حفظ المسودة بنجاح", msg)
}

// Get returns a single message.
// @Summary Get Message
// @Description Returns the details of a single message by ID
// @Tags Messages
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param id path int true "Message ID"
// @Success 200 {object} utils.APIResponse{data=models.Message}
// @Failure 400 {object} utils.APIResponse
// @Failure 401 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Router /dashboard/messages/{id} [get]
func (h *Handler) Get(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	user := getUser(c)
	if user == nil {
		return utils.Unauthorized(c)
	}

	msg, err := h.svc.GetMessage(id, user.ID)
	if err != nil {
		return utils.NotFound(c)
	}

	return utils.Success(c, "success", msg)
}

// MarkAsRead marks a message as read.
// @Summary Mark Message as Read
// @Description Mark a specific received message as read
// @Tags Messages
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param id path int true "Message ID"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 401 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/messages/{id}/read [post]
func (h *Handler) MarkAsRead(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	user := getUser(c)
	if user == nil {
		return utils.Unauthorized(c)
	}

	if err := h.svc.MarkAsRead(id, user.ID); err != nil {
		return utils.InternalError(c)
	}

	return utils.Success(c, "تم تعليم الرسالة كمقروءة", nil)
}

// ToggleImportant toggles the important flag on a message.
// @Summary Toggle Message Importance
// @Description Toggle the importance flag (star) on a message
// @Tags Messages
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param id path int true "Message ID"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 401 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Router /dashboard/messages/{id}/important [post]
func (h *Handler) ToggleImportant(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	user := getUser(c)
	if user == nil {
		return utils.Unauthorized(c)
	}

	if err := h.svc.ToggleImportant(id, user.ID); err != nil {
		return utils.NotFound(c)
	}

	return utils.Success(c, "تم تحديث حالة الرسالة", nil)
}

// Delete soft-deletes a message.
// @Summary Delete Message
// @Description Soft-delete a message
// @Tags Messages
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param id path int true "Message ID"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 401 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/messages/{id} [delete]
func (h *Handler) Delete(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	user := getUser(c)
	if user == nil {
		return utils.Unauthorized(c)
	}

	if err := h.svc.DeleteMessage(id, user.ID); err != nil {
		return utils.InternalError(c)
	}

	return utils.Success(c, "تم حذف الرسالة", nil)
}
