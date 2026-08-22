package contentaudit

import (
	"context"
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/imanjo/fiber-api/internal/contentquality"
	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/models"
	auditservice "github.com/imanjo/fiber-api/internal/services/contentaudit"
	"github.com/imanjo/fiber-api/internal/utils"
	"gorm.io/gorm"
)

type unifiedReadinessItem struct {
	ID                uint     `json:"id"`
	Type              string   `json:"type"`
	Title             string   `json:"title"`
	Status            string   `json:"status"`
	Score             int      `json:"score"`
	Level             string   `json:"level"`
	WordCount         int      `json:"word_count"`
	CharCount         int      `json:"char_count"`
	FilesCount        int      `json:"files_count"`
	ShouldIndex       bool     `json:"should_index"`
	ShouldShowAds     bool     `json:"should_show_ads"`
	Audited           bool     `json:"audited"`
	Decision          string   `json:"decision"`
	AdSenseRisk       string   `json:"adsense_risk"`
	GateReasons       []string `json:"gate_reasons"`
	DiagnosticSignals []string `json:"diagnostic_signals"`
	Issues            []string `json:"issues"`
	URL               string   `json:"url"`
}

type unifiedReadinessSummary struct {
	Total       int64 `json:"total"`
	Ready       int   `json:"ready"`
	Review      int   `json:"review"`
	Weak        int   `json:"weak"`
	NoIndex     int   `json:"no_index"`
	AdsEligible int   `json:"ads_eligible"`
	Audited     int   `json:"audited"`
	Unaudited   int   `json:"unaudited"`
}

type unifiedReadinessRow struct {
	Item      unifiedReadinessItem
	CreatedAt time.Time
}

type readinessFileCountRow struct {
	ID    uint
	Count int64
}

var (
	readinessScriptStyleRe = regexp.MustCompile(`(?is)<script.*?</script>|<style.*?</style>`)
	readinessHTMLTagRe     = regexp.MustCompile(`<[^>]+>`)
	readinessWhitespaceRe  = regexp.MustCompile(`\s+`)
)

func readinessPlainText(value string) string {
	value = readinessScriptStyleRe.ReplaceAllString(value, " ")
	value = readinessHTMLTagRe.ReplaceAllString(value, " ")
	value = html.UnescapeString(value)
	return strings.TrimSpace(readinessWhitespaceRe.ReplaceAllString(value, " "))
}

