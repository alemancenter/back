// Package adsensepolicy is a small, deliberately deterministic (no AI, no external calls)
// dashboard tool for spotting content that risks an AdSense content-policy rejection —
// duplicate/near-duplicate content, thin/weak content, generic AI-boilerplate content, and
// corrupted content — across articles and posts. It reuses internal/contentquality's existing,
// already-tested engines: DetectSimilarity (the same shingling/Jaccard engine ArticleService/
// PostService already gate new saves against, run here as the full O(n^2) cross-corpus scan that
// save-time gate deliberately skips — its exact/near/template severity ladder deliberately
// excludes SimilarityKindTitleOnly, a same-title-different-content match, since that is not a
// content-duplication risk and must never carry the same merge/delete/redirect recommendation),
// EvaluateDiagnostics (the reviewer-facing thin-content signal), DetectGenericFillerPhrases (the
// exact generic-padding phrases that got the site rejected by AdSense before — catches content
// that clears every word-count threshold yet is still mostly low-value filler), and
// DetectReplacementArtifacts (catches unresolved "${1}"-style template placeholders that have
// previously leaked into published content). Human-reviewed only: this never edits,
// unpublishes, or redirects anything by itself.
package adsensepolicy

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/imanjo/fiber-api/internal/contentquality"
	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/utils"
)

type Handler struct{}

func New() *Handler {
	return &Handler{}
}

type contentRow struct {
	ID              uint      `gorm:"column:id"`
	Title           string    `gorm:"column:title"`
	Content         string    `gorm:"column:content"`
	MetaDescription string    `gorm:"column:meta_description"`
	FilesCount      int       `gorm:"column:files_count"`
	Published       bool      `gorm:"column:published"`
	UpdatedAt       time.Time `gorm:"column:updated_at"`
}

// Member is one article/post participating in a duplicate cluster.
type Member struct {
	Key       string    `json:"key"`
	ID        uint      `json:"id"`
	Type      string    `json:"type"` // "article" | "post"
	Title     string    `json:"title"`
	Published bool      `json:"published"`
	UpdatedAt time.Time `json:"updated_at"`
	WordCount int       `json:"word_count"`
	PublicURL string    `json:"public_url"`
	EditURL   string    `json:"edit_url"`
}

// Cluster groups two or more items the similarity engine flagged as duplicates of each other.
type Cluster struct {
	ID             string                          `json:"id"`
	Kind           string                          `json:"kind"` // exact | near | template
	KindLabel      string                          `json:"kind_label"`
	Recommendation string                          `json:"recommendation"`
	MatchedOn      []string                        `json:"matched_on,omitempty"`
	MaxSimilarity  float64                         `json:"max_similarity"`
	MinSimilarity  float64                         `json:"min_similarity"`
	Members        []Member                        `json:"members"`
	Pairs          []contentquality.SimilarityPair `json:"pairs"`
}

// WeakContentItem is one article/post whose reviewer diagnostics show thin/weak content.
type WeakContentItem struct {
	Member
	WordCount int      `json:"word_count"`
	Signals   []string `json:"signals"`
}

// PolicyIssueItem is one article/post with a detected, concrete policy-relevant defect —
// currently just unresolved replacement-template artifacts leaking into published content.
type PolicyIssueItem struct {
	Member
	Issues []contentquality.ReplacementArtifact `json:"issues"`
}

type ScanSummary struct {
	ScannedItems     int `json:"scanned_items"`
	IgnoredItems     int `json:"ignored_items"`
	TotalClusters    int `json:"total_clusters"`
	ExactClusters    int `json:"exact_clusters"`
	NearClusters     int `json:"near_clusters"`
	TemplateClusters int `json:"template_clusters"`
	// TitleOnlyClusters counts clusters whose ONLY match is an identical title with different
	// content — not an AdSense content-duplication risk, counted separately so it never
	// inflates ExactClusters (see contentquality.SimilarityKindTitleOnly).
	TitleOnlyClusters int `json:"title_only_clusters"`
	AffectedItems     int `json:"affected_items"`
	WeakContentItems  int `json:"weak_content_items"`
	PolicyIssueItems  int `json:"policy_issue_items"`
}

// ScanResult is the full payload of one completed scan — cached in Redis so the dashboard page
// can show the last scan again on a plain reload instead of coming back empty until the admin
// clicks "فحص" again.
type ScanResult struct {
	Clusters     []Cluster         `json:"clusters"`
	WeakContent  []WeakContentItem `json:"weak_content"`
	PolicyIssues []PolicyIssueItem `json:"policy_issues"`
	Summary      ScanSummary       `json:"summary"`
	ScannedAt    time.Time         `json:"scanned_at"`
}

