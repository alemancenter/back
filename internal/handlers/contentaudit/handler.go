package contentaudit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/imanjo/fiber-api/internal/models"
	auditservice "github.com/imanjo/fiber-api/internal/services/contentaudit"
	"github.com/imanjo/fiber-api/internal/utils"
	"github.com/imanjo/fiber-api/pkg/logger"
	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type Handler struct {
	svc *auditservice.Service
}

func New(svc *auditservice.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Start(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var userID *uint
	if user, ok := c.Locals("user").(*models.User); ok && user != nil {
		id := user.ID
		userID = &id
	}

	run, err := h.svc.Start(ctx, models.PolicyAuditTriggerManual, userID)
	if err != nil {
		if errors.Is(err, auditservice.ErrAlreadyRunning) {
			return utils.BadRequest(c, "content audit is already running")
		}
		return utils.InternalError(c, "failed to start content audit")
	}

	return utils.Created(c, "content audit started", run)
}

func (h *Handler) ListRuns(c *fiber.Ctx) error {
	pag := utils.GetPagination(c)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	runs, total, err := h.svc.ListRuns(ctx, pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c, "failed to load content audit runs")
	}

	return utils.Paginated(c, "success", runs, pag.BuildMeta(total))
}

func (h *Handler) ShowRun(c *fiber.Ctx) error {
	runID, err := parseRunID(c)
	if err != nil {
		return utils.BadRequest(c, "invalid audit run id")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	run, err := h.svc.GetRun(ctx, runID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NotFound(c)
		}
		return utils.InternalError(c, "failed to load content audit run")
	}

	return utils.Success(c, "success", run)
}

func (h *Handler) ListFindings(c *fiber.Ctx) error {
	runID, err := parseRunID(c)
	if err != nil {
		return utils.BadRequest(c, "invalid audit run id")
	}

	pag := utils.GetPagination(c)
	risk := c.Query("risk")
	contentType := c.Query("type", c.Query("content_type"))
	search := c.Query("q")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	findings, total, err := h.svc.ListFindings(ctx, runID, risk, contentType, search, pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c, "failed to load content audit findings")
	}

	return utils.Paginated(c, "success", findings, pag.BuildMeta(total))
}

func (h *Handler) ExportCSV(c *fiber.Ctx) error {
	runID, err := parseRunID(c)
	if err != nil {
		return utils.BadRequest(c, "invalid audit run id")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var buf bytes.Buffer
	if err := h.svc.ExportCSV(ctx, runID, &buf); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NotFound(c)
		}
		return utils.InternalError(c, "failed to export content audit report")
	}

	c.Set(fiber.HeaderContentType, "text/csv; charset=utf-8")
	c.Set(fiber.HeaderContentDisposition, fmt.Sprintf(`attachment; filename="policy_audit_report_run_%d.csv"`, runID))
	return c.Send(buf.Bytes())
}

func parseRunID(c *fiber.Ctx) (uint64, error) {
	return strconv.ParseUint(c.Params("id"), 10, 64)
}

func (h *Handler) AnalyzeWithAI(c *fiber.Ctx) error {
	var req auditservice.AIAnalyzeRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "invalid AI analysis payload")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	decision, err := h.svc.AnalyzeWithAI(ctx, req, currentUserID(c))
	if err != nil {
		if errors.Is(err, auditservice.ErrUnsupportedContentType) || err == strconv.ErrSyntax {
			return utils.BadRequest(c, err.Error())
		}
		if errors.Is(err, auditservice.ErrAIAnalysisInProgress) {
			return c.Status(fiber.StatusConflict).JSON(utils.APIResponse{
				Success: false,
				Message: "AI analysis is already running for this content",
			})
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NotFound(c)
		}
		logger.Error("failed to analyze content with AI decision engine",
			zap.String("content_type", req.ContentType),
			zap.String("content_id", req.ContentID),
			zap.String("country_code", req.CountryCode),
			zap.Error(err),
		)
		return utils.InternalError(c, "failed to analyze content with AI decision engine")
	}
	return utils.Created(c, "AI decision created", decision)
}

