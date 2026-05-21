package articles

import (
	"fmt"
	"mime"
	"strconv"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/middleware"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
)

// Handler contains articles route handlers
type Handler struct {
	svc          services.ArticleService
	notification services.NotificationService
}

// New creates a new articles Handler
func New(svc services.ArticleService, notification services.NotificationService) *Handler {
	return &Handler{
		svc:          svc,
		notification: notification,
	}
}

// List returns a paginated list of published articles
// @Summary List Public Articles
// @Description Returns a paginated list of active/published articles
// @Tags Articles
// @Produce json
// @Param X-Country-Id header string false "Country ID"
// @Param grade_level query string false "Filter by grade level"
// @Param subject_id query int false "Filter by subject ID"
// @Param semester_id query int false "Filter by semester ID"
// @Param search query string false "Search query"
// @Param file_category query string false "Filter by file category (e.g. worksheet, exam)"
// @Param page query int false "Page number"
// @Param limit query int false "Items per page"
// @Success 200 {object} utils.APIResponse{data=[]models.Article}
// @Failure 500 {object} utils.APIResponse
// @Router /articles [get]
func (h *Handler) List(c *fiber.Ctx) error {
	countryID, _ := c.Locals("country_id").(database.CountryID)
	pag := utils.GetPagination(c)

	status := 1
	filters := &models.ArticleFilter{
		Status: &status,
	}

	// Filters
	if gradeLevel := c.Query("grade_level"); gradeLevel != "" {
		filters.GradeLevel = gradeLevel
	}
	if subjectID := c.Query("subject_id"); subjectID != "" {
		if id, err := strconv.ParseUint(subjectID, 10, 64); err == nil {
			parsedID := uint(id)
			filters.SubjectID = &parsedID
		}
	}
	if semesterID := c.Query("semester_id"); semesterID != "" {
		if id, err := strconv.ParseUint(semesterID, 10, 64); err == nil {
			parsedID := uint(id)
			filters.SemesterID = &parsedID
		}
	}
	if q := c.Query("q"); q != "" {
		filters.Query = q
	} else if search := c.Query("search"); search != "" {
		filters.Query = search
	}
	if fc := c.Query("file_category"); fc != "" {
		filters.FileCategory = fc
	}

	articles, total, err := h.svc.List(countryID, pag, filters)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Paginated(c, "success", newDashboardArticleResponses(articles), pag.BuildMeta(total))
}

// Show returns a single article
// @Summary Get Article
// @Description Get a single published article by ID
// @Tags Articles
// @Produce json
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Article ID"
// @Success 200 {object} utils.APIResponse{data=models.Article}
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /articles/{id} [get]
func (h *Handler) Show(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	article, err := h.svc.GetByID(countryID, id)
	if err != nil {
		if err == services.ErrNotFound {
			return utils.NotFound(c)
		}
		return utils.InternalError(c)
	}

	if article.Status != 1 {
		return utils.NotFound(c)
	}

	return utils.Success(c, "success", article)
}

// ByClass returns articles filtered by grade level
// @Summary List Articles by Grade Level
// @Description Returns a paginated list of published articles for a specific grade level
// @Tags Articles
// @Produce json
// @Param X-Country-Id header string false "Country ID"
// @Param grade_level path string true "Grade Level"
// @Param page query int false "Page number"
// @Param limit query int false "Items per page"
// @Success 200 {object} utils.APIResponse{data=[]models.Article}
// @Failure 500 {object} utils.APIResponse
// @Router /articles/by-class/{grade_level} [get]
func (h *Handler) ByClass(c *fiber.Ctx) error {
	gradeLevel := c.Params("grade_level")
	countryID, _ := c.Locals("country_id").(database.CountryID)
	pag := utils.GetPagination(c)

	articles, total, err := h.svc.GetByGradeLevel(countryID, gradeLevel, pag)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Paginated(c, "success", newDashboardArticleResponses(articles), pag.BuildMeta(total))
}

