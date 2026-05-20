package files

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/alemancenter/fiber-api/internal/database"
	_ "github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
)

// slugify converts a string to a filesystem-safe slug, preserving Unicode
// letters and digits (including Arabic), replacing everything else with hyphens.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevHyphen := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevHyphen = false
		} else if !prevHyphen {
			b.WriteByte('-')
			prevHyphen = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// articleGradeName returns the grade_name of the school class that owns the article.
// Returns "" if the article or class cannot be found.
func articleGradeName(countryID database.CountryID, articleID uint) string {
	db := database.GetManager().Get(countryID)
	if db == nil {
		return ""
	}
	var row struct {
		GradeLevel *string `gorm:"column:grade_level"`
	}
	if err := db.Table("articles").Select("grade_level").
		Where("id = ?", articleID).First(&row).Error; err != nil {
		return ""
	}
	if row.GradeLevel == nil || *row.GradeLevel == "" {
		return ""
	}
	var cls struct {
		GradeName string `gorm:"column:grade_name"`
	}
	if err := db.Table("school_classes").Select("grade_name").
		Where("grade_level = ?", *row.GradeLevel).First(&cls).Error; err != nil {
		return ""
	}
	return cls.GradeName
}

// Handler contains file management route handlers
type Handler struct {
	svc *services.FileService
}

// New creates a new files Handler
func New(svc *services.FileService) *Handler {
	return &Handler{svc: svc}
}

// Info returns file metadata with its parent article or post.
// @Summary Get File Info
// @Description Returns metadata about a file along with its parent article or post
// @Tags Files
// @Produce json
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "File ID"
// @Success 200 {object} utils.APIResponse{data=services.FileInfoResponse}
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /files/{id}/info [get]
func (h *Handler) Info(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	resp, err := h.svc.GetFileWithParent(countryID, id)
	if err != nil {
		if err == services.ErrNotFound {
			return utils.NotFound(c)
		}
		return utils.InternalError(c)
	}

	return utils.Success(c, "success", resp)
}

// IncrementView increments the file view count
// @Summary Increment File View
// @Description Increment the view counter for a specific file
// @Tags Files
// @Produce json
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "File ID"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /files/{id}/increment-view [post]
func (h *Handler) IncrementView(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	if err := h.svc.IncrementViewCount(countryID, id); err != nil {
		return utils.InternalError(c)
	}

	return utils.Success(c, "success", nil)
}

// UploadImage handles public image upload
// @Summary Upload Image
// @Description Upload a public image (e.g. avatar, basic photo)
// @Tags Files
// @Accept mpfd
// @Produce json
// @Param image formData file true "Image file to upload"
// @Success 201 {object} utils.APIResponse{data=services.UploadResponse}
// @Failure 400 {object} utils.APIResponse
// @Router /upload/image [post]
func (h *Handler) UploadImage(c *fiber.Ctx) error {
	photo, err := c.FormFile("image")
	if err != nil {
		return utils.BadRequest(c, "الصورة مطلوبة")
	}

	uploaded, err := h.svc.UploadImage(photo, "images")
	if err != nil {
		return utils.BadRequest(c, err.Error())
	}

	return utils.Created(c, "تم رفع الصورة بنجاح", services.UploadResponse{
		Path: uploaded.Path,
		URL:  uploaded.URL,
		Name: uploaded.Name,
		Size: uploaded.Size,
		Type: uploaded.MimeType,
	})
}

// UploadDocument handles public document upload
// @Summary Upload Document
// @Description Upload a public document (e.g. PDF, DOCX)
// @Tags Files
// @Accept mpfd
// @Produce json
// @Param file formData file true "Document file to upload"
// @Success 201 {object} utils.APIResponse{data=services.UploadResponse}
// @Failure 400 {object} utils.APIResponse
// @Router /upload/file [post]
func (h *Handler) UploadDocument(c *fiber.Ctx) error {
	doc, err := c.FormFile("file")
	if err != nil {
		return utils.BadRequest(c, "الملف مطلوب")
	}

	uploaded, err := h.svc.UploadDocument(doc, "documents")
	if err != nil {
		return utils.BadRequest(c, err.Error())
	}

	return utils.Created(c, "تم رفع الملف بنجاح", services.UploadResponse{
		Path: uploaded.Path,
		URL:  uploaded.URL,
		Name: uploaded.Name,
		Size: uploaded.Size,
		Type: uploaded.MimeType,
	})
}

// DashboardList returns all files for dashboard management
// @Summary List Files (Dashboard)
// @Description Returns a paginated list of all files for dashboard management
// @Tags Files
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param type query string false "Filter by file type (e.g. image, document)"
// @Param article_id query int false "Filter by associated article ID"
// @Param page query int false "Page number"
// @Param limit query int false "Items per page"
// @Success 200 {object} utils.APIResponse{data=[]models.File}
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/files [get]
func (h *Handler) DashboardList(c *fiber.Ctx) error {
	countryID, _ := c.Locals("country_id").(database.CountryID)
	pag := utils.GetPagination(c)

	fileType := c.Query("type")
	articleID := c.Query("article_id")

	fileList, total, err := h.svc.List(countryID, fileType, articleID, pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Paginated(c, "success", fileList, pag.BuildMeta(total))
}

// DashboardShow returns a single file (flat, no parent join)
// @Summary Get File (Dashboard)
// @Description Get metadata for a specific file by ID
// @Tags Files
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "File ID"
// @Success 200 {object} utils.APIResponse{data=models.File}
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/files/{id} [get]
func (h *Handler) DashboardShow(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}
	countryID, _ := c.Locals("country_id").(database.CountryID)
	file, err := h.svc.FindByID(countryID, id)
	if err != nil {
		if err == services.ErrNotFound {
			return utils.NotFound(c)
		}
		return utils.InternalError(c)
	}
	return utils.Success(c, "success", file)
}

// DashboardUpload uploads a file and creates a record
// @Summary Upload File (Dashboard)
// @Description Upload a file and attach it to an article or post, or upload it independently
// @Tags Files
// @Accept mpfd
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param file formData file true "File to upload"
// @Param article_id formData int false "Associated Article ID"
// @Param post_id formData int false "Associated Post ID"
// @Param file_name formData string false "Custom file name"
// @Param file_category formData string false "Category (e.g. worksheet, exam, attachment)"
// @Success 201 {object} utils.APIResponse{data=models.File}
// @Failure 400 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/files [post]
func (h *Handler) DashboardUpload(c *fiber.Ctx) error {
	uploadedFile, err := c.FormFile("file")
	if err != nil {
		return utils.BadRequest(c, "الملف مطلوب")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	// Build the storage subdirectory to mirror the old Laravel layout:
	//   post files  → files/posts/
	//   article files → files/{grade-slug}/{category-slug}/
	var subdir string
	switch {
	case c.FormValue("post_id") != "":
		subdir = "files/posts"
	case c.FormValue("article_id") != "":
		aid, _ := strconv.ParseUint(c.FormValue("article_id"), 10, 64)
		gradeSlug := slugify(articleGradeName(countryID, uint(aid)))
		categorySlug := slugify(c.FormValue("file_category"))
		switch {
		case gradeSlug != "" && categorySlug != "":
			subdir = "files/" + gradeSlug + "/" + categorySlug
		case gradeSlug != "":
			subdir = "files/" + gradeSlug
		default:
			subdir = "files"
		}
	default:
		subdir = "files"
	}

	var uploaded *services.UploadedFile

	// Try as document first, fallback to image
	uploaded, err = h.svc.UploadDocument(uploadedFile, subdir)
	if err != nil {
		uploaded, err = h.svc.UploadImage(uploadedFile, subdir)
		if err != nil {
			return utils.BadRequest(c, err.Error())
		}
	}

	var articleIDPtr *uint
	if articleID := c.FormValue("article_id"); articleID != "" {
		if id, err := strconv.ParseUint(articleID, 10, 64); err == nil {
			uid := uint(id)
			articleIDPtr = &uid
		}
	}

	var postIDPtr *uint
	if postID := c.FormValue("post_id"); postID != "" {
		if id, err := strconv.ParseUint(postID, 10, 64); err == nil {
			uid := uint(id)
			postIDPtr = &uid
		}
	}

	var fileNamePtr *string
	if fn := c.FormValue("file_name"); fn != "" {
		fileNamePtr = &fn
	}

	var fileCatPtr *string
	if fc := c.FormValue("file_category"); fc != "" {
		fileCatPtr = &fc
	}

	file, err := h.svc.CreateRecord(countryID, uploaded, articleIDPtr, postIDPtr, fileNamePtr, fileCatPtr)
	if err != nil {
		return utils.InternalError(c, "فشل حفظ بيانات الملف")
	}

	return utils.Created(c, "تم رفع الملف بنجاح", file)
}

// DashboardUpdate updates file metadata
// @Summary Update File Metadata (Dashboard)
// @Description Update the metadata (name, category, association) of a file
// @Tags Files
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "File ID"
// @Param request body services.UpdateFileInput true "File metadata update payload"
// @Success 200 {object} utils.APIResponse{data=models.File}
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/files/{id} [put]
func (h *Handler) DashboardUpdate(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	var req services.UpdateFileInput
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	file, err := h.svc.UpdateRecord(countryID, id, &req)
	if err != nil {
		if err == services.ErrNotFound {
			return utils.NotFound(c)
		}
		return utils.InternalError(c, "فشل تحديث الملف")
	}

	return utils.Success(c, "تم تحديث الملف بنجاح", file)
}

// DashboardDelete deletes a file
// @Summary Delete File (Dashboard)
// @Description Delete a file record and remove the file from storage
// @Tags Files
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "File ID"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/files/{id} [delete]
func (h *Handler) DashboardDelete(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	if err := h.svc.DeleteRecord(countryID, id); err != nil {
		if err == services.ErrNotFound {
			return utils.NotFound(c)
		}
		return utils.InternalError(c, "فشل حذف الملف")
	}

	return utils.Success(c, "تم حذف الملف بنجاح", nil)
}

// DashboardDownload serves a file for download
// @Summary Download File (Dashboard)
// @Description Direct download of a file via dashboard
// @Tags Files
// @Produce application/octet-stream
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "File ID"
// @Success 200 {file} file "Binary File"
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/files/{id}/download [get]
func (h *Handler) DashboardDownload(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	file, err := h.svc.FindByID(countryID, id)
	if err != nil {
		return utils.NotFound(c)
	}

	// Use SafeGetAbsPath to reject any DB-stored path that escapes the storage root
	absPath, err := h.svc.SafeGetAbsPath(file.FilePath)
	if err != nil {
		return utils.InternalError(c, "مسار الملف غير صالح")
	}

	c.Set("Content-Disposition", "attachment; filename=\""+file.FileName+"\"")
	c.Set("Content-Type", file.MimeType)
	return c.SendFile(absPath)
}

// SecureUploadImage uploads an image securely (with extra validation)
// @Summary Secure Image Upload
// @Description Securely upload an image (e.g. from authenticated dashboard)
// @Tags Files
// @Accept mpfd
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param image formData file true "Image file to upload"
// @Success 201 {object} utils.APIResponse{data=services.UploadResponse}
// @Failure 400 {object} utils.APIResponse
// @Router /dashboard/secure/upload-image [post]
func (h *Handler) SecureUploadImage(c *fiber.Ctx) error {
	return h.UploadImage(c)
}

// SecureUploadDocument uploads a document securely
// @Summary Secure Document Upload
// @Description Securely upload a document
// @Tags Files
// @Accept mpfd
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param file formData file true "Document file to upload"
// @Success 201 {object} utils.APIResponse{data=services.UploadResponse}
// @Failure 400 {object} utils.APIResponse
// @Router /dashboard/secure/upload-document [post]
func (h *Handler) SecureUploadDocument(c *fiber.Ctx) error {
	return h.UploadDocument(c)
}

// SecureView serves a file securely
// @Summary Secure File View
// @Description Serve a file securely using its relative path
// @Tags Files
// @Produce application/octet-stream
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param path query string true "Relative file path"
// @Success 200 {file} file "Binary File"
// @Failure 400 {object} utils.APIResponse
// @Router /secure/view [get]
func (h *Handler) SecureView(c *fiber.Ctx) error {
	relPath := c.Query("path")
	if relPath == "" {
		return utils.BadRequest(c, "مسار الملف مطلوب")
	}

	absPath, err := h.svc.SafeGetAbsPath(relPath)
	if err != nil {
		return utils.BadRequest(c, "مسار الملف غير صالح")
	}
	return c.SendFile(absPath)
}
