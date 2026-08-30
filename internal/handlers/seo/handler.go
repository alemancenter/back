package seo

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/models"
	"github.com/imanjo/fiber-api/internal/services"
	"github.com/imanjo/fiber-api/internal/services/contentaudit"
	"github.com/imanjo/fiber-api/internal/utils"
	"gorm.io/gorm"
)

type Handler struct {
	svc services.SEOService
	ai  services.AIService
}

func New(svc services.SEOService, ai services.AIService) *Handler { return &Handler{svc: svc, ai: ai} }

func countryID(c *fiber.Ctx) database.CountryID {
	if id, ok := c.Locals("country_id").(database.CountryID); ok && id != 0 {
		return id
	}
	for _, value := range []string{c.Query("country"), c.Query("country_code"), c.Get("X-Country-Id"), c.Get("X-Country-Code")} {
		if strings.TrimSpace(value) != "" {
			return database.CountryIDFromHeader(value)
		}
	}
	return database.CountryJordan
}

func userID(c *fiber.Ctx) uint {
	if user, ok := c.Locals("user").(*models.User); ok && user != nil {
		return user.ID
	}
	return 0
}

func canManageSEOContent(c *fiber.Ctx, contentType string) bool {
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		return false
	}
	if user.IsAdmin() || user.HasPermission("manage seo") {
		return true
	}
	return contentType == models.SEOContentTypeArticle && user.HasPermission("manage articles") ||
		contentType == models.SEOContentTypePost && user.HasPermission("manage posts")
}

func canAnalyzeSEO(c *fiber.Ctx) bool {
	user, ok := c.Locals("user").(*models.User)
	return ok && user != nil && (user.IsAdmin() || user.HasPermission("manage seo") || user.HasPermission("manage articles") || user.HasPermission("manage posts"))
}
func paramID(c *fiber.Ctx, name string) (uint, error) {
	value, err := strconv.ParseUint(c.Params(name), 10, 64)
	return uint(value), err
}

func handleError(c *fiber.Ctx, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, services.ErrNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		return utils.NotFound(c)
	case errors.Is(err, services.ErrSEOInvalidContentType):
		return utils.BadRequest(c, "نوع المحتوى غير مدعوم")
	case errors.Is(err, services.ErrSEOInvalidURL):
		return utils.BadRequest(c, "الرابط غير صالح")
	case errors.Is(err, services.ErrSEORedirectLoop):
		return utils.BadRequest(c, "التحويل ينشئ حلقة إعادة توجيه")
	case errors.Is(err, services.ErrSEOInvalidSchema):
		return utils.BadRequest(c, "بيانات Schema غير صالحة")
	case errors.Is(err, services.ErrSEOInvalidInput):
		return utils.BadRequest(c, "البيانات المدخلة غير صالحة")
	case errors.Is(err, services.ErrSEOIntegration):
		return c.Status(fiber.StatusBadGateway).JSON(utils.APIResponse{Success: false, Message: "تعذّر الاتصال بخدمة الفهرسة الخارجية"})
	case errors.Is(err, services.ErrSEOAuditRunning):
		return c.Status(fiber.StatusConflict).JSON(utils.APIResponse{Success: false, Message: "يوجد تدقيق SEO قيد التشغيل بالفعل"})
	case errors.Is(err, services.ErrSEOConflict):
		return c.Status(fiber.StatusConflict).JSON(utils.APIResponse{Success: false, Message: "يوجد سجل SEO آخر بالقيمة نفسها"})
	default:
		return utils.InternalError(c)
	}
}

func (h *Handler) Effective(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return utils.BadRequest(c, "معرف المحتوى غير صالح")
	}
	item, err := h.svc.GetEffective(c.Context(), countryID(c), c.Params("content_type"), id)
	if err != nil {
		return handleError(c, err)
	}
	return utils.Success(c, "بيانات SEO الفعالة", item)
}

func (h *Handler) ResolveRedirect(c *fiber.Ctx) error {
	path := c.Query("path")
	if path == "" {
		return utils.BadRequest(c, "path مطلوب")
	}
	item, err := h.svc.ResolveRedirect(c.Context(), countryID(c), path, c.Query("query"))
	if err != nil {
		return handleError(c, err)
	}
	return utils.Success(c, "تم العثور على تحويل", item)
}

type log404Request struct {
	Path     string `json:"path"`
	Query    string `json:"query"`
	Referrer string `json:"referrer"`
}

