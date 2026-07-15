package teacher_subscription

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/middleware"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
)

type Handler struct {
	svc services.TeacherSubscriptionService
}

func New(svc services.TeacherSubscriptionService) *Handler {
	return &Handler{svc: svc}
}

func queryUint(c *fiber.Ctx, key string) uint {
	value := c.Query(key)
	if value == "" {
		return 0
	}
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0
	}
	return uint(id)
}

func currentUser(c *fiber.Ctx) *models.User {
	user, _ := c.Locals("user").(*models.User)
	return user
}

func countryIDFromRequest(c *fiber.Ctx) database.CountryID {
	countryID := database.CountryIDFromHeader(c.Get("X-Country-Code"))
	if c.Query("country") != "" {
		countryID = database.CountryIDFromHeader(c.Query("country"))
	}
	if c.FormValue("country") != "" {
		countryID = database.CountryIDFromHeader(c.FormValue("country"))
	}
	return countryID
}

// sanitizePathSegment allow-lists a single path segment to letters (incl.
// Arabic), digits, dash and underscore — anything else (including "." and
// "/") is dropped. This closes path traversal via "..", absolute paths, or
// separator injection, since only characters that can never form a
// traversal sequence survive.
func sanitizePathSegment(value, fallback string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(value) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r >= 0x0600 && r <= 0x06FF: // Arabic block
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	safe := b.String()
	if safe == "" {
		return fallback
	}
	return safe
}

func privateTeacherPremiumDir(country, subject, category string) string {
	safeCountry := sanitizePathSegment(country, "jo")
	safeSubject := sanitizePathSegment(subject, "general")
	safeCategory := sanitizePathSegment(category, "files")
	return filepath.Join("storage", "private", "teacher-premium", safeCountry, safeSubject, safeCategory)
}

func parseOptionalUint(value string) *uint {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 {
		return nil
	}
	v := uint(parsed)
	return &v
}

func (h *Handler) requireActiveTeacherAccess(c *fiber.Ctx) (*models.User, error) {
	user := currentUser(c)
	if user == nil {
		return nil, utils.Unauthorized(c, "يرجى تسجيل الدخول أولًا")
	}
	access, err := h.svc.Access(user.ID)
	if err != nil || access == nil || !access.HasActive || !access.Allowed["teacher.subscription.access"] {
		return nil, utils.ForbiddenCode(c, "TEACHER_SUBSCRIPTION_INACTIVE", "اشتراك المعلم غير نشط أو تم إيقافه من الإدارة")
	}
	return user, nil
}

func (h *Handler) Plan(c *fiber.Ctx) error {
	plan, err := h.svc.PublicPlan()
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "success", plan)
}

func (h *Handler) PlanDesign(c *fiber.Ctx) error {
	return utils.Success(c, "success", h.svc.PlanDesign())
}

func (h *Handler) Access(c *fiber.Ctx) error {
	user := currentUser(c)
	if user == nil {
		return utils.Unauthorized(c, "يرجى تسجيل الدخول أولًا")
	}
	access, err := h.svc.Access(user.ID)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "success", access)
}

func (h *Handler) Me(c *fiber.Ctx) error {
	user := currentUser(c)
	if user == nil {
		return utils.Unauthorized(c, "يرجى تسجيل الدخول أولًا")
	}
	_ = h.svc.RegisterDevice(user.ID, c.IP(), c.Get("User-Agent"), c.Get("X-Device-Label"))
	summary, err := h.svc.MySummary(user.ID)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "success", summary)
}

func (h *Handler) Workspace(c *fiber.Ctx) error {
	user, accessErr := h.requireActiveTeacherAccess(c)
	if accessErr != nil {
		return accessErr
	}
	data, err := h.svc.Workspace(user.ID)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "success", data)
}

