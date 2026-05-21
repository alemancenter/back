package contentaudit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	auditservice "github.com/alemancenter/fiber-api/internal/services/contentaudit"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/alemancenter/fiber-api/pkg/logger"
	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type Handler struct {
	svc *auditservice.Service
}

func New(svc *auditservice.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Start(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var userID *uint
	if user, ok := c.Locals("user").(*models.User); ok && user != nil {
		id := user.ID
		userID = &id
	}

	run, err := h.svc.Start(ctx, models.PolicyAuditTriggerManual, userID)
	if err != nil {
		if errors.Is(err, auditservice.ErrAlreadyRunning) {
			return utils.BadRequest(c, "content audit is already running")
		}
		return utils.InternalError(c, "failed to start content audit")
	}

	return utils.Created(c, "content audit started", run)
}

func (h *Handler) ListRuns(c *fiber.Ctx) error {
	pag := utils.GetPagination(c)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	runs, total, err := h.svc.ListRuns(ctx, pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c, "failed to load content audit runs")
	}

	return utils.Paginated(c, "success", runs, pag.BuildMeta(total))
}

func (h *Handler) ShowRun(c *fiber.Ctx) error {
	runID, err := parseRunID(c)
	if err != nil {
		return utils.BadRequest(c, "invalid audit run id")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	run, err := h.svc.GetRun(ctx, runID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NotFound(c)
		}
		return utils.InternalError(c, "failed to load content audit run")
	}

	return utils.Success(c, "success", run)
}

func (h *Handler) ListFindings(c *fiber.Ctx) error {
	runID, err := parseRunID(c)
	if err != nil {
		return utils.BadRequest(c, "invalid audit run id")
	}

	pag := utils.GetPagination(c)
	risk := c.Query("risk")
	contentType := c.Query("type", c.Query("content_type"))
	search := c.Query("q")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	findings, total, err := h.svc.ListFindings(ctx, runID, risk, contentType, search, pag.PerPage, pag.Offset)
	if err != nil {
		return utils.InternalError(c, "failed to load content audit findings")
	}

	return utils.Paginated(c, "success", findings, pag.BuildMeta(total))
}

func (h *Handler) ExportCSV(c *fiber.Ctx) error {
	runID, err := parseRunID(c)
	if err != nil {
		return utils.BadRequest(c, "invalid audit run id")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var buf bytes.Buffer
	if err := h.svc.ExportCSV(ctx, runID, &buf); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NotFound(c)
		}
		return utils.InternalError(c, "failed to export content audit report")
	}

	c.Set(fiber.HeaderContentType, "text/csv; charset=utf-8")
	c.Set(fiber.HeaderContentDisposition, fmt.Sprintf(`attachment; filename="policy_audit_report_run_%d.csv"`, runID))
	return c.Send(buf.Bytes())
}

func parseRunID(c *fiber.Ctx) (uint64, error) {
	return strconv.ParseUint(c.Params("id"), 10, 64)
}

func (h *Handler) AnalyzeWithAI(c *fiber.Ctx) error {
	var req auditservice.AIAnalyzeRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.BadRequest(c, "invalid AI analysis payload")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	decision, err := h.svc.AnalyzeWithAI(ctx, req, currentUserID(c))
	if err != nil {
		if errors.Is(err, auditservice.ErrUnsupportedContentType) || err == strconv.ErrSyntax {
			return utils.BadRequest(c, err.Error())
		}
		if errors.Is(err, auditservice.ErrAIAnalysisInProgress) {
			return c.Status(fiber.StatusConflict).JSON(utils.APIResponse{
				Success: false,
				Message: "AI analysis is already running for this content",
			})
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NotFound(c)
		}
		logger.Error("failed to analyze content with AI decision engine",
			zap.String("content_type", req.ContentType),
			zap.String("content_id", req.ContentID),
			zap.String("country_code", req.CountryCode),
			zap.Error(err),
		)
		return utils.InternalError(c, "failed to analyze content with AI decision engine")
	}
	return utils.Created(c, "AI decision created", decision)
}

func (h *Handler) ShowAIDecision(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "invalid AI decision id")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	decision, err := h.svc.GetAIDecision(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NotFound(c)
		}
		return utils.InternalError(c, "failed to load AI decision")
	}
	return utils.Success(c, "success", decision)
}