func (h *Handler) Record404(c *fiber.Ctx) error {
	var req log404Request
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}
	if req.Referrer == "" {
		req.Referrer = c.Get("Referer")
	}
	if err := h.svc.Record404(c.Context(), countryID(c), req.Path, req.Query, req.Referrer, c.Get("User-Agent")); err != nil {
		return handleError(c, err)
	}
	return utils.Success(c, "تم تسجيل الرابط المفقود", nil)
}

func (h *Handler) PublicAuthor(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return utils.BadRequest(c, "معرف المؤلف غير صالح")
	}
	item, err := h.svc.GetAuthor(c.Context(), id)
	if err != nil || !item.Active {
		return utils.NotFound(c, "ملف المؤلف غير موجود")
	}
	// Keep the public response limited to authorship fields. Database IDs,
	// account IDs, activity flags and timestamps are dashboard concerns and do
	// not belong in public Person schema responses.
	public := struct {
		PublicName      string `json:"public_name"`
		Headline        string `json:"headline"`
		Biography       string `json:"biography"`
		Expertise       string `json:"expertise"`
		Credentials     string `json:"credentials"`
		Education       string `json:"education"`
		Awards          string `json:"awards"`
		Employer        string `json:"employer"`
		ProfileURL      string `json:"profile_url"`
		ImageURL        string `json:"image_url"`
		SocialLinksJSON string `json:"social_links_json"`
		KnowsAboutJSON  string `json:"knows_about_json"`
	}{
		PublicName: item.PublicName, Headline: item.Headline, Biography: item.Biography,
		Expertise: item.Expertise, Credentials: item.Credentials, Education: item.Education,
		Awards: item.Awards, Employer: item.Employer, ProfileURL: item.ProfileURL,
		ImageURL: item.ImageURL, SocialLinksJSON: item.SocialLinksJSON, KnowsAboutJSON: item.KnowsAboutJSON,
	}
	return utils.Success(c, "ملف المؤلف", public)
}

func (h *Handler) Overview(c *fiber.Ctx) error {
	item, err := h.svc.Overview(c.Context(), countryID(c))
	if err != nil {
		return handleError(c, err)
	}
	return utils.Success(c, "ملخص ImanSEO", item)
}

func (h *Handler) Content(c *fiber.Ctx) error {
	pag := utils.GetPagination(c)
	rows, total, err := h.svc.ListContent(c.Context(), countryID(c), c.Query("type", "all"), c.Query("q"), pag.PerPage, pag.Offset)
	if err != nil {
		return handleError(c, err)
	}
	return utils.Paginated(c, "محتوى SEO", rows, pag.BuildMeta(total))
}

func (h *Handler) Metadata(c *fiber.Ctx) error {
	if !canManageSEOContent(c, c.Params("content_type")) {
		return utils.Forbidden(c)
	}
	id, err := paramID(c, "id")
	if err != nil {
		return utils.BadRequest(c, "معرف المحتوى غير صالح")
	}
	item, err := h.svc.GetMetadata(c.Context(), countryID(c), c.Params("content_type"), id)
	if err != nil {
		return handleError(c, err)
	}
	return utils.Success(c, "بيانات SEO", item)
}

func (h *Handler) SaveMetadata(c *fiber.Ctx) error {
	if !canManageSEOContent(c, c.Params("content_type")) {
		return utils.Forbidden(c)
	}
	id, err := paramID(c, "id")
	if err != nil {
		return utils.BadRequest(c, "معرف المحتوى غير صالح")
	}
	var req services.SEOMetadataInput
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}
	item, err := h.svc.SaveMetadata(c.Context(), countryID(c), c.Params("content_type"), id, req, userID(c))
	if err != nil {
		return handleError(c, err)
	}
	return utils.Success(c, "تم حفظ بيانات SEO", item)
}

func (h *Handler) Analyze(c *fiber.Ctx) error {
	if !canAnalyzeSEO(c) {
		return utils.Forbidden(c)
	}
	var req services.SEOAnalysisInput
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}
	return utils.Success(c, "نتيجة تحليل SEO", h.svc.Analyze(req))
}

type optimizeRequest struct {
	Title        string `json:"title"`
	Content      string `json:"content"`
	FocusKeyword string `json:"focus_keyword"`
	ContentType  string `json:"content_type"`
	CountryCode  string `json:"country_code"`
	GradeName    string `json:"grade_name"`
	SubjectName  string `json:"subject_name"`
	CategoryName string `json:"category_name"`
}

