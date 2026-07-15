package chatbot

import (
	"strconv"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/middleware"
	"github.com/alemancenter/fiber-api/internal/models"
	chatbotSvc "github.com/alemancenter/fiber-api/internal/services/chatbot"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
)

type Handler struct{ service chatbotSvc.Service }

func New(service chatbotSvc.Service) *Handler { return &Handler{service: service} }

func countryID(c *fiber.Ctx) database.CountryID {
	if v, ok := c.Locals("country_id").(database.CountryID); ok {
		return v
	}
	return database.CountryIDFromHeader(c.Get("X-Country-Code", c.Query("country", "jo")))
}

func (h *Handler) Message(c *fiber.Ctx) error {
	var req chatbotSvc.MessageRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات الرسالة غير صالحة")
	}
	var userID *uint
	if user := middleware.GetUser(c); user != nil {
		uid := user.ID
		userID = &uid
	}
	resp, err := h.service.Reply(countryID(c), userID, utils.GetClientIP(c), c.Get("User-Agent"), req)
	if err != nil {
		return utils.InternalError(c, "تعذر معالجة رسالة المساعد الآن")
	}
	return utils.Success(c, "تم إنشاء الرد بنجاح", resp)
}

func (h *Handler) Suggestions(c *fiber.Ctx) error {
	return utils.Success(c, "اقتراحات المساعد", h.service.Suggestions())
}

func (h *Handler) Feedback(c *fiber.Ctx) error {
	var req struct {
		MessageID uint   `json:"message_id"`
		Rating    string `json:"rating"`
		Comment   string `json:"comment"`
	}
	if err := c.BodyParser(&req); err != nil || req.MessageID == 0 || req.Rating == "" {
		return utils.BadRequest(c, "بيانات التقييم غير صالحة")
	}
	if err := h.service.Feedback(countryID(c), req.MessageID, req.Rating, req.Comment); err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "تم حفظ التقييم", nil)
}

func (h *Handler) DashboardSessions(c *fiber.Ctx) error {
	pag := utils.GetPagination(c)
	sessions, total, err := h.service.ListSessionsPaginated(countryID(c), pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Paginated(c, "محادثات المساعد", sessions, pag.BuildMeta(total))
}

// BulkDeleteSessionsRequest is the payload for deleting multiple chat sessions.
type BulkDeleteSessionsRequest struct {
	IDs []uint `json:"ids" validate:"required,min=1,max=200,dive,required"`
}

func (h *Handler) DashboardBulkDeleteSessions(c *fiber.Ctx) error {
	var req BulkDeleteSessionsRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}
	if errs := utils.Validate(req); errs != nil {
		return utils.ValidationError(c, errs)
	}
	deleted, err := h.service.DeleteSessions(countryID(c), req.IDs)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "تم حذف المحادثات", fiber.Map{"deleted": deleted})
}

func (h *Handler) DashboardSession(c *fiber.Ctx) error {
	id64, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil || id64 == 0 {
		return utils.BadRequest(c, "معرف الجلسة غير صالح")
	}
	session, err := h.service.GetSession(countryID(c), uint(id64))
	if err != nil {
		return utils.InternalError(c, "الجلسة غير موجودة")
	}
	return utils.Success(c, "تفاصيل المحادثة", session)
}

func (h *Handler) DashboardKnowledge(c *fiber.Ctx) error {
	limit, _ := strconv.Atoi(c.Query("limit", "100"))
	items, err := h.service.ListKnowledge(countryID(c), c.Query("country", ""), limit)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "قاعدة معرفة المساعد", items)
}

func (h *Handler) StoreKnowledge(c *fiber.Ctx) error {
	var item models.ChatKnowledgeBase
	if err := c.BodyParser(&item); err != nil {
		return utils.BadRequest(c, "بيانات قاعدة المعرفة غير صالحة")
	}
	if item.Title == "" || item.Question == "" || item.Answer == "" {
		return utils.BadRequest(c, "العنوان والسؤال والجواب حقول مطلوبة")
	}
	if item.CountryCode == "" {
		item.CountryCode = database.CountryCode(countryID(c))
	}
	item.IsActive = true
	if user := middleware.GetUser(c); user != nil {
		uid := user.ID
		item.CreatedBy = &uid
		item.UpdatedBy = &uid
	}
	if err := h.service.CreateKnowledge(countryID(c), &item); err != nil {
		return utils.InternalError(c)
	}
	return utils.Created(c, "تمت إضافة معرفة جديدة للمساعد", item)
}

func (h *Handler) UpdateKnowledge(c *fiber.Ctx) error {
	id64, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil || id64 == 0 {
		return utils.BadRequest(c, "معرف غير صالح")
	}
	var item models.ChatKnowledgeBase
	if err := c.BodyParser(&item); err != nil {
		return utils.BadRequest(c, "بيانات قاعدة المعرفة غير صالحة")
	}
	item.ID = uint(id64)
	if item.CountryCode == "" {
		item.CountryCode = database.CountryCode(countryID(c))
	}
	if user := middleware.GetUser(c); user != nil {
		uid := user.ID
		item.UpdatedBy = &uid
	}
	if err := h.service.UpdateKnowledge(countryID(c), &item); err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "تم تحديث معرفة المساعد", item)
}

func (h *Handler) DeleteKnowledge(c *fiber.Ctx) error {
	id64, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil || id64 == 0 {
		return utils.BadRequest(c, "معرف غير صالح")
	}
	if err := h.service.DeleteKnowledge(countryID(c), uint(id64)); err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "تم حذف عنصر المعرفة", nil)
}
