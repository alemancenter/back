package dashboard

import (
	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
)

// Handler contains dashboard overview route handlers
type Handler struct {
	svc services.DashboardService
}

// New creates a new dashboard Handler
func New(svc services.DashboardService) *Handler {
	return &Handler{
		svc: svc,
	}
}

// Activities returns the activity log
// GET /api/dashboard/activities
func (h *Handler) Activities(c *fiber.Ctx) error {
	pag := utils.GetPagination(c)
	logName := c.Query("log_name")
	causerID := c.Query("causer_id")
	activities, total, err := h.svc.ListActivities(logName, causerID, pag.Offset, pag.PerPage)
	if err != nil {
		return utils.InternalError(c, "فشل جلب السجلات")
	}

	return utils.Paginated(c, "success", activities, pag.BuildMeta(total))
}

// CleanActivities removes old activity logs
// DELETE /api/dashboard/activities/clean
func (h *Handler) CleanActivities(c *fiber.Ctx) error {
	deletedCount, err := h.svc.CleanOldActivities()
	if err != nil {
		return utils.InternalError(c, "فشل تنظيف السجلات")
	}

	return utils.Success(c, "تم تنظيف السجلات القديمة", services.CleanActivitiesResponse{
		Deleted: deletedCount,
	})
}