// Optimize fills the whole SEO metadata bundle from the current title + content,
// tuned to the AnalyzeSEO rubric, and returns the projected analysis so the
// editor can show the new score immediately.
func (h *Handler) Optimize(c *fiber.Ctx) error {
	if !canAnalyzeSEO(c) {
		return utils.Forbidden(c)
	}
	var req optimizeRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" && strings.TrimSpace(req.Content) == "" {
		return utils.BadRequest(c, "أضف عنوانًا ومحتوى أولًا")
	}

	ctx, cancel := context.WithTimeout(c.Context(), 90*time.Second)
	defer cancel()
	bundle, provider, aiErr, err := contentaudit.GenerateDraftSEOBundle(ctx, h.ai, contentaudit.SEOOptimizeInput{
		Title:        req.Title,
		ContentHTML:  req.Content,
		FocusKeyword: req.FocusKeyword,
		ContentType:  req.ContentType,
		CountryCode:  firstSEOOptimizeValue(req.CountryCode, database.CountryCode(countryID(c))),
		GradeName:    req.GradeName,
		SubjectName:  req.SubjectName,
		CategoryName: req.CategoryName,
	})
	if err != nil {
		return utils.BadRequest(c, "تعذّر توليد تحسين SEO لهذا المحتوى حاليًا")
	}

	projected := h.svc.Analyze(services.SEOAnalysisInput{
		Title:           bundle.SEOTitle,
		Content:         req.Content,
		MetaDescription: bundle.MetaDescription,
		FocusKeyword:    bundle.FocusKeyword,
		SchemaType:      bundle.SchemaType,
	})
	payload := fiber.Map{
		"fields":   bundle,
		"analysis": projected,
		"score":    projected.Score,
		"provider": provider,
	}
	if aiErr != "" {
		// Surfaced to the editor so a silent fall back to the deterministic
		// bundle is visible ("model not found", "401", timeout, …).
		if len([]rune(aiErr)) > 300 {
			aiErr = string([]rune(aiErr)[:300])
		}
		payload["ai_error"] = aiErr
	}
	return utils.Success(c, "تم توليد تحسين SEO", payload)
}

func firstSEOOptimizeValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (h *Handler) Revisions(c *fiber.Ctx) error {
	if !canManageSEOContent(c, c.Params("content_type")) {
		return utils.Forbidden(c)
	}
	id, err := paramID(c, "id")
	if err != nil {
		return utils.BadRequest(c, "معرف المحتوى غير صالح")
	}
	rows, err := h.svc.ListRevisions(c.Context(), countryID(c), c.Params("content_type"), id, c.QueryInt("limit", 30))
	if err != nil {
		return handleError(c, err)
	}
	return utils.Success(c, "نسخ SEO", rows)
}

func (h *Handler) RestoreRevision(c *fiber.Ctx) error {
	if !canManageSEOContent(c, c.Params("content_type")) {
		return utils.Forbidden(c)
	}
	id, err := paramID(c, "id")
	if err != nil {
		return utils.BadRequest(c, "معرف المحتوى غير صالح")
	}
	revisionID, err := paramID(c, "revision_id")
	if err != nil {
		return utils.BadRequest(c, "معرف النسخة غير صالح")
	}
	item, err := h.svc.RestoreRevision(c.Context(), countryID(c), c.Params("content_type"), id, revisionID, userID(c))
	if err != nil {
		return handleError(c, err)
	}
	return utils.Success(c, "تمت استعادة نسخة SEO", item)
}

func (h *Handler) Redirects(c *fiber.Ctx) error {
	pag := utils.GetPagination(c)
	rows, total, err := h.svc.ListRedirects(c.Context(), countryID(c), c.Query("q"), pag.PerPage, pag.Offset)
	if err != nil {
		return handleError(c, err)
	}
	return utils.Paginated(c, "التحويلات", rows, pag.BuildMeta(total))
}
func (h *Handler) CreateRedirect(c *fiber.Ctx) error {
	var req services.SEORedirectInput
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}
	item, err := h.svc.CreateRedirect(c.Context(), countryID(c), req, userID(c))
	if err != nil {
		return handleError(c, err)
	}
	return utils.Created(c, "تم إنشاء التحويل", item)
}
func (h *Handler) UpdateRedirect(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return utils.BadRequest(c, "معرف التحويل غير صالح")
	}
	var req services.SEORedirectInput
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}
	item, err := h.svc.UpdateRedirect(c.Context(), countryID(c), id, req, userID(c))
	if err != nil {
		return handleError(c, err)
	}
	return utils.Success(c, "تم تحديث التحويل", item)
}
func (h *Handler) DeleteRedirect(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return utils.BadRequest(c, "معرف التحويل غير صالح")
	}
	if err := h.svc.DeleteRedirect(c.Context(), countryID(c), id); err != nil {
		return handleError(c, err)
	}
	return utils.NoContent(c)
}

