package sitemap

import (
	"strings"

	"github.com/alemancenter/fiber-api/internal/services"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
)

// Handler contains sitemap route handlers
type Handler struct {
	svc services.SitemapService
}

// New creates a new sitemap Handler
func New(svc services.SitemapService) *Handler {
	return &Handler{svc: svc}
}

// Status returns existence and last-modified time for each sitemap type.
// GET /api/dashboard/sitemap/status?database=jo
func (h *Handler) Status(c *fiber.Ctx) error {
	dbCode := c.Query("database", "jo")

	result := h.svc.GetStatus(dbCode)

	return utils.Success(c, "success", services.SitemapStatusResponse{
		Database: dbCode,
		Sitemaps: result,
	})
}

// GenerateAll generates all sitemap types for the given database.
// POST /api/dashboard/sitemap/generate
func (h *Handler) GenerateAll(c *fiber.Ctx) error {
	type Request struct {
		Database string `json:"database"`
	}
	var req Request
	c.BodyParser(&req)
	if req.Database == "" {
		req.Database = "jo"
	}

	validDBs := map[string]bool{"jo": true, "sa": true, "eg": true, "ps": true}
	if !validDBs[req.Database] {
		return utils.BadRequest(c, "قاعدة بيانات غير صحيحة: "+req.Database)
	}

	errs := h.svc.GenerateAll(req.Database)

	var failed []string
	for _, e := range errs {
		if e != nil {
			failed = append(failed, e.Error())
		}
	}
	if len(failed) > 0 {
		return utils.InternalError(c, strings.Join(failed, "; "))
	}

	return utils.Success(c, "تم توليد خرائط الموقع بنجاح", nil)
}

// Delete removes a specific sitemap file.
// DELETE /api/dashboard/sitemap/delete/:type/:database
func (h *Handler) Delete(c *fiber.Ctx) error {
	sitemapType := c.Params("type")
	dbCode := c.Params("database")

	allowed := map[string]bool{"articles": true, "post": true, "static": true, "index": true}
	if !allowed[sitemapType] {
		return utils.BadRequest(c, "نوع خريطة موقع غير صحيح")
	}

	if err := h.svc.Delete(sitemapType, dbCode); err != nil {
		return utils.InternalError(c, "فشل حذف الملف")
	}

	return utils.Success(c, "تم حذف خريطة الموقع بنجاح", nil)
}