// ByKeyword returns articles filtered by keyword
// @Summary List Articles by Keyword
// @Description Returns a paginated list of published articles containing a specific keyword
// @Tags Articles
// @Produce json
// @Param X-Country-Id header string false "Country ID"
// @Param keyword path string true "Keyword"
// @Param page query int false "Page number"
// @Param limit query int false "Items per page"
// @Success 200 {object} utils.APIResponse{data=[]models.Article}
// @Failure 500 {object} utils.APIResponse
// @Router /articles/by-keyword/{keyword} [get]
func (h *Handler) ByKeyword(c *fiber.Ctx) error {
	keyword := c.Params("keyword")
	countryID, _ := c.Locals("country_id").(database.CountryID)
	pag := utils.GetPagination(c)

	articles, total, err := h.svc.GetByKeyword(countryID, keyword, pag)
	if err != nil {
		if err == services.ErrNotFound {
			return utils.Paginated(c, "success", []models.Article{}, pag.BuildMeta(0))
		}
		return utils.InternalError(c)
	}

	return utils.Paginated(c, "success", newDashboardArticleResponses(articles), pag.BuildMeta(total))
}

// GetDownloadToken returns a short-lived signed token for a file download.
// @Summary Get File Download Token
// @Description Generates a short-lived, signed token allowing a user to download an article's file securely
// @Tags Articles
// @Produce json
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "File ID"
// @Success 200 {object} utils.APIResponse{data=map[string]string}
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /articles/file/{id}/download-url [get]
func (h *Handler) GetDownloadToken(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	token, err := h.svc.GetSignedDownloadToken(countryID, id)
	if err != nil {
		if err == services.ErrNotFound {
			return utils.NotFound(c)
		}
		return utils.InternalError(c)
	}

	return utils.Success(c, "success", fiber.Map{"token": token})
}

// DownloadFileSigned validates a signed token and serves the file.
// @Summary Download File via Token
// @Description Serves the actual file binary if the provided download token is valid and unexpired
// @Tags Articles
// @Produce application/octet-stream
// @Param token query string true "Signed Token"
// @Success 200 {file} file "Binary File"
// @Failure 400 {object} utils.APIResponse
// @Failure 401 {object} utils.APIResponse
// @Router /articles/download [get]
func (h *Handler) DownloadFileSigned(c *fiber.Ctx) error {
	token := c.Query("token")
	if token == "" {
		return utils.BadRequest(c, "رمز التحميل مطلوب")
	}

	file, absPath, err := h.svc.GetFileBySignedToken(token)
	if err != nil {
		return utils.Unauthorized(c, "رمز التحميل غير صالح أو منتهي الصلاحية")
	}

	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": file.FileName})
	c.Set("Content-Disposition", disposition)
	c.Set("Content-Type", file.MimeType)
	return c.SendFile(absPath)
}

// DownloadFile serves an article file for download (legacy — prefer signed URL via GetDownloadToken)
// @Summary Download File Directly (Legacy)
// @Description Direct file download. Note: Use GetDownloadToken instead for secure downloads
// @Tags Articles
// @Produce application/octet-stream
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "File ID"
// @Success 200 {file} file "Binary File"
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /articles/file/{id}/download [get]
func (h *Handler) DownloadFile(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	file, absPath, err := h.svc.GetFileForDownload(countryID, id)
	if err != nil {
		if err == services.ErrNotFound {
			return utils.NotFound(c)
		}
		return utils.InternalError(c)
	}

	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": file.FileName})
	c.Set("Content-Disposition", disposition)
	c.Set("Content-Type", file.MimeType)
	return c.SendFile(absPath)
}

// ------- Dashboard Article Handlers -------

type dashboardArticleResponse struct {
	models.Article
	Views      int `json:"views"`
	ViewsCount int `json:"views_count"`
	ViewCount  int `json:"view_count"`
}

func newDashboardArticleResponse(article models.Article) dashboardArticleResponse {
	views := article.VisitCount
	if views < 0 {
		views = 0
	}
	return dashboardArticleResponse{
		Article:    article,
		Views:      views,
		ViewsCount: views,
		ViewCount:  views,
	}
}

func newDashboardArticleResponses(articles []models.Article) []dashboardArticleResponse {
	items := make([]dashboardArticleResponse, 0, len(articles))
	for _, article := range articles {
		items = append(items, newDashboardArticleResponse(article))
	}
	return items
}