func (h *Handler) LatestAIDecision(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	decision, err := h.svc.LatestAIDecision(ctx, c.Params("type"), c.Params("content_id"), c.Query("country", c.Query("country_code")))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.Success(c, "لا يوجد تحليل AI محفوظ لهذا المحتوى بعد", fiber.Map{"exists": false, "decision": nil})
		}
		return utils.InternalError(c, "failed to load AI decision")
	}
	return utils.Success(c, "success", fiber.Map{"exists": true, "decision": decision})
}

func (h *Handler) CreateFixPreview(c *fiber.Ctx) error {
	var req auditservice.AIFixRequest
	if err := c.BodyParser(&req); err != nil || req.DecisionID == 0 {
		return utils.BadRequest(c, "invalid fix preview payload")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 360*time.Second)
	defer cancel()
	ctx = auditservice.WithAIModelStrategy(ctx, req.ModelStrategy)
	preview, err := h.svc.CreateFixPreview(ctx, req.DecisionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NotFound(c)
		}
		return utils.InternalError(c, "failed to create AI fix preview: "+err.Error())
	}
	return utils.Created(c, "AI fix preview created", preview)
}

func (h *Handler) ShowFixPreview(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "invalid fix preview id")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	preview, err := h.svc.GetFixPreview(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NotFound(c)
		}
		return utils.InternalError(c, "failed to load AI fix preview")
	}
	return utils.Success(c, "success", preview)
}

