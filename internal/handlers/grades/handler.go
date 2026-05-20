package grades

import (
	"strconv"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	_ "github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
)

const (
	classesAndFilterTTL = 24 * time.Hour
)

// Handler handles school classes, subjects, semesters, and grade-based content
type Handler struct {
	svc     services.GradeService
	fileSvc *services.FileService
}

// New creates a new grades Handler
func New(svc services.GradeService, fileSvc *services.FileService) *Handler {
	return &Handler{
		svc:     svc,
		fileSvc: fileSvc,
	}
}

// ListSchoolClasses returns all school classes.
// Result is cached per country for 1 hour — this data changes only via admin dashboard.
// @Summary List School Classes
// @Description Returns all school classes for the specified country (Cached for 1 hour)
// @Tags Grades
// @Produce json
// @Param X-Country-Id header string false "Country ID"
// @Success 200 {object} utils.APIResponse{data=[]models.SchoolClass}
// @Failure 500 {object} utils.APIResponse
// @Router /school-classes [get]
func (h *Handler) ListSchoolClasses(c *fiber.Ctx) error {
	countryID, _ := c.Locals("country_id").(database.CountryID)

	classes, err := h.svc.ListSchoolClasses(countryID)
	if err != nil {
		return utils.InternalError(c)
	}
	c.Set("Cache-Control", "public, max-age=86400, stale-while-revalidate=3600")
	return utils.Success(c, "success", classes)
}

// GetSchoolClass returns a single school class with its subjects
// @Summary Get School Class
// @Description Get a single school class by ID along with its subjects
// @Tags Grades
// @Produce json
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Class ID"
// @Success 200 {object} utils.APIResponse{data=models.SchoolClass}
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /school-classes/{id} [get]
func (h *Handler) GetSchoolClass(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	class, err := h.svc.GetSchoolClass(countryID, id)
	if err != nil {
		if err == services.ErrNotFound {
			return utils.NotFound(c)
		}
		return utils.InternalError(c)
	}

	c.Set("Cache-Control", "public, max-age=86400, stale-while-revalidate=3600")
	return utils.Success(c, "success", class)
}

