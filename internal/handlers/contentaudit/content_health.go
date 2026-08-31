package contentaudit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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

// contentHealthIssue is one human-readable problem plus the single action that
// resolves it: "ai" (AI fix with preview), "editor" (open the editor),
// "manual" (needs a human decision, no automation).
type contentHealthIssue struct {
	Label    string `json:"label"`
	Severity string `json:"severity"` // high | medium | low
	Fix      string `json:"fix"`      // ai | editor | manual
}

type contentHealthItem struct {
	ID          uint                 `json:"id"`
	Type        string               `json:"type"`
	Title       string               `json:"title"`
	Published   bool                 `json:"published"`
	Status      string               `json:"status"` // good | needs_work | problem
	Score       int                  `json:"score"`
	Indexable   bool                 `json:"indexable"`
	AdsEligible bool                 `json:"ads_eligible"`
	Issues      []contentHealthIssue `json:"issues"`
}

type contentHealthSummary struct {
	Total     int `json:"total"`
	Good      int `json:"good"`
	NeedsWork int `json:"needs_work"`
	Problem   int `json:"problem"`
}

type contentHealthComputed struct {
	Summary   contentHealthSummary `json:"summary"`
	Items     []contentHealthItem  `json:"items"` // full set, worst-first
	Generated time.Time            `json:"generated"`
}

// seoStoredAnalysis is the subset of services.SEOAnalysisResult persisted in
// seo_metadata.analysis_json — read here so the failed checks become issues
// without re-running AnalyzeSEO.
type seoStoredAnalysis struct {
	Score  int `json:"score"`
	Checks []struct {
		Code    string `json:"code"`
		Status  string `json:"status"`
		Message string `json:"message"`
	} `json:"checks"`
}

// seoCheckFix maps an analyzer check code to how a human resolves it.
var seoCheckFix = map[string]string{
	"title_presence": "ai", "title_length": "ai",
	"description_presence": "ai", "description_length": "ai",
	"focus_keyword": "ai", "keyword_title": "ai", "keyword_description": "ai",
	"keyword_density": "ai", "schema": "ai", "canonical": "ai",
	"keyword_intro": "editor", "share_image": "editor", "content_length": "editor",
	"headings": "editor", "readability": "editor", "internal_links": "editor",
	"external_links": "editor", "image_alts": "editor",
}

func readinessFixBucket(actionType string) string {
	switch actionType {
	case "analyze":
		return "audit" // never AI-quality-checked yet → run the audit first
	case "manual", "full_review":
		return "manual"
	default:
		return "ai"
	}
}

func normalizeHealthSeverity(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical", "high":
		return "high"
	case "medium", "warning":
		return "medium"
	default:
		return "low"
	}
}

const contentHealthCacheTTL = 2 * time.Minute