func readinessFileCountMaps(ctx context.Context, db *gorm.DB) (map[uint]int, map[uint]int) {
	articleCounts := make(map[uint]int)
	postCounts := make(map[uint]int)

	var articleRows []readinessFileCountRow
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

	var postRows []readinessFileCountRow
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

func latestReadinessDecisions(ctx context.Context, contentType, countryCode string) (map[uint]*models.ContentAIDecision, error) {
	var rows []models.ContentAIDecision
	if err := database.DB().WithContext(ctx).
		Preload("Issues").
		Where("content_type = ? AND country_code = ?", contentType, countryCode).
		Order("created_at DESC, id DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	latest := make(map[uint]*models.ContentAIDecision, len(rows))
	for i := range rows {
		row := &rows[i]
		rawID := strings.TrimSpace(row.ContentID)
		if idx := strings.LastIndex(rawID, ":"); idx >= 0 {
			rawID = rawID[idx+1:]
		}
		parsed, err := strconv.ParseUint(rawID, 10, 64)
		if err != nil || parsed == 0 {
			continue
		}
		id := uint(parsed)
		if _, exists := latest[id]; exists {
			continue
		}
		latest[id] = row
	}
	return latest, nil
}

func readinessGate(decision *models.ContentAIDecision, title, content, meta, keywords string, editorial ...*models.ContentEditorialDecision) auditservice.ContentQualityGate {
	gate := auditservice.EvaluateQualityGate(decision)
	artifacts := contentquality.DetectReplacementArtifacts(
		contentquality.TextField{Name: "title", Value: title},
		contentquality.TextField{Name: "content", Value: content},
		contentquality.TextField{Name: "meta_description", Value: meta},
		contentquality.TextField{Name: "keywords", Value: keywords},
	)
	gate = contentquality.ApplyReplacementArtifactGuard(gate, artifacts)
	if len(editorial) > 0 && editorial[0] != nil {
		gate = contentquality.ApplyEditorialDecision(gate, editorial[0].Decision)
	}
	return gate
}

func buildUnifiedReadinessItem(title, content, meta, keywords string, filesCount int, published bool, contentType string, id uint, countryCode string, gate auditservice.ContentQualityGate) unifiedReadinessItem {
	diagnostics := contentquality.EvaluateDiagnostics(title, readinessPlainText(content), meta, filesCount, published)
	shouldIndex := published && gate.Indexable
	shouldShowAds := published && gate.AdsEligible

	level := "review"
	if shouldShowAds {
		level = "ready"
	} else if !shouldIndex {
		level = "weak"
	}

	issues := make([]string, 0, len(gate.Reasons)+len(diagnostics.Signals))
	seen := make(map[string]struct{}, len(gate.Reasons)+len(diagnostics.Signals))
	for _, value := range append(append([]string{}, gate.Reasons...), diagnostics.Signals...) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		issues = append(issues, value)
	}

	urlType := "lesson/articles"
	if contentType == "post" {
		urlType = "posts"
	}

	return unifiedReadinessItem{
		ID:                id,
		Type:              contentType,
		Title:             title,
		Status:            map[bool]string{true: "published", false: "unpublished"}[published],
		Score:             gate.Score,
		Level:             level,
		WordCount:         diagnostics.WordCount,
		CharCount:         diagnostics.CharCount,
		FilesCount:        diagnostics.FilesCount,
		ShouldIndex:       shouldIndex,
		ShouldShowAds:     shouldShowAds,
		Audited:           gate.Audited,
		Decision:          gate.Decision,
		AdSenseRisk:       gate.Risk,
		GateReasons:       append([]string(nil), gate.Reasons...),
		DiagnosticSignals: append([]string(nil), diagnostics.Signals...),
		Issues:            issues,
		URL:               "/" + countryCode + "/" + urlType + "/" + strconv.FormatUint(uint64(id), 10),
	}
}

func updateUnifiedReadinessSummary(summary *unifiedReadinessSummary, item unifiedReadinessItem) {
	summary.Total++
	switch item.Level {
	case "ready":
		summary.Ready++
	case "weak":
		summary.Weak++
	default:
		summary.Review++
	}
	if !item.ShouldIndex {
		summary.NoIndex++
	}
	if item.ShouldShowAds {
		summary.AdsEligible++
	}
	if item.Audited {
		summary.Audited++
	} else {
		summary.Unaudited++
	}
}

// AdsenseReadinessUnified is the dashboard report backed by the same Content
// Quality Gate used by public ad-status, SEO noindex, and sitemap generation.
// Word count, title/meta length, and attachment presence are reviewer diagnostics
// only; they can never override the canonical gate decision.
func (h *Handler) AdsenseReadinessUnified(c *fiber.Ctx) error {
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

	articleFileCounts, postFileCounts := readinessFileCountMaps(ctx, db)
	rows := make([]unifiedReadinessRow, 0, 256)
	globalSummary := unifiedReadinessSummary{}

	if contentType == "all" || contentType == "article" {
		decisions, err := latestReadinessDecisions(ctx, "article", countryCode)
		if err != nil {
			return utils.InternalError(c, "تعذر تحميل قرارات جودة المقالات")
		}
		editorial, err := latestReadinessEditorialDecisions(ctx, "article", countryCode)
		if err != nil {
			return utils.InternalError(c, "تعذر تحميل قرارات المراجعة التحريرية للمقالات")
		}
		var articles []models.Article
		q := db.WithContext(ctx).
			Select("id", "title", "content", "meta_description", "status", "created_at").
			Order("created_at DESC")
		if search != "" {
			q = q.Where("title LIKE ?", "%"+search+"%")
		}
		if err := q.Find(&articles).Error; err != nil {
			return utils.InternalError(c, "تعذر تحميل المقالات لفحص جاهزية المحتوى")
		}
		for _, article := range articles {
			meta := ""
			if article.MetaDescription != nil {
				meta = *article.MetaDescription
			}
			gate := readinessGate(decisions[article.ID], article.Title, article.Content, meta, "", editorial[article.ID])
			item := buildUnifiedReadinessItem(article.Title, article.Content, meta, "", articleFileCounts[article.ID], article.Status == 1, "article", article.ID, countryCode, gate)
			updateUnifiedReadinessSummary(&globalSummary, item)
			if levelFilter == "" || item.Level == levelFilter {
				rows = append(rows, unifiedReadinessRow{Item: item, CreatedAt: article.CreatedAt})
			}
		}
	}

	if contentType == "all" || contentType == "post" {
		decisions, err := latestReadinessDecisions(ctx, "post", countryCode)
		if err != nil {
			return utils.InternalError(c, "تعذر تحميل قرارات جودة المنشورات")
		}
		editorial, err := latestReadinessEditorialDecisions(ctx, "post", countryCode)
		if err != nil {
			return utils.InternalError(c, "تعذر تحميل قرارات المراجعة التحريرية للمنشورات")
		}
		var posts []models.Post
		q := db.WithContext(ctx).
			Select("id", "title", "content", "meta_description", "keywords", "is_active", "created_at").
			Order("created_at DESC")
		if search != "" {
			q = q.Where("title LIKE ?", "%"+search+"%")
		}
		if err := q.Find(&posts).Error; err != nil {
			return utils.InternalError(c, "تعذر تحميل المنشورات لفحص جاهزية المحتوى")
		}
		for _, post := range posts {
			meta := ""
			if post.MetaDescription != nil {
				meta = *post.MetaDescription
			}
			keywords := ""
			if post.Keywords != nil {
				keywords = *post.Keywords
			}
			gate := readinessGate(decisions[post.ID], post.Title, post.Content, meta, keywords, editorial[post.ID])
			item := buildUnifiedReadinessItem(post.Title, post.Content, meta, keywords, postFileCounts[post.ID], post.IsActive, "post", post.ID, countryCode, gate)
			updateUnifiedReadinessSummary(&globalSummary, item)
			if levelFilter == "" || item.Level == levelFilter {
				rows = append(rows, unifiedReadinessRow{Item: item, CreatedAt: post.CreatedAt})
			}
		}
	}

	sort.SliceStable(rows, func(i, j int) bool { return rows[i].CreatedAt.After(rows[j].CreatedAt) })

	filteredTotal := int64(len(rows))
	start := pag.Offset
	if start > len(rows) {
		start = len(rows)
	}
	end := start + pag.PerPage
	if end > len(rows) {
		end = len(rows)
	}

	items := make([]unifiedReadinessItem, 0, end-start)
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