func (h *Handler) PremiumFiles(c *fiber.Ctx) error {
	user, accessErr := h.requireActiveTeacherAccess(c)
	if accessErr != nil {
		return accessErr
	}
	pag := utils.GetPagination(c)
	countryID := database.CountryIDFromHeader(c.Get("X-Country-Code"))
	if c.Query("country") != "" {
		countryID = database.CountryIDFromHeader(c.Query("country"))
	}
	items, total, err := h.svc.PremiumFiles(user.ID, countryID, c.Query("category"), c.Query("q"), pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Paginated(c, "success", items, pag.BuildMeta(total))
}

func (h *Handler) SaveLibraryItem(c *fiber.Ctx) error {
	user, accessErr := h.requireActiveTeacherAccess(c)
	if accessErr != nil {
		return accessErr
	}
	var req services.TeacherSaveLibraryRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات الحفظ غير صحيحة")
	}
	item, err := h.svc.SaveLibraryItem(user.ID, req)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Created(c, "تم الحفظ في مكتبتي", item)
}

func (h *Handler) Library(c *fiber.Ctx) error {
	user, accessErr := h.requireActiveTeacherAccess(c)
	if accessErr != nil {
		return accessErr
	}
	pag := utils.GetPagination(c)
	items, total, err := h.svc.Library(user.ID, pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Paginated(c, "success", items, pag.BuildMeta(total))
}

func (h *Handler) Downloads(c *fiber.Ctx) error {
	user, accessErr := h.requireActiveTeacherAccess(c)
	if accessErr != nil {
		return accessErr
	}
	pag := utils.GetPagination(c)
	items, total, err := h.svc.Downloads(user.ID, pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Paginated(c, "success", items, pag.BuildMeta(total))
}

func (h *Handler) AIGenerations(c *fiber.Ctx) error {
	user, accessErr := h.requireActiveTeacherAccess(c)
	if accessErr != nil {
		return accessErr
	}
	pag := utils.GetPagination(c)
	items, total, err := h.svc.AIGenerations(user.ID, pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Paginated(c, "success", items, pag.BuildMeta(total))
}

func (h *Handler) DownloadPremiumVaultFile(c *fiber.Ctx) error {
	user, accessErr := h.requireActiveTeacherAccess(c)
	if accessErr != nil {
		return accessErr
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف الملف غير صحيح")
	}

	countryID := countryIDFromRequest(c)
	file, subscription, err := h.svc.GetPremiumVaultFileForDownload(user.ID, countryID, uint(id))
	if err != nil {
		if errors.Is(err, services.ErrTeacherDeviceLimit) {
			return utils.ForbiddenCode(c, "TEACHER_DOWNLOAD_LIMIT_REACHED", "لقد وصلت إلى حد التحميلات المتاحة في اشتراكك الحالي")
		}
		return utils.ForbiddenCode(c, "TEACHER_PREMIUM_FILE_FORBIDDEN", "لا يمكنك تحميل هذا الملف أو لم يعد متاحًا ضمن اشتراكك")
	}

	if _, err := os.Stat(file.PrivatePath); err != nil {
		return utils.NotFound(c, "الملف غير موجود في التخزين الخاص")
	}

	download, err := h.svc.RecordPremiumVaultDownload(user.ID, subscription.ID, countryID, file, c.IP(), c.Get("User-Agent"))
	if err != nil {
		return utils.InternalError(c)
	}

	prepared, err := services.PrepareTeacherPremiumDownloadFile(user, file, download)
	if err != nil {
		return utils.InternalError(c)
	}
	if prepared != nil {
		_ = h.svc.UpdateDownloadWatermark(download.ID, prepared.Applied, prepared.Text, prepared.Path)
		c.Set("Content-Type", prepared.Mime)
		c.Set("X-Teacher-Watermark-Applied", fmt.Sprintf("%t", prepared.Applied))
		c.Set("X-Teacher-Download-Code", download.DownloadCode)
		c.Set("X-Teacher-Watermark", prepared.Text)
		return c.Download(prepared.Path, prepared.Name)
	}

	c.Set("Content-Type", file.MimeType)
	c.Set("X-Teacher-Download-Code", download.DownloadCode)
	c.Set("X-Teacher-Watermark", services.BuildTeacherWatermarkText(user.ID, download.DownloadCode))
	return c.Download(file.PrivatePath, file.OriginalFilename)
}

func (h *Handler) GenerateAI(c *fiber.Ctx) error {
	user, accessErr := h.requireActiveTeacherAccess(c)
	if accessErr != nil {
		return accessErr
	}
	var req services.TeacherAIGenerateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات أداة AI غير صحيحة")
	}
	item, err := h.svc.GenerateTeacherAI(user.ID, req)
	if err != nil {
		if errors.Is(err, services.ErrTeacherDeviceLimit) {
			return utils.ForbiddenCode(c, "TEACHER_AI_LIMIT_REACHED", "لقد وصلت إلى حد عمليات AI المتاحة في اشتراكك")
		}
		return utils.InternalError(c)
	}
	return utils.Created(c, "تم إنشاء المخرج الذكي", item)
}

func (h *Handler) ExportAI(c *fiber.Ctx) error {
	user, accessErr := h.requireActiveTeacherAccess(c)
	if accessErr != nil {
		return accessErr
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف عملية AI غير صحيح")
	}
	path, name, contentType, err := h.svc.ExportTeacherAI(user.ID, uint(id), c.Query("format", "word"))
	if err != nil {
		return utils.InternalError(c)
	}
	c.Set("Content-Type", contentType)
	return c.Download(path, name)
}

func (h *Handler) TeacherNotifications(c *fiber.Ctx) error {
	user, accessErr := h.requireActiveTeacherAccess(c)
	if accessErr != nil {
		return accessErr
	}
	pag := utils.GetPagination(c)
	items, total, err := h.svc.TeacherNotifications(user.ID, pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Paginated(c, "success", items, pag.BuildMeta(total))
}

func (h *Handler) PaymentSettings(c *fiber.Ctx) error {
	_ = h.svc.EnsureDefaultPaymentSettings()
	items, err := h.svc.PaymentSettings()
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "success", items)
}

func (h *Handler) CreateOrderWithProof(c *fiber.Ctx) error {
	user := currentUser(c)
	if user == nil {
		return utils.Unauthorized(c)
	}
	proofURL := ""
	proofPath := ""
	fileHeader, err := c.FormFile("payment_proof")
	if err == nil && fileHeader != nil {
		dir := filepath.Join("storage", "private", "teacher-payment-proofs", fmt.Sprintf("user-%d", user.ID))
		if err := os.MkdirAll(dir, 0750); err != nil {
			return utils.InternalError(c)
		}
		ext := filepath.Ext(fileHeader.Filename)
		stored := fmt.Sprintf("%d%s", time.Now().UnixNano(), ext)
		proofPath = filepath.Join(dir, stored)
		if err := c.SaveFile(fileHeader, proofPath); err != nil {
			return utils.InternalError(c)
		}
		proofURL = "private://" + proofPath
	}

	// The multipart form sends subjects as a comma-separated string (e.g.
	// "رياضيات,علوم,لغة عربية") since HTML forms don't carry JSON arrays.
	var subjects []string
	for _, s := range strings.Split(c.FormValue("subjects"), ",") {
		if v := strings.TrimSpace(s); v != "" {
			subjects = append(subjects, v)
		}
	}

	req := services.CreateTeacherOrderRequest{
		Subject:         c.FormValue("subject"),
		Subjects:        subjects,
		School:          c.FormValue("school"),
		City:            c.FormValue("city"),
		Phone:           c.FormValue("phone"),
		PaymentMethod:   c.FormValue("payment_method"),
		PayerName:       c.FormValue("payer_name"),
		PaymentRef:      c.FormValue("payment_reference"),
		PaymentProofURL: proofURL,
	}
	if len(subjects) == 0 && strings.TrimSpace(req.Subject) == "" {
		if proofPath != "" {
			_ = os.Remove(proofPath)
		}
		return utils.BadRequest(c, "يرجى تحديد المواد التي تدرّسها")
	}

	order, err := h.svc.CreateOrder(user, req)
	if err != nil {
		if proofPath != "" {
			_ = os.Remove(proofPath)
		}
		return utils.InternalError(c)
	}
	return utils.Created(c, "تم إرسال طلب الاشتراك مع إثبات الدفع", order)
}

func (h *Handler) CreateOrder(c *fiber.Ctx) error {
	user := currentUser(c)
	if user == nil {
		return utils.Unauthorized(c, "يرجى تسجيل الدخول أولًا")
	}
	var req services.CreateTeacherOrderRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات الطلب غير صحيحة")
	}
	if req.PaymentMethod == "" {
		return utils.BadRequest(c, "يرجى اختيار طريقة الدفع")
	}
	if len(req.Subjects) == 0 && strings.TrimSpace(req.Subject) == "" {
		return utils.BadRequest(c, "يرجى تحديد المواد التي تدرّسها")
	}
	order, err := h.svc.CreateOrder(user, req)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Created(c, "تم إرسال طلب الاشتراك بنجاح، سيتم مراجعته من الإدارة", order)
}

func (h *Handler) MyDevices(c *fiber.Ctx) error {
	user, accessErr := h.requireActiveTeacherAccess(c)
	if accessErr != nil {
		return accessErr
	}
	devices, err := h.svc.ListDevices(user.ID)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "success", devices)
}

