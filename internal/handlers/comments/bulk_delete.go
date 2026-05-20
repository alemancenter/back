package comments

import (
	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
)

// DashboardBulkDelete deletes multiple comments in the selected database.
// POST /api/dashboard/comments/:database/bulk-delete
func (h *Handler) DashboardBulkDelete(c *fiber.Ctx) error {
	dbCode := c.Params("database")

	var req services.BulkDeleteCommentsRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "invalid request body")
	}

	if errs := utils.Validate(req); errs != nil {
		return utils.ValidationError(c, errs)
	}

	deleted, err := h.svc.DeleteMany(dbCode, req.IDs)
	if err != nil {
		return utils.InternalError(c, "failed to delete selected comments")
	}

	return utils.Success(c, "selected comments deleted successfully", fiber.Map{
		"deleted": deleted,
	})
}
