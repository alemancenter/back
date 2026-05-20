package calendar

import (
	"strconv"

	"github.com/alemancenter/fiber-api/internal/database"
	_ "github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
)

// Handler contains calendar route handlers
type Handler struct {
	svc services.CalendarService
}

// New creates a new calendar Handler
func New(svc services.CalendarService) *Handler {
	return &Handler{
		svc: svc,
	}
}

// Databases returns available calendar databases (countries)
// @Summary List Calendar Databases
// @Description Returns available calendar databases/countries
// @Tags Calendar
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Success 200 {object} utils.APIResponse{data=[]services.DatabaseInfo}
// @Router /dashboard/calendar/databases [get]
func (h *Handler) Databases(c *fiber.Ctx) error {
	return utils.Success(c, "success", []services.DatabaseInfo{
		{ID: 1, Code: "jo", Name: "الأردن"},
		{ID: 2, Code: "sa", Name: "السعودية"},
		{ID: 3, Code: "eg", Name: "مصر"},
		{ID: 4, Code: "ps", Name: "فلسطين"},
	})
}

// GetEvents returns calendar events for the dashboard
// @Summary List Events (Dashboard)
// @Description Returns a list of calendar events for dashboard management, optionally filtered by date range
// @Tags Calendar
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param start query string false "Start date (YYYY-MM-DD)"
// @Param end query string false "End date (YYYY-MM-DD)"
// @Success 200 {object} utils.APIResponse{data=[]models.Event}
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/calendar/events [get]
func (h *Handler) GetEvents(c *fiber.Ctx) error {
	countryID, _ := c.Locals("country_id").(database.CountryID)
	start := c.Query("start")
	end := c.Query("end")

	events, err := h.svc.ListEvents(countryID, start, end)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Success(c, "success", events)
}

// CreateEvent creates a calendar event
// @Summary Create Event
// @Description Create a new calendar event
// @Tags Calendar
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param request body services.EventInput true "Event data payload"
// @Success 201 {object} utils.APIResponse{data=models.Event}
// @Failure 400 {object} utils.APIResponse
// @Failure 422 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/calendar/events [post]
func (h *Handler) CreateEvent(c *fiber.Ctx) error {
	var req services.EventInput
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	if errs := utils.Validate(req); errs != nil {
		return utils.ValidationError(c, errs)
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	event, err := h.svc.CreateEvent(countryID, &req)
	if err != nil {
		return utils.InternalError(c, "فشل إنشاء الحدث")
	}

	return utils.Created(c, "تم إنشاء الحدث بنجاح", event)
}

// UpdateEvent updates a calendar event
// @Summary Update Event
// @Description Update an existing calendar event by ID
// @Tags Calendar
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Event ID"
// @Param request body services.EventInput true "Event update payload"
// @Success 200 {object} utils.APIResponse{data=models.Event}
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Router /dashboard/calendar/events/{id} [put]
func (h *Handler) UpdateEvent(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	var req services.EventInput
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	event, err := h.svc.UpdateEvent(countryID, id, &req)
	if err != nil {
		return utils.NotFound(c)
	}

	return utils.Success(c, "تم تحديث الحدث بنجاح", event)
}

// DeleteEvent deletes a calendar event
// @Summary Delete Event
// @Description Delete an existing calendar event by ID
// @Tags Calendar
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Event ID"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Router /dashboard/calendar/events/{id} [delete]
func (h *Handler) DeleteEvent(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)
	if err := h.svc.DeleteEvent(countryID, id); err != nil {
		return utils.NotFound(c)
	}

	return utils.Success(c, "تم حذف الحدث بنجاح", nil)
}

// PublicEvents returns upcoming calendar events
// @Summary List Public Events
// @Description Returns upcoming public calendar events
// @Tags Calendar
// @Produce json
// @Param X-Country-Id header string false "Country ID"
// @Success 200 {object} utils.APIResponse{data=[]models.Event}
// @Failure 500 {object} utils.APIResponse
// @Router /home/calendar [get]
func (h *Handler) PublicEvents(c *fiber.Ctx) error {
	countryID, _ := c.Locals("country_id").(database.CountryID)

	start := c.Query("start")
	end := c.Query("end")
	limit := c.QueryInt("limit", 20)
	if limit <= 0 {
		limit = 20
	}

	events, err := h.svc.ListPublicEvents(countryID, start, end, limit)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Success(c, "success", events)
}

// PublicEventDetail returns a single public event
// @Summary Get Public Event
// @Description Returns details of a specific public calendar event by ID
// @Tags Calendar
// @Produce json
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Event ID"
// @Success 200 {object} utils.APIResponse{data=models.Event}
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Router /home/event/{id} [get]
func (h *Handler) PublicEventDetail(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	event, err := h.svc.GetEvent(countryID, id)
	if err != nil {
		return utils.NotFound(c)
	}

	return utils.Success(c, "success", event)
}