func (h *Handler) DeactivateMyDevice(c *fiber.Ctx) error {
	user, accessErr := h.requireActiveTeacherAccess(c)
	if accessErr != nil {
		return accessErr
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف الجهاز غير صحيح")
	}
	if err := h.svc.DeactivateDevice(user.ID, uint(id)); err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "تم تعطيل الجهاز", nil)
}

func (h *Handler) AdminFinancialReport(c *fiber.Ctx) error {
	data, err := h.svc.AdminFinancialReport()
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "success", data)
}

func (h *Handler) AdminUsageAnalytics(c *fiber.Ctx) error {
	data, err := h.svc.AdminUsageAnalytics()
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "success", data)
}

func (h *Handler) AdminTeacherDetail(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("userID"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف المعلم غير صحيح")
	}
	data, err := h.svc.AdminTeacherDetail(uint(id))
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "success", data)
}

func (h *Handler) AdminDeactivateTeacherDevice(c *fiber.Ctx) error {
	admin := middleware.GetUser(c)
	if admin == nil {
		return utils.Unauthorized(c)
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف الجهاز غير صحيح")
	}
	var req services.TeacherDeactivateDeviceRequest
	_ = c.BodyParser(&req)
	if err := h.svc.AdminDeactivateTeacherDevice(uint(id), admin.ID, req, c.IP()); err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "تم تعطيل الجهاز", fiber.Map{"id": id})
}