func (h *Handler) ApplyFix(c *fiber.Ctx) error {
	var req auditservice.ApplyFixRequest
	if err := c.BodyParser(&req); err != nil || req.FixPreviewID == 0 {
		return utils.BadRequest(c, "invalid apply fix payload")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	preview, err := h.svc.ApplyFix(ctx, req.FixPreviewID, currentUserID(c), req.Note)
	if err != nil {
		if errors.Is(err, auditservice.ErrFixAlreadyClosed) || errors.Is(err, auditservice.ErrUnsupportedContentType) {
			return utils.BadRequest(c, err.Error())
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NotFound(c)
		}
		return utils.InternalError(c, "failed to apply AI fix")
	}
	return utils.Success(c, "AI fix applied", preview)
}

func (h *Handler) RejectFix(c *fiber.Ctx) error {
	var req auditservice.RejectFixRequest
	if err := c.BodyParser(&req); err != nil || req.FixPreviewID == 0 {
		return utils.BadRequest(c, "invalid reject fix payload")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	preview, err := h.svc.RejectFix(ctx, req.FixPreviewID, currentUserID(c), req.Note)
	if err != nil {
		if errors.Is(err, auditservice.ErrFixAlreadyClosed) {
			return utils.BadRequest(c, err.Error())
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NotFound(c)
		}
		return utils.InternalError(c, "failed to reject AI fix")
	}
	return utils.Success(c, "AI fix rejected", preview)
}

// PublicAdStatus returns ad eligibility for a content item.
// Intended for public article pages — returns only adsense_risk and eligible fields.
// No full decision data is exposed.
func (h *Handler) PublicAdStatus(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "invalid id")
	}
	countryCode := c.Get("X-Country-Code", c.Query("country", "jo"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	decision, err := h.svc.LatestAIDecision(ctx, "article", strconv.FormatUint(id, 10), countryCode)
	if err != nil {
		// No decision found → default to eligible (ads shown until audit flags it)
		return utils.Success(c, "success", fiber.Map{"eligible": true, "adsense_risk": "none"})
	}

	eligible := decision.AdSenseRisk != "high" &&
		decision.Decision != models.AIDecisionRejected &&
		decision.Decision != models.AIDecisionRestrictedAds

	return utils.Success(c, "success", fiber.Map{
		"eligible":     eligible,
		"adsense_risk": decision.AdSenseRisk,
	})
}

func currentUserID(c *fiber.Ctx) *uint {
	if user, ok := c.Locals("user").(*models.User); ok && user != nil {
		id := user.ID
		return &id
	}
	return nil
}

type adsenseReadinessItem struct {
	ID            uint     `json:"id"`
	Type          string   `json:"type"`
	Title         string   `json:"title"`
	Status        string   `json:"status"`
	Score         int      `json:"score"`
	Level         string   `json:"level"`
	WordCount     int      `json:"word_count"`
	CharCount     int      `json:"char_count"`
	FilesCount    int      `json:"files_count"`
	ShouldIndex   bool     `json:"should_index"`
	ShouldShowAds bool     `json:"should_show_ads"`
	Issues        []string `json:"issues"`
	URL           string   `json:"url"`
}

type adsenseReadinessSummary struct {
	Total       int64 `json:"total"`
	Ready       int   `json:"ready"`
	Review      int   `json:"review"`
	Weak        int   `json:"weak"`
	NoIndex     int   `json:"no_index"`
	AdsEligible int   `json:"ads_eligible"`
}

var htmlTagRe = regexp.MustCompile(`<[^>]+>`)
var whiteSpaceRe = regexp.MustCompile(`\s+`)

func plainTextForAdsense(value string) string {
	value = regexp.MustCompile(`(?is)<script.*?</script>|<style.*?</style>`).ReplaceAllString(value, " ")
	value = htmlTagRe.ReplaceAllString(value, " ")
	replacer := strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&quot;", "\"", "&#039;", "'", "&lt;", "<", "&gt;", ">")
	value = replacer.Replace(value)
	return strings.TrimSpace(whiteSpaceRe.ReplaceAllString(value, " "))
}

func evaluateAdsenseItem(title, content, meta string, filesCount int, published bool, contentType string, id uint, countryCode string) adsenseReadinessItem {
	text := plainTextForAdsense(content)
	words := 0
	if text != "" {
		words = len(strings.Fields(text))
	}
	issues := make([]string, 0, 6)
	score := 0
	if len(strings.TrimSpace(title)) >= 20 {
		score += 10
	} else {
		issues = append(issues, "العنوان قصير")
	}
	if words >= 300 {
		score += 25
	} else if words >= 120 {
		score += 12
		issues = append(issues, "المحتوى متوسط ويحتاج تعزيزًا")
	} else {
		issues = append(issues, "المحتوى قصير جدًا")
	}
	if len(strings.TrimSpace(meta)) >= 80 {
		score += 12
	} else {
		issues = append(issues, "وصف meta غير كافٍ")
	}
	if filesCount > 0 {
		score += 12
	} else {
		issues = append(issues, "لا توجد ملفات/مرفقات واضحة")
	}
	if published {
		score += 10
	} else {
		issues = append(issues, "غير منشور أو غير فعال")
	}
	score += 16
	if len(text) >= 600 {
		score += 15
	} else {
		issues = append(issues, "النص الفعلي أقل من حد الإعلانات الآمن")
	}
	if score > 100 {
		score = 100
	}
	level := "weak"
	if score >= 80 {
		level = "ready"
	} else if score >= 60 {
		level = "review"
	}
	shouldIndex := score >= 45 && published
	shouldShowAds := score >= 75 && len(text) >= 600 && published
	urlType := "lesson/articles"
	if contentType == "post" {
		urlType = "posts"
	}
	return adsenseReadinessItem{ID: id, Type: contentType, Title: title, Status: map[bool]string{true: "published", false: "unpublished"}[published], Score: score, Level: level, WordCount: words, CharCount: len(text), FilesCount: filesCount, ShouldIndex: shouldIndex, ShouldShowAds: shouldShowAds, Issues: issues, URL: fmt.Sprintf("/%s/%s/%d", countryCode, urlType, id)}
}

type adsenseReadinessRow struct {
	Item      adsenseReadinessItem
	CreatedAt time.Time
}

type adsenseFileCountRow struct {
	ID    uint
	Count int64
}

func buildAdsenseFileCountMaps(ctx context.Context, db *gorm.DB) (map[uint]int, map[uint]int) {
	articleCounts := make(map[uint]int)
	postCounts := make(map[uint]int)

	var articleRows []adsenseFileCountRow
	if err := db.WithContext(ctx).
		Table("files").
		Select("article_id AS id, COUNT(*) AS count").
		Where("article_id IS NOT NULL").
		Group("article_id").
		Scan(&articleRows).Error; err == nil {
		for _, row := range articleRows {
			articleCounts[row.ID] = int(row.Count)
		}
	}

	var postRows []adsenseFileCountRow
	if err := db.WithContext(ctx).
		Table("files").
		Select("post_id AS id, COUNT(*) AS count").
		Where("post_id IS NOT NULL").
		Group("post_id").
		Scan(&postRows).Error; err == nil {
		for _, row := range postRows {
			postCounts[row.ID] = int(row.Count)
		}
	}

	return articleCounts, postCounts
}

func updateAdsenseSummary(summary *adsenseReadinessSummary, item adsenseReadinessItem) {
	summary.Total++
	switch item.Level {
	case "ready":
		summary.Ready++
	case "review":
		summary.Review++
	default:
		summary.Weak++
	}
	if !item.ShouldIndex {
		summary.NoIndex++
	}
	if item.ShouldShowAds {
		summary.AdsEligible++
	}
}

// AdsenseReadiness returns a complete dashboard report that helps prioritize
// AdSense fixes across all articles and posts, not only the first page.
func (h *Handler) AdsenseReadiness(c *fiber.Ctx) error {
	countryID, _ := c.Locals("country_id").(database.CountryID)
	countryCode := c.Query("country", database.CountryCode(countryID))
	if countryCode == "" {
		countryCode = "jo"
	}
	db := database.GetManager().GetByCode(countryCode)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pag := utils.GetPagination(c)
	contentType := strings.ToLower(strings.TrimSpace(c.Query("type", "all")))
	if contentType != "all" && contentType != "article" && contentType != "post" {
		contentType = "all"
	}
	levelFilter := strings.ToLower(strings.TrimSpace(c.Query("level")))
	if levelFilter == "all" {
		levelFilter = ""
	}
	search := strings.TrimSpace(c.Query("q"))

	articleFileCounts, postFileCounts := buildAdsenseFileCountMaps(ctx, db)
	rows := make([]adsenseReadinessRow, 0, 256)
	globalSummary := adsenseReadinessSummary{}

	if contentType == "all" || contentType == "article" {
		var articles []models.Article
		q := db.WithContext(ctx).
			Select("id", "title", "content", "meta_description", "status", "created_at").
			Order("created_at DESC")
		if search != "" {
			q = q.Where("title LIKE ?", "%"+search+"%")
		}
		if err := q.Find(&articles).Error; err != nil {
			return utils.InternalError(c, "تعذر تحميل المقالات لفحص جاهزية AdSense")
		}
		for _, a := range articles {
			meta := ""
			if a.MetaDescription != nil {
				meta = *a.MetaDescription
			}
			item := evaluateAdsenseItem(a.Title, a.Content, meta, articleFileCounts[a.ID], a.Status == 1, "article", a.ID, countryCode)
			updateAdsenseSummary(&globalSummary, item)
			if levelFilter == "" || item.Level == levelFilter {
				rows = append(rows, adsenseReadinessRow{Item: item, CreatedAt: a.CreatedAt})
			}
		}
	}

	if contentType == "all" || contentType == "post" {
		var posts []models.Post
		q := db.WithContext(ctx).
			Select("id", "title", "content", "meta_description", "is_active", "created_at").
			Order("created_at DESC")
		if search != "" {
			q = q.Where("title LIKE ?", "%"+search+"%")
		}
		if err := q.Find(&posts).Error; err != nil {
			return utils.InternalError(c, "تعذر تحميل المنشورات لفحص جاهزية AdSense")
		}
		for _, p := range posts {
			meta := ""
			if p.MetaDescription != nil {
				meta = *p.MetaDescription
			}
			item := evaluateAdsenseItem(p.Title, p.Content, meta, postFileCounts[p.ID], p.IsActive, "post", p.ID, countryCode)
			updateAdsenseSummary(&globalSummary, item)
			if levelFilter == "" || item.Level == levelFilter {
				rows = append(rows, adsenseReadinessRow{Item: item, CreatedAt: p.CreatedAt})
			}
		}
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].CreatedAt.After(rows[j].CreatedAt)
	})

	filteredTotal := int64(len(rows))
	start := pag.Offset
	if start > len(rows) {
		start = len(rows)
	}
	end := start + pag.PerPage
	if end > len(rows) {
		end = len(rows)
	}

	items := make([]adsenseReadinessItem, 0, end-start)
	for _, row := range rows[start:end] {
		items = append(items, row.Item)
	}

	meta := pag.BuildMeta(filteredTotal)
	return utils.Success(c, "success", fiber.Map{
		"summary": globalSummary,
		"items":   items,
		"meta": fiber.Map{
			"current_page":   meta.CurrentPage,
			"per_page":       meta.PerPage,
			"total":          meta.Total,
			"last_page":      meta.LastPage,
			"from":           meta.From,
			"to":             meta.To,
			"filtered_total": filteredTotal,
		},
	})
}