// scanCacheTTL is generous (not a "freshness" window — a scan only ever changes when someone
// clicks "فحص" again) so a result survives well past any reasonable gap between dashboard
// visits; it exists only so an abandoned scan doesn't linger in Redis forever.
const scanCacheTTL = 30 * 24 * time.Hour

func scanCacheKey(countryID database.CountryID) string {
	return database.Redis().Key("adsense_policy", "scan", string(database.CountryCode(countryID)))
}

// GetLastScan returns the most recently completed scan for this country, or success with a nil
// data payload if none has ever run — the dashboard page loads this on render so results
// persist across a plain page reload instead of disappearing until "فحص" is clicked again.
// @Summary Get the last AdSense content-policy scan result
// @Tags AdSense Policy
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Success 200 {object} utils.APIResponse
// @Router /dashboard/adsense-policy/scan [get]
func (h *Handler) GetLastScan(c *fiber.Ctx) error {
	countryID, _ := c.Locals("country_id").(database.CountryID)
	if countryID == 0 {
		countryID = database.CountryJordan
	}
	var result ScanResult
	if !database.Redis().GetJSON(c.UserContext(), scanCacheKey(countryID), &result) {
		return utils.Success(c, "success", nil)
	}
	return utils.Success(c, "success", result)
}

// ScanDuplicates runs a full deterministic content-policy scan (duplicate/near-duplicate
// content, thin content, and corrupted-content artifacts) across every article and post in the
// requesting country's database. Triggered on-demand (dashboard "فحص كامل" button) rather than
// on every page load — the duplicate scan is O(n^2) in library size, same tradeoff the deleted
// content-audit tool made.
// @Summary Scan articles and posts for AdSense content-policy risks
// @Tags AdSense Policy
// @Produce json
// @Security BearerAuth
// @Security FrontendKeyAuth
// @Param X-Country-Id header string false "Country ID"
// @Success 200 {object} utils.APIResponse
// @Router /dashboard/adsense-policy/scan [post]
func (h *Handler) ScanDuplicates(c *fiber.Ctx) error {
	countryID, _ := c.Locals("country_id").(database.CountryID)
	if countryID == 0 {
		countryID = database.CountryJordan
	}
	countryCode := string(database.CountryCode(countryID))

	ctx, cancel := context.WithTimeout(c.Context(), 55*time.Second)
	defer cancel()

	documents, members, rows, err := loadDocuments(ctx, countryID, countryCode)
	if err != nil {
		return utils.InternalError(c, "تعذر تحميل المقالات والمنشورات للفحص")
	}

	options := contentquality.DefaultSimilarityOptions()
	report := contentquality.DetectSimilarity(documents, options)
	clusters := buildClusters(report.Clusters, members)
	weakItems := findWeakContent(rows, members)
	policyIssues := findPolicyIssues(rows, members)
	summary := summarize(clusters, report, weakItems, policyIssues)

	result := ScanResult{
		Clusters: clusters, WeakContent: weakItems, PolicyIssues: policyIssues,
		Summary: summary, ScannedAt: time.Now(),
	}
	// Best-effort: a cache write failure shouldn't fail a scan that already succeeded.
	_ = database.Redis().SetJSON(c.UserContext(), scanCacheKey(countryID), result, scanCacheTTL)

	return utils.Success(c, "success", result)
}

