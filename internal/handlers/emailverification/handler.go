package emailverification

import (
	"strconv"
	"strings"

	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/alemancenter/fiber-api/pkg/logger"
	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

type Handler struct {
	svc services.EmailVerificationReminderService
}

func New(svc services.EmailVerificationReminderService) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) List(c *fiber.Ctx) error {
	req := services.EmailReminderListRequest{
		Search:      strings.TrimSpace(c.Query("search")),
		EmailStatus: strings.TrimSpace(c.Query("email_status")),
		Only:        strings.TrimSpace(c.Query("only")),
		Page:        parseInt(c.Query("page"), 1),
		PerPage:     parseInt(c.Query("per_page"), 25),
	}
	res, err := h.svc.List(req)
	if err != nil {
		return utils.InternalError(c, "failed to list unverified emails")
	}
	return utils.Success(c, "success", res)
}

func (h *Handler) Stats(c *fiber.Ctx) error {
	stats, err := h.svc.Stats()
	if err != nil {
		return utils.InternalError(c, "failed to load email verification stats")
	}
	return utils.Success(c, "success", stats)
}

func (h *Handler) SendReminders(c *fiber.Ctx) error {
	var req services.EmailReminderSendRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "invalid request")
	}
	res, err := h.svc.SendReminders(req)
	if err != nil {
		return utils.InternalError(c, "failed to send reminders")
	}
	return utils.Success(c, "verification reminders processed", res)
}

type idsRequest struct {
	IDs    []uint `json:"ids"`
	Reason string `json:"reason"`
}

func (h *Handler) MarkInvalid(c *fiber.Ctx) error {
	var req idsRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "invalid request")
	}
	updated, err := h.svc.MarkInvalid(req.IDs, req.Reason)
	if err != nil {
		logger.Error("failed to mark email verification records invalid",
			zap.Uints("ids", req.IDs),
			zap.Error(err),
		)
		return utils.InternalError(c, "failed to mark emails invalid")
	}
	return utils.Success(c, "emails marked invalid", fiber.Map{"updated": updated})
}

func (h *Handler) ClearStatus(c *fiber.Ctx) error {
	var req idsRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "invalid request")
	}
	updated, err := h.svc.ClearStatus(req.IDs)
	if err != nil {
		return utils.InternalError(c, "failed to clear email status")
	}
	return utils.Success(c, "email status cleared", fiber.Map{"updated": updated})
}

func (h *Handler) DeleteUsers(c *fiber.Ctx) error {
	var req idsRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "invalid request")
	}
	var callerID uint
	if user, ok := c.Locals("user").(*models.User); ok && user != nil {
		callerID = user.ID
	}
	deleted, err := h.svc.DeleteUsers(req.IDs, callerID)
	if err != nil {
		return utils.InternalError(c, "failed to delete users")
	}
	return utils.Success(c, "unverified users deleted", fiber.Map{"deleted": deleted})
}

type deleteFilteredRequest struct {
	Search      string `json:"search"`
	EmailStatus string `json:"email_status"`
	Only        string `json:"only"`
	Confirm     string `json:"confirm"`
}

func (h *Handler) DeleteFiltered(c *fiber.Ctx) error {
	var req deleteFilteredRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "invalid request")
	}
	if req.Confirm != "DELETE_UNVERIFIED" {
		return utils.BadRequest(c, "confirmation is required")
	}

	var callerID uint
	if user, ok := c.Locals("user").(*models.User); ok && user != nil {
		callerID = user.ID
	}

	deleted, err := h.svc.DeleteFiltered(services.EmailReminderListRequest{
		Search:      strings.TrimSpace(req.Search),
		EmailStatus: strings.TrimSpace(req.EmailStatus),
		Only:        strings.TrimSpace(req.Only),
	}, callerID)
	if err != nil {
		logger.Error("failed to delete filtered unverified users",
			zap.String("search", req.Search),
			zap.String("email_status", req.EmailStatus),
			zap.String("only", req.Only),
			zap.Error(err),
		)
		return utils.InternalError(c, "failed to delete filtered users")
	}

	return utils.Success(c, "filtered unverified users deleted", fiber.Map{"deleted": deleted})
}

func parseInt(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