func (h *Handler) ShowAIDecision(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "invalid AI decision id")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	decision, err := h.svc.GetAIDecision(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NotFound(c)
		}
		return utils.InternalError(c, "failed to load AI decision")
	}
	return utils.Success(c, "success", decision)
}

func (h *Handler) LatestAIDecision(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	decision, err := h.svc.LatestAIDecision(ctx, c.Params("type"), c.Params("content_id"), c.Query("country", c.Query("country_code")))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.Success(c, "لا يوجد تحليل AI محفوظ لهذا المحتوى بعد", fiber.Map{"exists": false, "decision": nil})
		}
		return utils.InternalError(c, "failed to load AI decision")
	}
	return utils.Success(c, "success", fiber.Map{"exists": true, "decision": decision})
}

func (h *Handler) CreateFixPreview(c *fiber.Ctx) error {
	var req auditservice.AIFixRequest
	if err := c.BodyParser(&req); err != nil || req.DecisionID == 0 {
		return utils.BadRequest(c, "invalid fix preview payload")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 360*time.Second)
	defer cancel()
	ctx = auditservice.WithAIModelStrategy(ctx, req.ModelStrategy)
	preview, err := h.svc.CreateGroundedFixPreview(ctx, req.DecisionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NotFound(c)
		}
		if errors.Is(err, auditservice.ErrGroundedSourceInsufficient) || errors.Is(err, auditservice.ErrGroundedValidationFailed) || errors.Is(err, auditservice.ErrGroundedAIUnavailable) {
			return c.Status(fiber.StatusUnprocessableEntity).JSON(utils.APIResponse{Success: false, Message: err.Error()})
		}
		return utils.InternalError(c, "failed to create grounded AI fix preview: "+err.Error())
	}
	return utils.Created(c, "grounded AI fix preview created", preview)
}

func (h *Handler) ShowFixPreview(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "invalid fix preview id")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	preview, err := h.svc.GetFixPreview(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NotFound(c)
		}
		return utils.InternalError(c, "failed to load AI fix preview")
	}
	return utils.Success(c, "success", preview)
}

func (h *Handler) ApplyFix(c *fiber.Ctx) error {
	var req auditservice.ApplyFixRequest
	if err := c.BodyParser(&req); err != nil || req.FixPreviewID == 0 {
		return utils.BadRequest(c, "invalid apply fix payload")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	preview, err := h.svc.ApplyGroundedFix(ctx, req.FixPreviewID, currentUserID(c), req.Note)
	if err != nil {
		if errors.Is(err, auditservice.ErrFixAlreadyClosed) || errors.Is(err, auditservice.ErrUnsupportedContentType) || errors.Is(err, auditservice.ErrUngroundedFixPreview) || errors.Is(err, auditservice.ErrGroundedValidationFailed) || errors.Is(err, auditservice.ErrGroundedSourceChanged) {
			return utils.BadRequest(c, err.Error())
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NotFound(c)
		}
		return utils.InternalError(c, "failed to apply grounded AI fix")
	}
	return utils.Success(c, "grounded AI fix applied", preview)
}

func (h *Handler) RejectFix(c *fiber.Ctx) error {
	var req auditservice.RejectFixRequest
	if err := c.BodyParser(&req); err != nil || req.FixPreviewID == 0 {
		return utils.BadRequest(c, "invalid reject fix payload")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	preview, err := h.svc.RejectFix(ctx, req.FixPreviewID, currentUserID(c), req.Note)
	if err != nil {
		if errors.Is(err, auditservice.ErrFixAlreadyClosed) {
			return utils.BadRequest(c, err.Error())
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NotFound(c)
		}
		return utils.InternalError(c, "failed to reject AI fix")
	}
	return utils.Success(c, "AI fix rejected", preview)
}

func currentUserID(c *fiber.Ctx) *uint {
	if user, ok := c.Locals("user").(*models.User); ok && user != nil {
		id := user.ID
		return &id
	}
	return nil
}
