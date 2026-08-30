package searchconsole

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/imanjo/fiber-api/internal/models"
	"github.com/imanjo/fiber-api/internal/repositories"
	svc "github.com/imanjo/fiber-api/internal/services/searchconsole"
	"github.com/imanjo/fiber-api/internal/utils"
)

// Handler exposes the Google Search Console integration described in
// back/docs/reports/CONTENT_QUALITY_GOVERNANCE_CENTER_PLAN.md §4.4. svc may be
// nil when GSC isn't configured (GSC_ENABLED unset or no service account
// key) — every method checks for that and returns a clear message instead of
// panicking, so the feature is cleanly absent rather than half-broken.
type Handler struct {
	repo repositories.GSCRepository
	svc  *svc.Service
}

func New(repo repositories.GSCRepository, service *svc.Service) *Handler {
	return &Handler{repo: repo, svc: service}
}

// ListProperties returns the configured country -> Search Console property map.
func (h *Handler) ListProperties(c *fiber.Ctx) error {
	properties, err := h.repo.ListProperties(c.Context())
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "خصائص Search Console", properties)
}

type upsertPropertyRequest struct {
	SiteURL string `json:"site_url"`
	Active  *bool  `json:"active"`
}

var gscDomainPropertyPattern = regexp.MustCompile(`^sc-domain:[A-Za-z0-9.-]+$`)

func validGSCProperty(value string) bool {
	if gscDomainPropertyPattern.MatchString(value) {
		return true
	}
	parsed, err := url.ParseRequestURI(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

// UpsertProperty sets the site_url Search Console property for one country.
// The one-time step of adding the service account as a user on that property
// in the Search Console UI still has to happen outside this app — see plan §4.1.
func (h *Handler) UpsertProperty(c *fiber.Ctx) error {
	countryCode := strings.TrimSpace(c.Params("country_code"))
	if countryCode != "jo" && countryCode != "sa" && countryCode != "eg" && countryCode != "ps" {
		return utils.BadRequest(c, "رمز الدولة مطلوب")
	}
	var req upsertPropertyRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}
	siteURL := strings.TrimSpace(req.SiteURL)
	if !validGSCProperty(siteURL) {
		return utils.BadRequest(c, "خاصية Search Console غير صالحة")
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	property := &models.GSCProperty{CountryCode: countryCode, SiteURL: siteURL, Active: active}
	if err := h.repo.UpsertProperty(c.Context(), property); err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "تم حفظ الخاصية", property)
}

// Status returns the last stored (cached) Google index status for one content
// item — never a live call, so this stays fast and free of quota cost. Call
// Sync first if a fresher check is needed.
func (h *Handler) Status(c *fiber.Ctx) error {
	contentType := strings.TrimSpace(c.Params("content_type"))
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "معرف غير صحيح")
	}
	countryCode := strings.TrimSpace(c.Query("country_code"))
	if countryCode == "" {
		return utils.BadRequest(c, "country_code مطلوب")
	}
	status, err := h.repo.GetURLStatus(c.Context(), contentType, uint(id), countryCode)
	if err != nil {
		return utils.NotFound(c, "لا توجد بيانات من Google لهذا الرابط بعد")
	}
	return utils.Success(c, "حالة الفهرسة في Google", status)
}

