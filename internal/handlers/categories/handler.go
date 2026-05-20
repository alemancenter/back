package categories

import (
	"strconv"

	"github.com/alemancenter/fiber-api/internal/database"
	_ "github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/repositories"
	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
)

// Handler contains categories route handlers
type Handler struct {
	svc services.CategoryService
}

// New creates a new categories Handler
func New(svc services.CategoryService) *Handler {
	return &Handler{svc: svc}
}

// List returns all active categories (cached per country).
// @Summary List Active Categories
// @Description Returns all active categories for the specified country
// @Tags Categories
// @Produce json
// @Param X-Country-Id header string false "Country ID"
// @Success 200 {object} utils.APIResponse{data=[]models.Category}
// @Failure 500 {object} utils.APIResponse
// @Router /categories [get]
func (h *Handler) List(c *fiber.Ctx) error {
	countryID, _ := c.Locals("country_id").(database.CountryID)

	cats, err := h.svc.GetActiveCategories(countryID)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "success", cats)
}

// Show returns a single category
// @Summary Get Category
// @Description Get a category by its ID
// @Tags Categories
// @Produce json
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Category ID"
// @Success 200 {object} utils.APIResponse{data=models.Category}
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /categories/{id} [get]
func (h *Handler) Show(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	category, err := h.svc.GetByID(countryID, id)
	if err != nil {
		if err == services.ErrNotFound {
			return utils.NotFound(c)
		}
		return utils.InternalError(c)
	}

	return utils.Success(c, "success", category)
}

// DashboardList returns all categories for dashboard management
// @Summary List Categories (Dashboard)
// @Description Returns a paginated list of categories for dashboard management
// @Tags Categories
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param search query string false "Search query"
// @Param is_active query string false "Filter by active status (true/false)"
// @Param page query int false "Page number"
// @Param limit query int false "Items per page"
// @Success 200 {object} utils.APIResponse{data=[]models.Category}
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/categories [get]
func (h *Handler) DashboardList(c *fiber.Ctx) error {
	countryID, _ := c.Locals("country_id").(database.CountryID)
	pag := utils.GetPagination(c)

	search := c.Query("search")
	isActiveStr := c.Query("is_active")

	categoryList, total, err := h.svc.ListDashboard(countryID, search, isActiveStr, pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Paginated(c, "success", categoryList, pag.BuildMeta(total))
}

// DashboardShow returns a single category (dashboard)
// @Summary Get Category (Dashboard)
// @Description Get a single category for dashboard viewing/editing
// @Tags Categories
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Category ID"
// @Success 200 {object} utils.APIResponse{data=models.Category}
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/categories/{id} [get]
func (h *Handler) DashboardShow(c *fiber.Ctx) error {
	return h.Show(c)
}

// DashboardCreate creates a new category
// @Summary Create Category
// @Description Create a new category
// @Tags Categories
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param request body services.CreateCategoryRequest true "Category data"
// @Success 201 {object} utils.APIResponse{data=models.Category}
// @Failure 400 {object} utils.APIResponse
// @Failure 422 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/categories [post]
func (h *Handler) DashboardCreate(c *fiber.Ctx) error {
	var req services.CreateCategoryRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}
	if errs := utils.Validate(req); errs != nil {
		return utils.ValidationError(c, errs)
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	category, err := h.svc.Create(countryID, &req)
	if err != nil {
		return utils.InternalError(c, "فشل إنشاء التصنيف")
	}

	return utils.Created(c, "تم إنشاء التصنيف بنجاح", category)
}

// DashboardUpdate updates a category
// @Summary Update Category
// @Description Update an existing category
// @Tags Categories
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Category ID"
// @Param request body services.UpdateCategoryRequest true "Category data"
// @Success 200 {object} utils.APIResponse{data=models.Category}
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/categories/{id} [put]
func (h *Handler) DashboardUpdate(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	var req services.UpdateCategoryRequest
	c.BodyParser(&req)

	category, err := h.svc.Update(countryID, id, &req)
	if err != nil {
		if err == services.ErrNotFound {
			return utils.NotFound(c)
		}
		return utils.InternalError(c, "فشل تحديث التصنيف")
	}

	return utils.Success(c, "تم تحديث التصنيف بنجاح", category)
}

// DashboardDelete deletes a category
// @Summary Delete Category
// @Description Delete a category by ID
// @Tags Categories
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Category ID"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/categories/{id} [delete]
func (h *Handler) DashboardDelete(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	if err := h.svc.Delete(countryID, id); err != nil {
		return utils.InternalError(c, "فشل حذف التصنيف")
	}

	return utils.Success(c, "تم حذف التصنيف بنجاح", nil)
}

// DashboardUploadImages uploads image and/or icon_image for a category.
// Accepts multipart/form-data with fields "image" and/or "icon_image".
// Files are stored under category_images/ and the category record is updated.
func (h *Handler) DashboardUploadImages(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	fileRepo := repositories.NewFileRepository()
	fileSvc := services.NewFileService(fileRepo)

	req := &services.UpdateCategoryRequest{}

	if img, err := c.FormFile("image"); err == nil {
		uploaded, err := fileSvc.UploadImage(img, "category_images")
		if err != nil {
			return utils.BadRequest(c, "فشل رفع الصورة: "+err.Error())
		}
		req.Image = uploaded.Path
	}

	if iconImg, err := c.FormFile("icon_image"); err == nil {
		uploaded, err := fileSvc.UploadImage(iconImg, "category_images")
		if err != nil {
			return utils.BadRequest(c, "فشل رفع الأيقونة: "+err.Error())
		}
		req.IconImage = uploaded.Path
	}

	if req.Image == "" && req.IconImage == "" {
		return utils.BadRequest(c, "لم يتم تقديم أي صورة")
	}

	category, err := h.svc.Update(countryID, id, req)
	if err != nil {
		if err == services.ErrNotFound {
			return utils.NotFound(c)
		}
		return utils.InternalError(c, "فشل تحديث صور الفئة")
	}

	return utils.Success(c, "تم رفع الصور بنجاح", category)
}

// DashboardToggleStatus toggles category active status
// @Summary Toggle Category Status
// @Description Toggle the is_active status of a category
// @Tags Categories
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Category ID"
// @Success 200 {object} utils.APIResponse{data=models.Category}
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/categories/{id}/toggle [post]
func (h *Handler) DashboardToggleStatus(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	category, err := h.svc.GetByID(countryID, id)
	if err != nil {
		return utils.NotFound(c)
	}

	newStatus := !category.IsActive
	updatedCategory, err := h.svc.Update(countryID, id, &services.UpdateCategoryRequest{
		IsActive: &newStatus,
	})
	if err != nil {
		return utils.InternalError(c, "فشل تحديث حالة التصنيف")
	}

	return utils.Success(c, "تم تحديث حالة التصنيف", updatedCategory)
}
