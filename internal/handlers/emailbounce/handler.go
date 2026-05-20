package emailbounce

import (
	"strings"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
)

var validBounceStatuses = map[string]bool{
	"active":        true,
	"hard_bounce":   true,
	"soft_bounce":   true,
	"invalid_email": true,
	"unsubscribed":  true,
}

// Handler exposes bounce management endpoints for the admin dashboard.
type Handler struct {
	reader *services.BounceIMAPReader
}

func New(reader *services.BounceIMAPReader) *Handler {
	return &Handler{reader: reader}
}

// GET /api/dashboard/settings/email-bounce/events
// Query: page, per_page, email (partial match), bounce_type
func (h *Handler) ListEvents(c *fiber.Ctx) error {
	pg := utils.GetPagination(c)
	emailFilter := strings.TrimSpace(c.Query("email"))
	typeFilter := strings.TrimSpace(c.Query("bounce_type"))

	db := database.GetManager().Get(database.CountryJordan)
	query := db.Model(&models.EmailBounceEvent{})

	if emailFilter != "" {
		query = query.Where("email LIKE ?", "%"+emailFilter+"%")
	}
	if typeFilter != "" {
		query = query.Where("bounce_type = ?", typeFilter)
	}

	var total int64
	query.Count(&total)

	var events []models.EmailBounceEvent
	query.Order("created_at DESC").
		Offset(pg.Offset).
		Limit(pg.PerPage).
		Find(&events)

	if events == nil {
		events = []models.EmailBounceEvent{}
	}

	return utils.Paginated(c, "success", events, pg.BuildMeta(total))
}

// GET /api/dashboard/settings/email-bounce/stats
func (h *Handler) Stats(c *fiber.Ctx) error {
	db := database.GetManager().Get(database.CountryJordan)

	type statusRow struct {
		Status string `json:"status"`
		Count  int64  `json:"count"`
	}
	var statuses []statusRow
	db.Model(&models.User{}).
		Select("email_bounce_status as status, count(*) as count").
		Group("email_bounce_status").
		Scan(&statuses)

	if statuses == nil {
		statuses = []statusRow{}
	}

	var totalEvents int64
	db.Model(&models.EmailBounceEvent{}).Count(&totalEvents)

	type typeRow struct {
		BounceType string `json:"bounce_type"`
		Count      int64  `json:"count"`
	}
	var byType []typeRow
	db.Model(&models.EmailBounceEvent{}).
		Select("bounce_type, count(*) as count").
		Group("bounce_type").
		Scan(&byType)

	if byType == nil {
		byType = []typeRow{}
	}

	return utils.Success(c, "success", fiber.Map{
		"user_statuses": statuses,
		"events_by_type": byType,
		"total_events":  totalEvents,
	})
}

// POST /api/dashboard/settings/email-bounce/mark
// Body: { "emails": ["a@b.com"], "status": "hard_bounce" }
func (h *Handler) MarkStatus(c *fiber.Ctx) error {
	var req struct {
		Emails []string `json:"emails"`
		Status string   `json:"status"`
	}
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "invalid request body")
	}
	if len(req.Emails) == 0 {
		return utils.BadRequest(c, "emails list is required")
	}
	if !validBounceStatuses[req.Status] {
		return utils.BadRequest(c, "invalid status value")
	}

	for _, countryID := range allShards() {
		db := database.GetManager().Get(countryID)
		db.Model(&models.User{}).
			Where("email IN ?", req.Emails).
			Update("email_bounce_status", req.Status)
	}

	return utils.Success(c, "status updated", fiber.Map{"updated": len(req.Emails)})
}

// POST /api/dashboard/settings/email-bounce/reset
// Body: { "emails": ["a@b.com"] }
func (h *Handler) ResetStatus(c *fiber.Ctx) error {
	var req struct {
		Emails []string `json:"emails"`
	}
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "invalid request body")
	}
	if len(req.Emails) == 0 {
		return utils.BadRequest(c, "emails list is required")
	}

	for _, countryID := range allShards() {
		db := database.GetManager().Get(countryID)
		db.Model(&models.User{}).
			Where("email IN ?", req.Emails).
			Updates(map[string]interface{}{
				"email_bounce_status":   "active",
				"email_bounce_count":    0,
				"email_bounce_reason":   nil,
				"email_last_bounce_at":  nil,
			})
	}

	return utils.Success(c, "status reset to active", fiber.Map{"updated": len(req.Emails)})
}

// POST /api/dashboard/settings/email-bounce/process-now
// Sends a trigger to the already-running scheduler goroutine; does not spawn a new one.
func (h *Handler) ProcessNow(c *fiber.Ctx) error {
	triggered := h.reader.TriggerNow()
	if triggered {
		return utils.Success(c, "bounce processing triggered", nil)
	}
	return utils.Success(c, "processing already queued", nil)
}

func allShards() []database.CountryID {
	return []database.CountryID{
		database.CountryJordan,
		database.CountrySaudi,
		database.CountryEgypt,
		database.CountryPalestine,
	}
}