// loadDocuments returns the similarity-engine documents, a Key->Member lookup, and the raw rows
// (kept around so the weak-content/policy-issue passes below don't need a second DB round trip).
func loadDocuments(ctx context.Context, countryID database.CountryID, countryCode string) ([]contentquality.SimilarityDocument, map[string]Member, map[string]contentRow, error) {
	db := database.DBForCountry(countryID).WithContext(ctx)
	documents := make([]contentquality.SimilarityDocument, 0)
	members := make(map[string]Member)
	rows := make(map[string]contentRow)

	var articleRows []contentRow
	if err := db.Raw(`
		SELECT a.id, a.title, a.content, COALESCE(a.meta_description, '') AS meta_description,
			(a.status = 1) AS published, a.updated_at,
			(SELECT COUNT(*) FROM files f WHERE f.article_id = a.id) AS files_count
		FROM articles a
	`).Scan(&articleRows).Error; err != nil {
		return nil, nil, nil, err
	}
	for _, row := range articleRows {
		key := fmt.Sprintf("article:%d", row.ID)
		rows[key] = row
		members[key] = Member{
			Key: key, ID: row.ID, Type: "article", Title: row.Title, Published: row.Published,
			UpdatedAt: row.UpdatedAt, WordCount: contentquality.SimilarityWordCount(row.Content),
			PublicURL: fmt.Sprintf("/%s/lesson/articles/%d", countryCode, row.ID),
			EditURL:   fmt.Sprintf("/dashboard/articles/%d/edit", row.ID),
		}
		documents = append(documents, contentquality.SimilarityDocument{Key: key, Title: row.Title, Content: row.Content})
	}

	var postRows []contentRow
	if err := db.Raw(`
		SELECT p.id, p.title, p.content, COALESCE(p.meta_description, '') AS meta_description,
			p.is_active AS published, p.updated_at,
			(SELECT COUNT(*) FROM files f WHERE f.post_id = p.id) AS files_count
		FROM posts p
	`).Scan(&postRows).Error; err != nil {
		return nil, nil, nil, err
	}
	for _, row := range postRows {
		key := fmt.Sprintf("post:%d", row.ID)
		rows[key] = row
		members[key] = Member{
			Key: key, ID: row.ID, Type: "post", Title: row.Title, Published: row.Published,
			UpdatedAt: row.UpdatedAt, WordCount: contentquality.SimilarityWordCount(row.Content),
			PublicURL: fmt.Sprintf("/%s/posts/%d", countryCode, row.ID),
			EditURL:   fmt.Sprintf("/dashboard/posts/%d/edit", row.ID),
		}
		documents = append(documents, contentquality.SimilarityDocument{Key: key, Title: row.Title, Content: row.Content})
	}

	return documents, members, rows, nil
}

func buildClusters(clusters []contentquality.SimilarityCluster, memberMap map[string]Member) []Cluster {
	items := make([]Cluster, 0, len(clusters))
	for _, cluster := range clusters {
		clusterMembers := make([]Member, 0, len(cluster.Members))
		for _, key := range cluster.Members {
			if member, ok := memberMap[key]; ok {
				clusterMembers = append(clusterMembers, member)
			}
		}
		if len(clusterMembers) < 2 {
			continue
		}
		sort.SliceStable(clusterMembers, func(i, j int) bool {
			if clusterMembers[i].Published != clusterMembers[j].Published {
				return clusterMembers[i].Published
			}
			return clusterMembers[i].UpdatedAt.After(clusterMembers[j].UpdatedAt)
		})
		matchedOn := map[string]bool{}
		for _, pair := range cluster.Pairs {
			for _, on := range pair.MatchedOn {
				matchedOn[on] = true
			}
		}
		matchedOnList := make([]string, 0, len(matchedOn))
		for on := range matchedOn {
			matchedOnList = append(matchedOnList, on)
		}
		sort.Strings(matchedOnList)
		items = append(items, Cluster{
			ID: cluster.ID, Kind: cluster.Kind, KindLabel: kindLabel(cluster.Kind),
			Recommendation: recommendation(cluster.Kind), MatchedOn: matchedOnList,
			MaxSimilarity: cluster.MaxSimilarity, MinSimilarity: cluster.MinSimilarity,
			Members: clusterMembers, Pairs: cluster.Pairs,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if kindPriority(items[i].Kind) != kindPriority(items[j].Kind) {
			return kindPriority(items[i].Kind) > kindPriority(items[j].Kind)
		}
		return items[i].MaxSimilarity > items[j].MaxSimilarity
	})
	return items
}

// findWeakContent runs EvaluateDiagnostics (word count, title length, meta description length,
// attachments, publish state) over every item, ALSO checks for the same generic AI-filler
// boilerplate phrases the content-draft/fix AI is instructed to never produce
// (contentquality.DetectGenericFillerPhrases), and returns the ones with at least one signal.
// The filler-phrase check matters on its own: a published item can clear every word-count
// threshold and still be mostly generic padding with no real subject-matter value — exactly the
// "thin/low-value content" failure mode that got the site rejected by AdSense before, and one a
// word-count check alone cannot see. Only published items are reported — an unfinished draft
// being short or padded is not a policy risk since nothing has shipped yet.
func findWeakContent(rows map[string]contentRow, members map[string]Member) []WeakContentItem {
	items := make([]WeakContentItem, 0)
	for key, row := range rows {
		if !row.Published {
			continue
		}
		member, ok := members[key]
		if !ok {
			continue
		}
		plainText := contentquality.NormalizeForSimilarity(row.Content)
		diag := contentquality.EvaluateDiagnostics(row.Title, plainText, row.MetaDescription, row.FilesCount, row.Published)
		signals := diag.Signals
		if filler := contentquality.DetectGenericFillerPhrases(plainText); len(filler) > 0 {
			signals = append(signals, fmt.Sprintf(
				"إشارة تحريرية: يحتوي على عبارات حشو عامة نمطية للذكاء الاصطناعي لا تحمل قيمة معرفية محددة (\"%s\") — قد يُعدّ محتوى منخفض القيمة وفق سياسة AdSense حتى لو كان طويلاً.",
				strings.Join(filler, "\"، \""),
			))
		}
		if len(signals) == 0 {
			continue
		}
		items = append(items, WeakContentItem{Member: member, WordCount: diag.WordCount, Signals: signals})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].WordCount < items[j].WordCount })
	return items
}