// ListSubjects returns subjects for a class
// @Summary List Subjects for a Class
// @Description Get a list of subjects associated with a specific school class
// @Tags Grades
// @Produce json
// @Param X-Country-Id header string false "Country ID"
// @Param classId path int true "Class ID"
// @Success 200 {object} utils.APIResponse{data=[]models.Subject}
// @Failure 400 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /filter/subjects/{classId} [get]
func (h *Handler) ListSubjects(c *fiber.Ctx) error {
	classID, err := strconv.ParseUint(c.Params("classId"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	subjects, err := h.svc.ListSubjects(countryID, classID)
	if err != nil {
		return utils.InternalError(c)
	}
	c.Set("Cache-Control", "public, max-age=86400, stale-while-revalidate=3600")
	return utils.Success(c, "success", subjects)
}

// ListSemesters returns semesters for a subject's grade level
// @Summary List Semesters for a Subject
// @Description Get a list of semesters associated with the grade level of a specific subject
// @Tags Grades
// @Produce json
// @Param X-Country-Id header string false "Country ID"
// @Param subjectId path int true "Subject ID"
// @Success 200 {object} utils.APIResponse{data=services.SemestersResponse}
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /filter/semesters/{subjectId} [get]
func (h *Handler) ListSemesters(c *fiber.Ctx) error {
	subjectID, err := strconv.ParseUint(c.Params("subjectId"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	semesters, subject, err := h.svc.ListSemesters(countryID, subjectID)
	if err != nil {
		if subject == nil {
			return utils.NotFound(c)
		}
		return utils.InternalError(c)
	}

	c.Set("Cache-Control", "public, max-age=86400, stale-while-revalidate=3600")
	return utils.Success(c, "success", services.SemestersResponse{
		Subject:   subject,
		ClassID:   subject.GradeLevel,
		Semesters: semesters,
	})
}

// FilterMeta returns top-level filter metadata (cached per country).
// @Summary Get Filter Metadata
// @Description Returns top-level metadata including school classes to populate frontend filters
// @Tags Grades
// @Produce json
// @Param X-Country-Id header string false "Country ID"
// @Success 200 {object} utils.APIResponse{data=services.FilterMetaResponse}
// @Failure 500 {object} utils.APIResponse
// @Router /filter [get]
func (h *Handler) FilterMeta(c *fiber.Ctx) error {
	countryID, _ := c.Locals("country_id").(database.CountryID)

	classes, err := h.svc.FilterMeta(countryID)
	if err != nil {
		return utils.InternalError(c)
	}
	c.Set("Cache-Control", "public, max-age=86400, stale-while-revalidate=3600")
	return utils.Success(c, "success", services.FilterMetaResponse{Classes: classes})
}

// GetGradeArticles returns articles for a specific grade
// @Summary List Articles for a Grade
// @Description Get a paginated list of articles associated with a specific school class (grade level)
// @Tags Grades
// @Produce json
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Class ID (Grade Level)"
// @Param page query int false "Page number"
// @Param limit query int false "Items per page"
// @Success 200 {object} utils.APIResponse{data=[]models.Article}
// @Failure 400 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /grades/articles/{id} [get]
func (h *Handler) GetGradeArticles(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)
	pag := utils.GetPagination(c)

	articles, total, err := h.svc.ListGradeArticles(countryID, id, pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Paginated(c, "success", articles, pag.BuildMeta(total))
}

// DownloadGradeFile downloads a file for a grade article
// @Summary Download Grade File
// @Description Direct download of a file attached to a grade article
// @Tags Grades
// @Produce application/octet-stream
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "File ID"
// @Success 200 {file} file "Binary File"
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /grades/files/{id}/download [get]
func (h *Handler) DownloadGradeFile(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	file, err := h.fileSvc.FindByID(countryID, id)
	if err != nil {
		if err == services.ErrNotFound {
			return utils.NotFound(c)
		}
		return utils.InternalError(c)
	}

	absPath := h.fileSvc.GetAbsPath(file.FilePath)

	// Increment view count
	go h.fileSvc.IncrementViewCount(countryID, id)

	c.Set("Content-Disposition", "attachment; filename=\""+file.FileName+"\"")
	c.Set("Content-Type", file.MimeType)
	return c.SendFile(absPath)
}

// ── Dashboard ────────────────────────────────────────────────────────────────

// DashboardListSchoolClasses returns all classes for dashboard
// @Summary List School Classes (Dashboard)
// @Description Get a paginated list of school classes for dashboard management
// @Tags Grades
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param page query int false "Page number"
// @Param limit query int false "Items per page"
// @Success 200 {object} utils.APIResponse{data=[]models.SchoolClass}
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/school-classes [get]
func (h *Handler) DashboardListSchoolClasses(c *fiber.Ctx) error {
	countryID, _ := c.Locals("country_id").(database.CountryID)
	pag := utils.GetPagination(c)

	classes, total, err := h.svc.ListSchoolClassesDashboard(countryID, pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Paginated(c, "success", classes, pag.BuildMeta(total))
}

// DashboardCreateSchoolClass creates a school class
// @Summary Create School Class
// @Description Create a new school class
// @Tags Grades
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param request body services.SchoolClassInput true "School class data"
// @Success 201 {object} utils.APIResponse{data=models.SchoolClass}
// @Failure 400 {object} utils.APIResponse
// @Failure 422 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/school-classes [post]
func (h *Handler) DashboardCreateSchoolClass(c *fiber.Ctx) error {
	var req services.SchoolClassInput
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}
	if errs := utils.Validate(req); errs != nil {
		return utils.ValidationError(c, errs)
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	class, err := h.svc.CreateSchoolClass(countryID, &req)
	if err != nil {
		return utils.InternalError(c, "فشل إنشاء الصف")
	}

	return utils.Created(c, "تم إنشاء الصف بنجاح", class)
}

// DashboardUpdateSchoolClass updates a school class
// @Summary Update School Class
// @Description Update an existing school class
// @Tags Grades
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Class ID"
// @Param request body services.SchoolClassInput true "School class data"
// @Success 200 {object} utils.APIResponse{data=models.SchoolClass}
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/school-classes/{id} [put]
func (h *Handler) DashboardUpdateSchoolClass(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	var req services.SchoolClassInput
	c.BodyParser(&req)

	class, err := h.svc.UpdateSchoolClass(countryID, id, &req)
	if err != nil {
		if err == services.ErrNotFound {
			return utils.NotFound(c)
		}
		return utils.InternalError(c, "فشل تحديث الصف")
	}

	return utils.Success(c, "تم تحديث الصف بنجاح", class)
}

// DashboardDeleteSchoolClass deletes a school class
// @Summary Delete School Class
// @Description Delete a school class by ID
// @Tags Grades
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Class ID"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/school-classes/{id} [delete]
func (h *Handler) DashboardDeleteSchoolClass(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)
	if err := h.svc.DeleteSchoolClass(countryID, id); err != nil {
		return utils.InternalError(c, "فشل الحذف")
	}

	return utils.Success(c, "تم حذف الصف بنجاح", nil)
}

// DashboardListSubjects returns all subjects for dashboard
// @Summary List Subjects (Dashboard)
// @Description Get a paginated list of subjects for dashboard management
// @Tags Grades
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param page query int false "Page number"
// @Param limit query int false "Items per page"
// @Success 200 {object} utils.APIResponse{data=[]models.Subject}
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/subjects [get]
func (h *Handler) DashboardListSubjects(c *fiber.Ctx) error {
	countryID, _ := c.Locals("country_id").(database.CountryID)
	pag := utils.GetPagination(c)

	subjects, total, err := h.svc.ListSubjectsDashboard(countryID, pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Paginated(c, "success", subjects, pag.BuildMeta(total))
}

// DashboardCreateSubject creates a subject
// @Summary Create Subject
// @Description Create a new subject
// @Tags Grades
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param request body services.SubjectInput true "Subject data"
// @Success 201 {object} utils.APIResponse{data=models.Subject}
// @Failure 400 {object} utils.APIResponse
// @Failure 422 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/subjects [post]
func (h *Handler) DashboardCreateSubject(c *fiber.Ctx) error {
	var req services.SubjectInput
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}
	if errs := utils.Validate(req); errs != nil {
		return utils.ValidationError(c, errs)
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	subject, err := h.svc.CreateSubject(countryID, &req)
	if err != nil {
		return utils.InternalError(c, "فشل إنشاء المادة")
	}

	return utils.Created(c, "تم إنشاء المادة بنجاح", subject)
}

// DashboardGetSubject returns a subject
// @Summary Get Subject (Dashboard)
// @Description Get a single subject by ID
// @Tags Grades
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Subject ID"
// @Success 200 {object} utils.APIResponse{data=models.Subject}
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/subjects/{id} [get]
func (h *Handler) DashboardGetSubject(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	subject, err := h.svc.GetSubject(countryID, id)
	if err != nil {
		if err == services.ErrNotFound {
			return utils.NotFound(c)
		}
		return utils.InternalError(c)
	}

	return utils.Success(c, "success", subject)
}

// DashboardUpdateSubject updates a subject
// @Summary Update Subject
// @Description Update an existing subject
// @Tags Grades
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Subject ID"
// @Param request body services.SubjectInput true "Subject data"
// @Success 200 {object} utils.APIResponse{data=models.Subject}
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/subjects/{id} [put]
func (h *Handler) DashboardUpdateSubject(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	var req services.SubjectInput
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}
	if errs := utils.Validate(req); errs != nil {
		return utils.ValidationError(c, errs)
	}

	subject, err := h.svc.UpdateSubject(countryID, id, &req)
	if err != nil {
		if err == services.ErrNotFound {
			return utils.NotFound(c)
		}
		return utils.InternalError(c, "فشل تحديث المادة")
	}

	return utils.Success(c, "تم تحديث المادة بنجاح", subject)
}

// DashboardDeleteSubject deletes a subject
// @Summary Delete Subject
// @Description Delete a subject by ID
// @Tags Grades
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Subject ID"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/subjects/{id} [delete]
func (h *Handler) DashboardDeleteSubject(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)
	if err := h.svc.DeleteSubject(countryID, id); err != nil {
		return utils.InternalError(c, "فشل الحذف")
	}

	return utils.Success(c, "تم حذف المادة بنجاح", nil)
}

// DashboardListSemesters returns all semesters for dashboard
// @Summary List Semesters (Dashboard)
// @Description Get a paginated list of semesters for dashboard management
// @Tags Grades
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param page query int false "Page number"
// @Param limit query int false "Items per page"
// @Success 200 {object} utils.APIResponse{data=[]models.Semester}
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/semesters [get]
func (h *Handler) DashboardListSemesters(c *fiber.Ctx) error {
	countryID, _ := c.Locals("country_id").(database.CountryID)
	pag := utils.GetPagination(c)

	semesters, total, err := h.svc.ListSemestersDashboard(countryID, pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}

	return utils.Paginated(c, "success", semesters, pag.BuildMeta(total))
}

// DashboardCreateSemester creates a semester
// @Summary Create Semester
// @Description Create a new semester
// @Tags Grades
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param request body services.SemesterInput true "Semester data"
// @Success 201 {object} utils.APIResponse{data=models.Semester}
// @Failure 400 {object} utils.APIResponse
// @Failure 422 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/semesters [post]
func (h *Handler) DashboardCreateSemester(c *fiber.Ctx) error {
	var req services.SemesterInput
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	semester, err := h.svc.CreateSemester(countryID, &req)
	if err != nil {
		return utils.InternalError(c, "فشل إنشاء الفصل الدراسي")
	}

	return utils.Created(c, "تم إنشاء الفصل الدراسي بنجاح", semester)
}

// DashboardGetSemester returns a semester
// @Summary Get Semester (Dashboard)
// @Description Get a single semester by ID
// @Tags Grades
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Semester ID"
// @Success 200 {object} utils.APIResponse{data=models.Semester}
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/semesters/{id} [get]
func (h *Handler) DashboardGetSemester(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	semester, err := h.svc.GetSemester(countryID, id)
	if err != nil {
		if err == services.ErrNotFound {
			return utils.NotFound(c)
		}
		return utils.InternalError(c)
	}

	return utils.Success(c, "success", semester)
}

// DashboardUpdateSemester updates a semester
// @Summary Update Semester
// @Description Update an existing semester
// @Tags Grades
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Semester ID"
// @Param request body services.SemesterInput true "Semester data"
// @Success 200 {object} utils.APIResponse{data=models.Semester}
// @Failure 400 {object} utils.APIResponse
// @Failure 404 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/semesters/{id} [put]
func (h *Handler) DashboardUpdateSemester(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	var req services.SemesterInput
	c.BodyParser(&req)

	semester, err := h.svc.UpdateSemester(countryID, id, &req)
	if err != nil {
		if err == services.ErrNotFound {
			return utils.NotFound(c)
		}
		return utils.InternalError(c, "فشل تحديث الفصل الدراسي")
	}

	return utils.Success(c, "تم تحديث الفصل الدراسي بنجاح", semester)
}

// DashboardDeleteSemester deletes a semester
// @Summary Delete Semester
// @Description Delete a semester by ID
// @Tags Grades
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Param id path int true "Semester ID"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 500 {object} utils.APIResponse
// @Router /dashboard/semesters/{id} [delete]
func (h *Handler) DashboardDeleteSemester(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)

	if err := h.svc.DeleteSemester(countryID, id); err != nil {
		return utils.InternalError(c, "فشل الحذف")
	}

	return utils.Success(c, "تم حذف الفصل الدراسي بنجاح", nil)
}