// TestConnection makes one live, synchronous URL Inspection call and returns
// the raw result — lets an admin verify the service account + property are
// wired correctly from the dashboard itself, without waiting on a background
// sync or reading server logs.
func (h *Handler) TestConnection(c *fiber.Ctx) error {
	if h.svc == nil {
		return utils.BadRequest(c, "لم يتم تفعيل ربط Google Search Console بعد (GSC_ENABLED أو مفتاح الحساب الخدمي غير مضبوطين على الخادم)")
	}
	countryCode := strings.TrimSpace(c.Query("country_code"))
	url := strings.TrimSpace(c.Query("url"))
	if countryCode == "" || url == "" {
		return utils.BadRequest(c, "country_code وurl مطلوبان")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := h.svc.TestInspect(ctx, countryCode, url)
	if err != nil {
		if errors.Is(err, svc.ErrNotConfigured) {
			return utils.BadRequest(c, "لم يتم تفعيل ربط Google Search Console بعد")
		}
		return utils.InternalError(c, "تعذّر الاتصال بـ Google: "+err.Error())
	}

	return utils.Success(c, "تم الاتصال بـ Google Search Console بنجاح", fiber.Map{
		"index_status":     result.IndexStatus,
		"coverage_state":   result.CoverageState,
		"verdict":          result.Verdict,
		"robots_txt_state": result.RobotsTxtState,
	})
}

type syncTargetRequest struct {
	ContentType string `json:"content_type"`
	ContentID   uint   `json:"content_id"`
	URL         string `json:"url"`
}

type syncBatchRequest struct {
	CountryCode string              `json:"country_code"`
	Targets     []syncTargetRequest `json:"targets"`
}

// Sync starts a background URL Inspection sync for an explicit list of
// targets. Targets are supplied by the caller (e.g. the readiness dashboard's
// current selection) rather than discovered here, so prioritization stays in
// the layer that already knows what's newly published or newly ready — see
// plan §4.3.
func (h *Handler) Sync(c *fiber.Ctx) error {
	if h.svc == nil {
		return utils.BadRequest(c, "لم يتم تفعيل ربط Google Search Console بعد")
	}
	var req syncBatchRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "بيانات غير صحيحة")
	}
	countryCode := strings.TrimSpace(req.CountryCode)
	if countryCode == "" || len(req.Targets) == 0 {
		return utils.BadRequest(c, "country_code وقائمة الأهداف مطلوبة")
	}

	targets := make([]svc.Target, 0, len(req.Targets))
	for _, t := range req.Targets {
		if strings.TrimSpace(t.URL) == "" {
			continue
		}
		targets = append(targets, svc.Target{
			ContentType: t.ContentType,
			ContentID:   t.ContentID,
			CountryCode: countryCode,
			URL:         t.URL,
		})
	}
	if len(targets) == 0 {
		return utils.BadRequest(c, "لا توجد روابط صالحة في قائمة الأهداف")
	}

	run, err := h.svc.SyncBatch(c.Context(), countryCode, targets, models.GSCSyncTriggerManual)
	if err != nil {
		return h.syncStartError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(utils.APIResponse{Success: true, Message: "بدأت مزامنة حالة الفهرسة", Data: run})
}

// SyncAnalytics starts a bounded background Search Analytics sync. The
// dashboard defaults to 90 days so its ranking views are useful immediately.
func (h *Handler) SyncAnalytics(c *fiber.Ctx) error {
	if h.svc == nil {
		return utils.BadRequest(c, "لم يتم تفعيل ربط Google Search Console بعد")
	}
	countryCode := strings.TrimSpace(c.Query("country_code"))
	if countryCode == "" {
		return utils.BadRequest(c, "country_code مطلوب")
	}
	days := c.QueryInt("days", 90)
	if days <= 0 || days > 400 {
		days = 90
	}
	run, err := h.svc.SyncSearchAnalytics(c.Context(), countryCode, days)
	if err != nil {
		return h.syncStartError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(utils.APIResponse{Success: true, Message: "بدأت مزامنة بيانات الأداء", Data: run})
}

func (h *Handler) syncStartError(c *fiber.Ctx, err error) error {
	if errors.Is(err, svc.ErrAlreadyRunning) {
		return c.Status(fiber.StatusConflict).JSON(utils.APIResponse{Success: false, Message: "مزامنة أخرى قيد التشغيل بالفعل لهذه الدولة"})
	}
	if errors.Is(err, svc.ErrNotConfigured) {
		return utils.BadRequest(c, "لم يتم تفعيل ربط Google Search Console بعد")
	}
	return utils.InternalError(c)
}

// Analytics returns stored Search Analytics rows for one country, optionally
// filtered to a single URL, for the trailing `days` (default 30).
func (h *Handler) Analytics(c *fiber.Ctx) error {
	countryCode := strings.TrimSpace(c.Query("country_code"))
	if countryCode == "" {
		return utils.BadRequest(c, "country_code مطلوب")
	}
	url := strings.TrimSpace(c.Query("url"))
	days := c.QueryInt("days", 30)
	if days <= 0 || days > 400 {
		days = 30
	}
	since := time.Now().AddDate(0, 0, -days)

	rows, err := h.repo.ListAnalytics(c.Context(), countryCode, url, since)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Success(c, "بيانات الأداء في بحث Google", rows)
}

// Keywords returns aggregated real Search Console queries and average
// positions. It is a report over stored data, not a synthetic rank estimate.
func (h *Handler) Keywords(c *fiber.Ctx) error {
	countryCode := strings.TrimSpace(c.Query("country_code"))
	if countryCode == "" {
		return utils.BadRequest(c, "country_code مطلوب")
	}
	days := c.QueryInt("days", 30)
	if days <= 0 || days > 400 {
		days = 30
	}
	pag := utils.GetPagination(c)
	since := time.Now().UTC().AddDate(0, 0, -days)
	since = time.Date(since.Year(), since.Month(), since.Day(), 0, 0, 0, 0, time.UTC)
	rows, total, err := h.repo.ListKeywordAnalytics(c.Context(), countryCode, strings.TrimSpace(c.Query("q")), since, pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c)
	}
	return utils.Paginated(c, "عبارات البحث والترتيب", rows, pag.BuildMeta(total))
}