// findPolicyIssues scans every item's raw content for unresolved template-replacement
// artifacts (e.g. "${1}") that have previously leaked into published content — broken,
// non-functional-looking text that risks both a poor user experience and an AdSense
// content-quality flag.
func findPolicyIssues(rows map[string]contentRow, members map[string]Member) []PolicyIssueItem {
	items := make([]PolicyIssueItem, 0)
	for key, row := range rows {
		member, ok := members[key]
		if !ok {
			continue
		}
		artifacts := contentquality.DetectReplacementArtifacts(
			contentquality.TextField{Name: "title", Value: row.Title},
			contentquality.TextField{Name: "content", Value: row.Content},
		)
		if len(artifacts) == 0 {
			continue
		}
		items = append(items, PolicyIssueItem{Member: member, Issues: artifacts})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Published != items[j].Published {
			return items[i].Published
		}
		return len(items[i].Issues) > len(items[j].Issues)
	})
	return items
}

func summarize(clusters []Cluster, report contentquality.SimilarityReport, weakItems []WeakContentItem, policyIssues []PolicyIssueItem) ScanSummary {
	summary := ScanSummary{
		ScannedItems: report.ScannedDocuments, IgnoredItems: report.IgnoredDocuments, TotalClusters: len(clusters),
		WeakContentItems: len(weakItems), PolicyIssueItems: len(policyIssues),
	}
	affected := make(map[string]struct{})
	for _, cluster := range clusters {
		switch cluster.Kind {
		case contentquality.SimilarityKindExact:
			summary.ExactClusters++
		case contentquality.SimilarityKindNear:
			summary.NearClusters++
		case contentquality.SimilarityKindTemplate:
			summary.TemplateClusters++
		case contentquality.SimilarityKindTitleOnly:
			summary.TitleOnlyClusters++
		}
		for _, member := range cluster.Members {
			affected[member.Key] = struct{}{}
		}
	}
	summary.AffectedItems = len(affected)
	return summary
}

func kindLabel(kind string) string {
	switch kind {
	case contentquality.SimilarityKindExact:
		return "تطابق كامل في المحتوى"
	case contentquality.SimilarityKindNear:
		return "تكرار محتوى (تشابه عالٍ)"
	case contentquality.SimilarityKindTemplate:
		return "تشابه قالبي"
	case contentquality.SimilarityKindTitleOnly:
		return "تطابق العنوان فقط (المحتوى مختلف)"
	default:
		return kind
	}
}

func recommendation(kind string) string {
	switch kind {
	case contentquality.SimilarityKindExact:
		return "تطابق نصي كامل في المحتوى بعد التطبيع (قد يتطابق العنوان أيضًا). راجع الصفحات يدويًا قبل أي دمج أو حذف أو إعادة توجيه — هذا هو التكرار الذي تستهدفه سياسة AdSense لجودة المحتوى."
	case contentquality.SimilarityKindNear:
		return "تشابه مرتفع جدًا في المحتوى. راجع الهدف والمرفقات ثم وحّد الصفحات أو أعد كتابة أحدهما."
	case contentquality.SimilarityKindTemplate:
		return "بنية قالبية متشابهة مع اختلافات محدودة. أعد كتابة المحتوى ليحمل قيمة فريدة قبل النشر."
	case contentquality.SimilarityKindTitleOnly:
		return "العنوانان متطابقان تمامًا لكن المحتوى مختلف تمامًا بينهما — هذا ليس تكرار محتوى ولا يخالف سياسة AdSense بحد ذاته، فلا داعٍ للدمج أو الحذف. يُستحسن فقط تمييز أحد العنوانين لتفادي التباس القارئ ومحركات البحث."
	default:
		return "مراجعة بشرية مطلوبة قبل أي قرار تحرير."
	}
}

func kindPriority(kind string) int {
	switch kind {
	case contentquality.SimilarityKindExact:
		return 4
	case contentquality.SimilarityKindNear:
		return 3
	case contentquality.SimilarityKindTemplate:
		return 2
	case contentquality.SimilarityKindTitleOnly:
		return 1
	default:
		return 0
	}
}