func (h *Handler) AdminDashboard(c *fiber.Ctx) error {
	data, err := h.svc.AdminDashboard()
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "success", data)
}

func (h *Handler) AdminListSubscriptions(c *fiber.Ctx) error {
	pag := utils.GetPagination(c)
	items, total, err := h.svc.AdminListSubscriptions(c.Query("status"), c.Query("q"), pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Paginated(c, "success", items, pag.BuildMeta(total))
}

func (h *Handler) AdminListTeachers(c *fiber.Ctx) error {
	pag := utils.GetPagination(c)
	items, total, err := h.svc.AdminListTeachers(c.Query("q"), pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Paginated(c, "success", items, pag.BuildMeta(total))
}

func (h *Handler) AdminListDevices(c *fiber.Ctx) error {
	pag := utils.GetPagination(c)
	items, total, err := h.svc.AdminListDevices(queryUint(c, "user_id"), c.Query("active"), pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Paginated(c, "success", items, pag.BuildMeta(total))
}

func (h *Handler) AdminListPremiumDownloads(c *fiber.Ctx) error {
	pag := utils.GetPagination(c)
	items, total, err := h.svc.AdminListPremiumDownloads(queryUint(c, "user_id"), pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Paginated(c, "success", items, pag.BuildMeta(total))
}

func (h *Handler) AdminListAIGenerations(c *fiber.Ctx) error {
	pag := utils.GetPagination(c)
	items, total, err := h.svc.AdminListAIGenerations(queryUint(c, "user_id"), pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Paginated(c, "success", items, pag.BuildMeta(total))
}

func (h *Handler) AdminGetPremiumVaultFileDetail(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف الملف غير صحيح")
	}
	data, err := h.svc.AdminGetPremiumVaultFileDetail(countryIDFromRequest(c), uint(id))
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "success", data)
}

func (h *Handler) AdminArchivePremiumVaultFile(c *fiber.Ctx) error {
	admin := middleware.GetUser(c)
	if admin == nil {
		return utils.Unauthorized(c)
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف الملف غير صحيح")
	}
	var req services.TeacherArchiveFileRequest
	_ = c.BodyParser(&req)
	item, err := h.svc.AdminArchivePremiumVaultFile(countryIDFromRequest(c), uint(id), req, admin.ID, c.IP())
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "تمت أرشفة ملف Premium", item)
}

func (h *Handler) AdminReactivateSubscription(c *fiber.Ctx) error {
	admin := middleware.GetUser(c)
	if admin == nil {
		return utils.Unauthorized(c)
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف الاشتراك غير صحيح")
	}
	var req services.TeacherRenewSubscriptionRequest
	_ = c.BodyParser(&req)
	item, err := h.svc.ReactivateSubscription(uint(id), admin.ID, req, c.IP())
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "تمت إعادة تفعيل الاشتراك", item)
}

func (h *Handler) AdminRenewSubscription(c *fiber.Ctx) error {
	admin := middleware.GetUser(c)
	if admin == nil {
		return utils.Unauthorized(c)
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف الاشتراك غير صحيح")
	}
	var req services.TeacherRenewSubscriptionRequest
	_ = c.BodyParser(&req)
	item, err := h.svc.AdminRenewSubscription(uint(id), admin.ID, req, c.IP())
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "تم تجديد الاشتراك", item)
}

