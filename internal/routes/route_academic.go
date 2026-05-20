package routes

import (
	"github.com/alemancenter/fiber-api/internal/middleware"
	"github.com/gofiber/fiber/v2"
)

// registerAcademicRoutes handles all routes related to academic structure:
// School Classes, Grades, Subjects, Semesters, and Filter metadata.
func registerAcademicRoutes(public, dash fiber.Router, h *Handlers) {
	// =====================
	// PUBLIC ROUTES
	// =====================

	// School classes & filter (Aliases to Grades)
	public.Get("/school-classes", h.Grades.ListSchoolClasses)
	public.Get("/school-classes/:id", h.Grades.GetSchoolClass)
	public.Get("/filter", h.Grades.FilterMeta)
	public.Get("/filter/subjects/:classId", h.Grades.ListSubjects)
	public.Get("/filter/semesters/:subjectId", h.Grades.ListSemesters)

	// Grades (Aliases to School Classes - maintained for frontend backward compatibility)
	// Important: Static/specific routes must come before dynamic /:id routes
	public.Get("/grades", h.Grades.ListSchoolClasses)
	public.Get("/grades/subjects/:classId", h.Grades.ListSubjects)
	public.Get("/grades/articles/:id", h.Grades.GetGradeArticles)
	public.Get("/grades/files/:id/download", h.Grades.DownloadGradeFile)
	public.Get("/grades/:id", h.Grades.GetSchoolClass)

	// =====================
	// ADMIN DASHBOARD ROUTES
	// =====================

	// School classes
	dashClasses := dash.Group("/school-classes", middleware.Can("manage classes"))
	dashClasses.Get("", h.Grades.DashboardListSchoolClasses)
	dashClasses.Post("", h.Grades.DashboardCreateSchoolClass)
	dashClasses.Get("/:id", h.Grades.GetSchoolClass)
	dashClasses.Put("/:id", h.Grades.DashboardUpdateSchoolClass)
	dashClasses.Delete("/:id", h.Grades.DashboardDeleteSchoolClass)

	// Semesters
	dashSemesters := dash.Group("/semesters", middleware.Can("manage semesters"))
	dashSemesters.Get("", h.Grades.DashboardListSemesters)
	dashSemesters.Post("", h.Grades.DashboardCreateSemester)
	dashSemesters.Get("/:id", h.Grades.DashboardGetSemester)
	dashSemesters.Put("/:id", h.Grades.DashboardUpdateSemester)
	dashSemesters.Delete("/:id", h.Grades.DashboardDeleteSemester)

	// Subjects
	dashSubjects := dash.Group("/subjects", middleware.Can("manage subjects"))
	dashSubjects.Get("", h.Grades.DashboardListSubjects)
	dashSubjects.Post("", h.Grades.DashboardCreateSubject)
	dashSubjects.Get("/:id", h.Grades.DashboardGetSubject)
	dashSubjects.Put("/:id", h.Grades.DashboardUpdateSubject)
	dashSubjects.Delete("/:id", h.Grades.DashboardDeleteSubject)
}
