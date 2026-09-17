// Package contentgen is a narrowly-scoped AI writing assistant for article/post creation and
// editing — reintroduced at the site owner's explicit request after the previous, much broader
// AI subsystem was removed for shipping thin/duplicate content that got the site rejected by
// AdSense. This one is deliberately constrained: always scoped to the exact title +
// grade/subject/semester (articles) or category (posts) the admin has already selected, a
// ~500-word target, an attachment's filename only (never its content — parsing DOCX/PDF text
// was deleted along with the old subsystem), and a same-project duplicate-content check before
// the draft is ever shown. A second, separate pass then suggests SEO metadata (seo_title,
// meta_description, focus_keyword, etc.) scoped to that same title and finished content, scored
// against the same AnalyzeSEO the manual "تحليل الآن" button uses and retried until it clears an
// 85% floor or attempts run out (services.ContentDraftService.generateSEODraft). It only ever
// fills the editor and the SEO panel with an editable draft; saving/publishing stays a fully
// manual, separate step exactly like typing the content by hand.
package contentgen

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/services"
	"github.com/imanjo/fiber-api/internal/utils"
)

type Handler struct {
	svc services.ContentDraftService
}

func New(svc services.ContentDraftService) *Handler {
	return &Handler{svc: svc}
}

type draftRequest struct {
	ContentType    string `json:"content_type"`
	Title          string `json:"title"`
	GradeLevel     string `json:"grade_level"`
	SubjectName    string `json:"subject_name"`
	SemesterName   string `json:"semester_name"`
	CategoryName   string `json:"category_name"`
	AttachmentName string `json:"attachment_name"`
}

// GenerateDraft generates an AI-assisted content draft, scoped to the given title and
// classification, checked against existing site content for duplication before being returned.
// @Summary Generate an AI-assisted article/post content draft
// @Tags Content Generation
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 503 {object} utils.APIResponse
// @Router /dashboard/content-gen/draft [post]
func (h *Handler) GenerateDraft(c *fiber.Ctx) error {
	var req draftRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	req.ContentType = strings.ToLower(strings.TrimSpace(req.ContentType))
	if req.ContentType != "article" && req.ContentType != "post" {
		return utils.BadRequest(c, "نوع المحتوى غير صحيح")
	}
	if strings.TrimSpace(req.Title) == "" {
		return utils.BadRequest(c, "العنوان مطلوب قبل التوليد")
	}
	if req.ContentType == "article" && (strings.TrimSpace(req.SubjectName) == "" || strings.TrimSpace(req.SemesterName) == "") {
		return utils.BadRequest(c, "يرجى اختيار الصف والمادة والفصل الدراسي قبل التوليد")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)
	if countryID == 0 {
		countryID = database.CountryJordan
	}

	result, err := h.svc.GenerateDraft(c.Context(), services.ContentDraftRequest{
		ContentType:    req.ContentType,
		Title:          req.Title,
		GradeLevel:     req.GradeLevel,
		SubjectName:    req.SubjectName,
		SemesterName:   req.SemesterName,
		CategoryName:   req.CategoryName,
		AttachmentName: req.AttachmentName,
		CountryID:      countryID,
	})
	if err != nil {
		return utils.BadRequest(c, err.Error())
	}

	return utils.Success(c, "success", result)
}

type fixRequest struct {
	ContentType string `json:"content_type"`
	ID          uint64 `json:"id"`
}

// FixContent fixes one already-published article/post's currently-detected AdSense
// content-policy problem(s) — the same ones surfaced by the /dashboard/adsense-policy scan
// (thin/medium content, near-duplicate content, corrupted template artifacts, a too-short
// title, or a missing/short meta description) — and returns an editable draft for the admin to
// review inside the normal edit page. Never saves anything itself.
// @Summary Fix an article/post's detected AdSense content-policy problem with AI
// @Tags Content Generation
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Success 200 {object} utils.APIResponse
// @Failure 400 {object} utils.APIResponse
// @Failure 503 {object} utils.APIResponse
// @Router /dashboard/content-gen/fix [post]
func (h *Handler) FixContent(c *fiber.Ctx) error {
	var req fixRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}

	req.ContentType = strings.ToLower(strings.TrimSpace(req.ContentType))
	if req.ContentType != "article" && req.ContentType != "post" {
		return utils.BadRequest(c, "نوع المحتوى غير صحيح")
	}
	if req.ID == 0 {
		return utils.BadRequest(c, "معرف العنصر مطلوب")
	}

	countryID, _ := c.Locals("country_id").(database.CountryID)
	if countryID == 0 {
		countryID = database.CountryJordan
	}

	result, err := h.svc.FixPolicyContent(c.Context(), services.PolicyFixRequest{
		ContentType: req.ContentType,
		ID:          req.ID,
		CountryID:   countryID,
	})
	if err != nil {
		return utils.BadRequest(c, err.Error())
	}

	return utils.Success(c, "success", result)
}