func (h *Handler) AdminRunExpiryMaintenance(c *fiber.Ctx) error {
	stats, err := h.svc.RunExpiryMaintenance()
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "تم تنفيذ صيانة انتهاء الاشتراكات", stats)
}

func (h *Handler) AdminListAuditLogs(c *fiber.Ctx) error {
	pag := utils.GetPagination(c)
	items, total, err := h.svc.AdminListAuditLogs(c.Query("entity_type"), queryUint(c, "entity_id"), pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Paginated(c, "success", items, pag.BuildMeta(total))
}

func (h *Handler) AdminListPremiumVaultFiles(c *fiber.Ctx) error {
	pag := utils.GetPagination(c)
	countryID := countryIDFromRequest(c)

	items, total, err := h.svc.AdminListPremiumVaultFiles(countryID, c.Query("q"), c.Query("active"), c.Query("category"), c.Query("subject"), pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Paginated(c, "success", items, pag.BuildMeta(total))
}

func (h *Handler) AdminCreatePremiumVaultFile(c *fiber.Ctx) error {
	admin := middleware.GetUser(c)
	if admin == nil {
		return utils.Unauthorized(c)
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		return utils.BadRequest(c, "يرجى رفع ملف Premium")
	}

	country := c.FormValue("country", "jo")
	category := c.FormValue("category", "exam")
	subject := c.FormValue("subject_name")
	if strings.TrimSpace(subject) == "" {
		return utils.BadRequest(c, "يرجى تحديد المادة")
	}

	dir := privateTeacherPremiumDir(country, subject, category)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return utils.InternalError(c)
	}

	ext := filepath.Ext(fileHeader.Filename)
	storedName := fmt.Sprintf("%d%s", time.Now().UnixNano(), ext)
	privatePath := filepath.Join(dir, storedName)

	if err := c.SaveFile(fileHeader, privatePath); err != nil {
		return utils.InternalError(c)
	}

	countryID := database.CountryIDFromHeader(country)
	req := services.TeacherPremiumVaultCreateRequest{
		Country:      country,
		Title:        c.FormValue("title"),
		Description:  c.FormValue("description"),
		GradeLevel:   parseOptionalUint(c.FormValue("grade_level")),
		GradeName:    c.FormValue("grade_name"),
		SubjectID:    parseOptionalUint(c.FormValue("subject_id")),
		SubjectName:  subject,
		SemesterID:   parseOptionalUint(c.FormValue("semester_id")),
		SemesterName: c.FormValue("semester_name"),
		Category:     category,
		FileType:     c.FormValue("file_type"),
	}

	item, err := h.svc.AdminCreatePremiumVaultFile(countryID, req, privatePath, storedName, fileHeader.Filename, fileHeader.Header.Get("Content-Type"), fileHeader.Size, admin.ID)
	if err != nil {
		_ = os.Remove(privatePath)
		return utils.InternalError(c, err.Error())
	}

	return utils.Created(c, "تم رفع ملف Premium للمعلمين بنجاح", item)
}

func (h *Handler) AdminUpdatePremiumVaultFile(c *fiber.Ctx) error {
	admin := middleware.GetUser(c)
	if admin == nil {
		return utils.Unauthorized(c)
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف الملف غير صحيح")
	}

	countryID := countryIDFromRequest(c)
	var req services.TeacherPremiumVaultCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات الملف غير صحيحة")
	}

	var active *bool
	if c.Query("active") != "" {
		v := c.Query("active") == "true" || c.Query("active") == "1"
		active = &v
	}

	item, err := h.svc.AdminUpdatePremiumVaultFile(countryID, uint(id), req, active, admin.ID)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "تم تحديث ملف Premium", item)
}