func (h *Handler) NotFoundLogs(c *fiber.Ctx) error {
	pag := utils.GetPagination(c)
	resolved := c.QueryBool("resolved", false)
	rows, total, err := h.svc.List404(c.Context(), countryID(c), resolved, c.Query("q"), pag.PerPage, pag.Offset)
	if err != nil {
		return handleError(c, err)
	}
	return utils.Paginated(c, "سجل 404", rows, pag.BuildMeta(total))
}

type resolve404Request struct {
	RedirectID *uint `json:"redirect_id"`
}

func (h *Handler) Resolve404Log(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return utils.BadRequest(c, "المعرف غير صالح")
	}
	var req resolve404Request
	_ = c.BodyParser(&req)
	if err := h.svc.Resolve404(c.Context(), countryID(c), id, req.RedirectID); err != nil {
		return handleError(c, err)
	}
	return utils.Success(c, "تمت معالجة السجل", nil)
}
func (h *Handler) Clear404(c *fiber.Ctx) error {
	if err := h.svc.Clear404(c.Context(), countryID(c), c.QueryBool("resolved", true)); err != nil {
		return handleError(c, err)
	}
	return utils.NoContent(c)
}

func (h *Handler) LinkSuggestions(c *fiber.Ctx) error {
	if !canManageSEOContent(c, c.Params("content_type")) {
		return utils.Forbidden(c)
	}
	id, err := paramID(c, "id")
	if err != nil {
		return utils.BadRequest(c, "معرف المحتوى غير صالح")
	}
	rows, err := h.svc.LinkSuggestions(c.Context(), countryID(c), c.Params("content_type"), id, c.QueryInt("limit", 10))
	if err != nil {
		return handleError(c, err)
	}
	return utils.Success(c, "اقتراحات الروابط الداخلية", rows)
}

func (h *Handler) StartAudit(c *fiber.Ctx) error {
	run, err := h.svc.StartAudit(c.Context(), countryID(c), userID(c))
	if err != nil {
		return handleError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(utils.APIResponse{Success: true, Message: "بدأ تدقيق SEO", Data: run})
}
func (h *Handler) Audits(c *fiber.Ctx) error {
	rows, err := h.svc.ListAudits(c.Context(), countryID(c), c.QueryInt("limit", 20))
	if err != nil {
		return handleError(c, err)
	}
	return utils.Success(c, "سجل تدقيق SEO", rows)
}
func (h *Handler) Audit(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return utils.BadRequest(c, "معرف التدقيق غير صالح")
	}
	item, err := h.svc.GetAudit(c.Context(), countryID(c), id)
	if err != nil {
		return handleError(c, err)
	}
	return utils.Success(c, "تفاصيل تدقيق SEO", item)
}
func (h *Handler) AuditIssues(c *fiber.Ctx) error {
	id, err := paramID(c, "id")
	if err != nil {
		return utils.BadRequest(c, "معرف التدقيق غير صالح")
	}
	pag := utils.GetPagination(c)
	rows, total, err := h.svc.ListAuditIssues(c.Context(), countryID(c), id, c.Query("severity"), pag.PerPage, pag.Offset)
	if err != nil {
		return handleError(c, err)
	}
	return utils.Paginated(c, "مشكلات تدقيق SEO", rows, pag.BuildMeta(total))
}

func (h *Handler) Authors(c *fiber.Ctx) error {
	rows, err := h.svc.ListAuthors(c.Context(), c.QueryBool("active", false))
	if err != nil {
		return handleError(c, err)
	}
	return utils.Success(c, "ملفات المؤلفين", rows)
}
func (h *Handler) SaveAuthor(c *fiber.Ctx) error {
	var req models.SEOAuthorProfile
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}
	item, err := h.svc.UpsertAuthor(c.Context(), req)
	if err != nil {
		return handleError(c, err)
	}
	return utils.Success(c, "تم حفظ ملف المؤلف", item)
}

type indexNowRequest struct {
	URLs []string `json:"urls"`
}

func (h *Handler) IndexNow(c *fiber.Ctx) error {
	var req indexNowRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}
	rows, err := h.svc.SubmitIndexNow(c.Context(), countryID(c), req.URLs)
	if err != nil {
		return handleError(c, err)
	}
	return utils.Success(c, "تم إرسال الروابط إلى IndexNow", rows)
}