// DashboardList returns all articles for dashboard management
// @Summary List Articles (Dashboard)
// @Description Returns a paginated list of all articles for dashboard management
// @Tags Articles
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param status query int false "Filter by status"
// @Param q query string false "Search query"
// @Param page query int false "Page number"
// @Param limit query int false "Items per page"
// @Success 200 {object} utils.APIResponse{data=[]models.Article}
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/articles [get]
func (h *Handler) DashboardList(c *fiber.Ctx) error {
	countryID, _ := c.Locals("country_id").(database.CountryID)
	pag := utils.GetPagination(c)

	filters := &models.ArticleFilter{
		Order: "created_at DESC",
	}

	if status := c.Query("status"); status != "" {
		if s, err := strconv.Atoi(status); err == nil {
			filters.Status = &s
		}
	}
	if q := c.Query("q"); q != "" {
		filters.Query = q
	} else if search := c.Query("search"); search != "" {
		filters.Query = search
	}

	articles, total, err := h.svc.List(countryID, pag, filters)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Paginated(c, "success", newDashboardArticleResponses(articles), pag.BuildMeta(total))
}

// DashboardCreateData returns metadata needed by the create article form.
// @Summary Get Create Form Metadata
// @Description Returns necessary data (subjects, classes, semesters) to populate the article creation form
// @Tags Articles
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Success 200 {object} utils.APIResponse{data=map[string]interface{}}
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/articles/create [get]
func (h *Handler) DashboardCreateData(c *fiber.Ctx) error {
	countryID, _ := c.Locals("country_id").(database.CountryID)

	data, err := h.svc.GetDashboardCreateData(countryID)
	if err != nil {
		return utils.InternalError(c, "فشل تحميل بيانات إنشاء المقالة")
	}

	return utils.Success(c, "success", data)
}

// DashboardEditData returns the article with auxiliary lists for the edit form.
// @Summary Get Edit Form Data
// @Description Returns the article details along with necessary data (subjects, classes) to populate the edit form
// @Tags Articles
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Article ID"
// @Success 200 {object} utils.APIResponse{data=map[string]interface{}}
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/articles/{id}/edit [get]
func (h *Handler) DashboardEditData(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	data, err := h.svc.GetDashboardEditData(countryID, id)
	if err != nil {
		if err == services.ErrNotFound {
			return utils.NotFound(c)
		}
		return utils.InternalError(c, "فشل تحميل بيانات المقالة")
	}

	return utils.Success(c, "success", data)
}

// DashboardCreate creates a new article
// @Summary Create Article
// @Description Create a new article from the dashboard
// @Tags Articles
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param request body services.ArticleInput true "Article data"
// @Success 201 {object} utils.APIResponse{data=models.Article}
// @Failure 400 {object} utils.APIResponse
// @Failure 422 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/articles [post]
func (h *Handler) DashboardCreate(c *fiber.Ctx) error {
	var req services.ArticleInput
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	if errs := utils.Validate(req); errs != nil {
		return utils.ValidationError(c, errs)
	}

	user := middleware.GetUser(c)
	countryID, _ := c.Locals("country_id").(database.CountryID)

	var authorID *uint
	if user != nil {
		authorID = &user.ID
	}

	article, err := h.svc.CreateArticle(countryID, &req, authorID)
	if err != nil {
		return utils.InternalError(c, "فشل إنشاء المقالة")
	}

	if h.notification != nil {
		includeIDs := []uint{}
		if authorID != nil {
			includeIDs = append(includeIDs, *authorID)
		}
		go func() {
			_ = h.notification.NotifyUsersWithPermissions(
				"App\\Notifications\\ArticleCreated",
				fmt.Sprintf("مقال جديد: %s", article.Title),
				fmt.Sprintf("تم إنشاء مقال جديد بعنوان \"%s\"", article.Title),
				fmt.Sprintf("/dashboard/articles/edit/%d", article.ID),
				[]string{"manage articles", "manage notifications"},
				includeIDs...,
			)
		}()
	}

	return utils.Created(c, "تم إنشاء المقالة بنجاح", article)
}

// DashboardUpdate updates an existing article
// @Summary Update Article
// @Description Update an existing article from the dashboard
// @Tags Articles
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Article ID"
// @Param request body services.ArticleInput true "Article data"
// @Success 200 {object} utils.APIResponse{data=models.Article}
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/articles/{id} [put]
func (h *Handler) DashboardUpdate(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	var req services.ArticleInput
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	user := middleware.GetUser(c)
	var authorID *uint
	if user != nil {
		authorID = &user.ID
	}

	article, err := h.svc.UpdateArticle(countryID, id, &req, authorID)
	if err != nil {
		if err == services.ErrNotFound {
			return utils.NotFound(c)
		}
		return utils.InternalError(c, "فشل تحديث المقالة")
	}

	if h.notification != nil {
		includeIDs := []uint{}
		if authorID != nil {
			includeIDs = append(includeIDs, *authorID)
		}
		if article.AuthorID != nil {
			includeIDs = append(includeIDs, *article.AuthorID)
		}
		go func() {
			_ = h.notification.NotifyUsersWithPermissions(
				"App\\Notifications\\ArticleUpdated",
				"تم تحديث مقالة",
				fmt.Sprintf("تم تحديث مقالة: %s", article.Title),
				fmt.Sprintf("/dashboard/articles/edit/%d", article.ID),
				[]string{"manage articles", "manage notifications"},
				includeIDs...,
			)
		}()
	}

	return utils.Success(c, "تم تحديث المقالة بنجاح", article)
}