func (h *Handler) AdminDisablePremiumVaultFile(c *fiber.Ctx) error {
	admin := middleware.GetUser(c)
	if admin == nil {
		return utils.Unauthorized(c)
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف الملف غير صحيح")
	}

	countryID := countryIDFromRequest(c)
	active := false
	item, err := h.svc.AdminUpdatePremiumVaultFile(countryID, uint(id), services.TeacherPremiumVaultCreateRequest{}, &active, admin.ID)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "تم إيقاف ملف Premium", item)
}

func (h *Handler) AdminListPremiumFiles(c *fiber.Ctx) error {
	pag := utils.GetPagination(c)
	countryID := database.CountryIDFromHeader(c.Get("X-Country-Code"))
	if c.Query("country") != "" {
		countryID = database.CountryIDFromHeader(c.Query("country"))
	}

	items, total, err := h.svc.AdminListPremiumFiles(countryID, c.Query("q"), c.Query("premium"), c.Query("category"), c.Query("subject"), pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Paginated(c, "success", items, pag.BuildMeta(total))
}

func (h *Handler) AdminUpdatePremiumFile(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف الملف غير صحيح")
	}

	countryID := database.CountryIDFromHeader(c.Get("X-Country-Code"))
	if c.Query("country") != "" {
		countryID = database.CountryIDFromHeader(c.Query("country"))
	}

	var req services.TeacherPremiumFileAdminRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات الملف المدفوع غير صحيحة")
	}
	if req.Country != "" {
		countryID = database.CountryIDFromHeader(req.Country)
	}

	item, err := h.svc.AdminUpdatePremiumFile(countryID, uint(id), req)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "تم تحديث إعدادات Premium للملف", item)
}