// ContentHealth returns one simple verdict per article/post — status + score +
// human-readable issues, each with the one action that fixes it. It merges the
// readiness gate/diagnostics with the stored SEO analysis so the editor sees a
// single list instead of six overlapping audit surfaces. Result is cached per
// country for 2 minutes so filtering/paging never re-runs the heavy scan.
func (h *Handler) ContentHealth(c *fiber.Ctx) error {
	countryID, _ := c.Locals("country_id").(database.CountryID)
	countryCode := c.Query("country", database.CountryCode(countryID))
	if countryCode == "" {
		countryCode = "jo"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	computed, err := h.contentHealthComputed(ctx, countryCode)
	if err != nil {
		return utils.InternalError(c, "تعذر تحميل صحة المحتوى")
	}

	typeFilter := strings.ToLower(strings.TrimSpace(c.Query("type", "all")))
	statusFilter := strings.ToLower(strings.TrimSpace(c.Query("status")))
	search := strings.ToLower(strings.TrimSpace(c.Query("q")))
	filtered := make([]contentHealthItem, 0, len(computed.Items))
	for _, item := range computed.Items {
		if typeFilter != "" && typeFilter != "all" && item.Type != typeFilter {
			continue
		}
		if statusFilter != "" && statusFilter != "all" && item.Status != statusFilter {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(item.Title), search) {
			continue
		}
		filtered = append(filtered, item)
	}

	pag := utils.GetPagination(c)
	total := int64(len(filtered))
	start := pag.Offset
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + pag.PerPage
	if end > len(filtered) {
		end = len(filtered)
	}
	meta := pag.BuildMeta(total)
	return utils.Success(c, "success", fiber.Map{
		"summary": computed.Summary,
		"items":   filtered[start:end],
		"meta": fiber.Map{
			"current_page": meta.CurrentPage, "per_page": meta.PerPage,
			"total": meta.Total, "last_page": meta.LastPage, "from": meta.From, "to": meta.To,
		},
	})
}

func (h *Handler) contentHealthComputed(ctx context.Context, countryCode string) (contentHealthComputed, error) {
	rdb := database.Redis()
	cacheKey := rdb.Key("content_health", "v1", countryCode)
	var cached contentHealthComputed
	if rdb.GetJSON(ctx, cacheKey, &cached) && !cached.Generated.IsZero() {
		return cached, nil
	}

	db := database.GetManager().GetByCode(countryCode)
	collected, err := collectReadinessItems(ctx, db, countryCode, "all", "")
	if err != nil {
		return contentHealthComputed{}, err
	}

	// Exact / near-exact duplicate detection. NormalizeForSimilarity strips HTML,
	// diacritics and punctuation, so a lightly-edited copy hashes to the same
	// value. O(n) — safe behind the 2-minute cache.
	dupOf := make(map[string]string)
	if len(collected) > 1 {
		byHash := make(map[string][]int, len(collected))
		for i := range collected {
			norm := contentquality.NormalizeForSimilarity(collected[i].body)
			if len([]rune(norm)) < 200 {
				continue // too short to meaningfully flag as a duplicate
			}
			sum := sha256.Sum256([]byte(norm))
			h := hex.EncodeToString(sum[:])
			byHash[h] = append(byHash[h], i)
		}
		for _, idxs := range byHash {
			if len(idxs) < 2 {
				continue
			}
			for _, i := range idxs {
				for _, j := range idxs {
					if j == i {
						continue
					}
					k := collected[i].Item.Type + ":" + strconv.FormatUint(uint64(collected[i].Item.ID), 10)
					dupOf[k] = collected[j].Item.Title
					break
				}
			}
		}
	}

	// Stored SEO analysis + noindex flag — one query.
	seoByKey := make(map[string]seoStoredAnalysis)
	seoNoindex := make(map[string]bool)
	var seoRows []models.SEOMetadata
	_ = db.WithContext(ctx).
		Select("content_type", "content_id", "robots_index", "analysis_json").
		Where("content_type IN ?", []string{"article", "post"}).
		Find(&seoRows).Error
	for _, row := range seoRows {
		key := row.ContentType + ":" + strconv.FormatUint(uint64(row.ContentID), 10)
		if !row.RobotsIndex {
			seoNoindex[key] = true
		}
		if strings.TrimSpace(row.AnalysisJSON) == "" {
			continue
		}
		var analysis seoStoredAnalysis
		if json.Unmarshal([]byte(row.AnalysisJSON), &analysis) == nil {
			seoByKey[key] = analysis
		}
	}

	items := make([]contentHealthItem, 0, len(collected))
	summary := contentHealthSummary{}
	for _, row := range collected {
		it := row.Item
		key := it.Type + ":" + strconv.FormatUint(uint64(it.ID), 10)
		published := it.Status == "published"
		health := contentHealthItem{
			ID: it.ID, Type: it.Type, Title: it.Title, Published: published,
			Score: it.Score, Indexable: it.ShouldIndex, AdsEligible: it.ShouldShowAds,
			Issues: make([]contentHealthIssue, 0, 4),
		}
		seen := make(map[string]bool)
		addIssue := func(label, severity, fix string) {
			label = strings.TrimSpace(label)
			if label == "" || seen[label] {
				return
			}
			seen[label] = true
			health.Issues = append(health.Issues, contentHealthIssue{Label: label, Severity: severity, Fix: fix})
		}

		for _, p := range it.Problems {
			if p.Code == readinessProblemUnpublished {
				continue // "draft" is a state, not a health issue
			}
			addIssue(p.Message, normalizeHealthSeverity(p.Severity), readinessFixBucket(p.ActionType))
		}
		if analysis, ok := seoByKey[key]; ok {
			if analysis.Score > 0 {
				health.Score = analysis.Score
			}
			for _, ch := range analysis.Checks {
				if ch.Status == "good" {
					continue
				}
				sev := "medium"
				if ch.Status == "error" {
					sev = "high"
				} else if ch.Status == "notice" {
					sev = "low"
				}
				fix := seoCheckFix[ch.Code]
				if fix == "" {
					fix = "editor"
				}
				addIssue(ch.Message, sev, fix)
			}
		}
		if seoNoindex[key] {
			addIssue("مُستبعد من الفهرسة (noindex)", "medium", "editor")
		}
		if other := dupOf[key]; other != "" {
			addIssue("محتوى مطابق لـ: "+other, "high", "manual")
		}

		health.Status = contentHealthStatus(health, published, it.ShouldIndex)
		items = append(items, health)
		summary.Total++
		switch health.Status {
		case "good":
			summary.Good++
		case "problem":
			summary.Problem++
		default:
			summary.NeedsWork++
		}
	}

	rank := map[string]int{"problem": 0, "needs_work": 1, "good": 2}
	sort.SliceStable(items, func(i, j int) bool {
		if rank[items[i].Status] != rank[items[j].Status] {
			return rank[items[i].Status] < rank[items[j].Status]
		}
		return items[i].Score < items[j].Score
	})

	computed := contentHealthComputed{Summary: summary, Items: items, Generated: time.Now().UTC()}
	_ = rdb.SetJSON(ctx, cacheKey, computed, contentHealthCacheTTL)
	return computed, nil
}

// ContentHealthAudit runs the AI content-quality audit for one article/post —
// the "بدء تدقيق الجودة" action for items whose only blocker is "never audited".
// It self-loads the title/content so the drawer needs to send only the id.
func (h *Handler) ContentHealthAudit(c *fiber.Ctx) error {
	contentType := "article"
	if strings.ToLower(strings.TrimSpace(c.Params("content_type"))) == "post" {
		contentType = "post"
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil || id == 0 {
		return utils.BadRequest(c, "معرف المحتوى غير صالح")
	}
	countryID, _ := c.Locals("country_id").(database.CountryID)
	countryCode := database.CountryCode(countryID)
	if countryCode == "" {
		countryCode = "jo"
	}
	db := database.GetManager().GetByCode(countryCode)

	var title, content, meta string
	if contentType == "article" {
		var a models.Article
		if err := db.Select("title", "content", "meta_description").First(&a, id).Error; err != nil {
			return utils.NotFound(c)
		}
		title, content = a.Title, a.Content
		if a.MetaDescription != nil {
			meta = *a.MetaDescription
		}
	} else {
		var p models.Post
		if err := db.Select("title", "content", "meta_description").First(&p, id).Error; err != nil {
			return utils.NotFound(c)
		}
		title, content = p.Title, p.Content
		if p.MetaDescription != nil {
			meta = *p.MetaDescription
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	decision, err := h.svc.AnalyzeWithAI(ctx, auditservice.AIAnalyzeRequest{
		ContentType:     contentType,
		ContentID:       strconv.FormatUint(id, 10),
		CountryCode:     countryCode,
		Title:           title,
		Content:         content,
		MetaDescription: meta,
	}, currentUserID(c))
	if err != nil {
		if errors.Is(err, auditservice.ErrAIAnalysisInProgress) {
			return c.Status(fiber.StatusConflict).JSON(utils.APIResponse{Success: false, Message: "التدقيق جارٍ بالفعل لهذا المحتوى"})
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NotFound(c)
		}
		return utils.InternalError(c, "تعذّر تدقيق المحتوى بالذكاء")
	}

	_ = database.Redis().Del(context.Background(), database.Redis().Key("content_health", "v1", countryCode))
	return utils.Success(c, "تم تدقيق المحتوى", fiber.Map{"decision": decision})
}

func contentHealthStatus(h contentHealthItem, published, shouldIndex bool) string {
	if published && !shouldIndex {
		return "problem"
	}
	for _, issue := range h.Issues {
		if issue.Severity == "high" {
			return "problem"
		}
	}
	if len(h.Issues) > 0 {
		return "needs_work"
	}
	return "good"
}