// DashboardDelete deletes an article
// @Summary Delete Article
// @Description Delete an article by ID
// @Tags Articles
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Article ID"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/articles/{id} [delete]
func (h *Handler) DashboardDelete(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)
	user := middleware.GetUser(c)
	var authorID *uint
	if user != nil {
		authorID = &user.ID
	}

	// Fetch article before deletion so we can notify the author
	article, _ := h.svc.GetByID(countryID, id)

	if err := h.svc.DeleteArticle(countryID, id, authorID); err != nil {
		if err == services.ErrNotFound {
			return utils.NotFound(c)
		}
		return utils.InternalError(c, "فشل حذف المقالة")
	}

	if h.notification != nil && article != nil {
		includeIDs := []uint{}
		if authorID != nil {
			includeIDs = append(includeIDs, *authorID)
		}
		if article.AuthorID != nil {
			includeIDs = append(includeIDs, *article.AuthorID)
		}
		go func(title string) {
			_ = h.notification.NotifyUsersWithPermissions(
				"App\\Notifications\\ArticleDeleted",
				"تم حذف مقالة",
				fmt.Sprintf("تم حذف مقالة: %s", title),
				"/dashboard/articles",
				[]string{"manage articles", "manage notifications"},
				includeIDs...,
			)
		}(article.Title)
	}

	return utils.Success(c, "تم حذف المقالة بنجاح", nil)
}

// DashboardPublish publishes an article
// @Summary Publish Article
// @Description Change article status to published
// @Tags Articles
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Article ID"
// @Success 200 {object} utils.APIResponse{data=models.Article}
// @Router /dashboard/articles/{id}/publish [post]
func (h *Handler) DashboardPublish(c *fiber.Ctx) error {
	return h.setArticleStatus(c, 1, "تم نشر المقالة بنجاح")
}

// DashboardUnpublish unpublishes an article
// @Summary Unpublish Article
// @Description Change article status to draft/unpublished
// @Tags Articles
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Article ID"
// @Success 200 {object} utils.APIResponse{data=models.Article}
// @Router /dashboard/articles/{id}/unpublish [post]
func (h *Handler) DashboardUnpublish(c *fiber.Ctx) error {
	return h.setArticleStatus(c, 0, "تم إلغاء نشر المقالة بنجاح")
}

func (h *Handler) setArticleStatus(c *fiber.Ctx, status int8, message string) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	article, err := h.svc.SetArticleStatus(countryID, id, status)
	if err != nil {
		if err == services.ErrNotFound {
			return utils.NotFound(c)
		}
		return utils.InternalError(c)
	}

	if status == 1 && h.notification != nil {
		user := middleware.GetUser(c)
		includeIDs := []uint{}
		if user != nil {
			includeIDs = append(includeIDs, user.ID)
		}
		if article.AuthorID != nil {
			includeIDs = append(includeIDs, *article.AuthorID)
		}
		go func() {
			_ = h.notification.NotifyUsersWithPermissions(
				"App\\Notifications\\ArticlePublished",
				"تم نشر مقالة",
				fmt.Sprintf("تم نشر مقالة: %s", article.Title),
				fmt.Sprintf("/dashboard/articles/edit/%d", article.ID),
				[]string{"manage articles", "manage notifications"},
				includeIDs...,
			)
		}()
	}

	return utils.Success(c, message, article)
}

// DashboardStats returns article statistics
// @Summary Get Article Statistics
// @Description Returns dashboard statistics for articles (total, published, draft, pending)
// @Tags Articles
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Success 200 {object} utils.APIResponse{data=services.ArticleDashboardStats}
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/articles/stats [get]
func (h *Handler) DashboardStats(c *fiber.Ctx) error {
	countryID, _ := c.Locals("country_id").(database.CountryID)

	stats, err := h.svc.GetDashboardStats(countryID)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Success(c, "success", stats)
}