func (h *Handler) AdminDisablePremiumFile(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف الملف غير صحيح")
	}

	countryID := database.CountryIDFromHeader(c.Get("X-Country-Code"))
	if c.Query("country") != "" {
		countryID = database.CountryIDFromHeader(c.Query("country"))
	}

	item, err := h.svc.AdminDisablePremiumFile(countryID, uint(id))
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "تم إلغاء Premium عن الملف", item)
}

func (h *Handler) AdminGetOrderDetail(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف الطلب غير صحيح")
	}
	data, err := h.svc.AdminGetOrderDetail(uint(id))
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "success", data)
}

func (h *Handler) AdminDownloadOrderProof(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف الطلب غير صحيح")
	}
	path, name, contentType, err := h.svc.AdminOrderProofPath(uint(id))
	if err != nil {
		return utils.NotFound(c, "إثبات الدفع غير موجود")
	}
	c.Set("Content-Type", contentType)
	if strings.HasPrefix(contentType, "image/") || contentType == "application/pdf" {
		return c.SendFile(path, false)
	}
	return c.Download(path, name)
}

func (h *Handler) AdminListOrders(c *fiber.Ctx) error {
	pag := utils.GetPagination(c)
	status := c.Query("status")
	orders, total, err := h.svc.ListOrders(status, pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Paginated(c, "success", orders, pag.BuildMeta(total))
}

func (h *Handler) AdminCancelSubscription(c *fiber.Ctx) error {
	admin := middleware.GetUser(c)
	if admin == nil {
		return utils.Unauthorized(c)
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف الاشتراك غير صحيح")
	}
	var req services.TeacherCancelRequest
	_ = c.BodyParser(&req)
	subscription, err := h.svc.AdminCancelSubscription(uint(id), admin.ID, req)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "تم إيقاف الاشتراك وإزالة دور Teacher Pro عند الحاجة", subscription)
}

func (h *Handler) AdminRemoveTeacherMembership(c *fiber.Ctx) error {
	admin := middleware.GetUser(c)
	if admin == nil {
		return utils.Unauthorized(c)
	}
	userID, err := strconv.ParseUint(c.Params("userID"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف المستخدم غير صحيح")
	}
	var req services.TeacherCancelRequest
	_ = c.BodyParser(&req)
	if err := h.svc.AdminRemoveTeacherMembership(uint(userID), admin.ID, req); err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "تم حذف عضوية المعلم من الاشتراك وإزالة دور Teacher Pro دون حذف حساب المستخدم", nil)
}

func (h *Handler) AdminApproveOrder(c *fiber.Ctx) error {
	admin := middleware.GetUser(c)
	if admin == nil {
		return utils.Unauthorized(c)
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف الطلب غير صحيح")
	}
	var req services.TeacherOrderReviewRequest
	_ = c.BodyParser(&req)
	sub, err := h.svc.ApproveOrder(uint(id), admin.ID, req)
	if err != nil {
		if errors.Is(err, services.ErrTeacherOrderNotPending) {
			return utils.BadRequest(c, "لا يمكن الموافقة على طلب غير معلق")
		}
		return utils.InternalError(c)
	}
	return utils.Success(c, "تم تفعيل اشتراك المعلم للفصل الدراسي", sub)
}

func (h *Handler) AdminRejectOrder(c *fiber.Ctx) error {
	admin := middleware.GetUser(c)
	if admin == nil {
		return utils.Unauthorized(c)
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف الطلب غير صحيح")
	}
	var req services.TeacherOrderReviewRequest
	_ = c.BodyParser(&req)
	if err := h.svc.RejectOrder(uint(id), admin.ID, req); err != nil {
		if errors.Is(err, services.ErrTeacherOrderNotPending) {
			return utils.BadRequest(c, "لا يمكن رفض طلب غير معلق")
		}
		return utils.InternalError(c)
	}
	return utils.Success(c, "تم رفض الطلب", nil)
}
