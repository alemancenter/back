// Package adsensepolicy is a small, deliberately deterministic (no AI, no external calls)
// dashboard tool for spotting content that risks an AdSense content-policy rejection —
// starting with duplicate/near-duplicate content across articles and posts, the issue that
// previously got the site rejected. It reuses internal/contentquality's shingling/Jaccard
// similarity engine (the same one article/post Create/Update already gate new saves against in
// enforceUniqueContent) but runs the full O(n^2) cross-corpus scan that gate deliberately
// skips, over the *existing* published+draft library. Human-reviewed only: this never edits,
// unpublishes, or redirects anything by itself.
package adsensepolicy

import (
	"context"
	"fmt"
	"sort"
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
	ID        uint      `gorm:"column:id"`
	Title     string    `gorm:"column:title"`
	Content   string    `gorm:"column:content"`
	Published bool      `gorm:"column:published"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
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
	MaxSimilarity  float64                          `json:"max_similarity"`
	MinSimilarity  float64                          `json:"min_similarity"`
	Members        []Member                        `json:"members"`
	Pairs          []contentquality.SimilarityPair `json:"pairs"`
}

type ScanSummary struct {
	ScannedItems    int `json:"scanned_items"`
	IgnoredItems    int `json:"ignored_items"`
	TotalClusters   int `json:"total_clusters"`
	ExactClusters   int `json:"exact_clusters"`
	NearClusters    int `json:"near_clusters"`
	TemplateClusters int `json:"template_clusters"`
	AffectedItems   int `json:"affected_items"`
}

// ScanDuplicates runs a full duplicate/near-duplicate content scan across every article and
// post in the requesting country's database and returns every cluster found. Triggered
// on-demand (dashboard "فحص كامل" button) rather than on every page load — the underlying scan
// is O(n^2) in library size, same tradeoff the deleted content-audit tool made.
// @Summary Scan articles and posts for duplicate/near-duplicate content
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

	documents, members, err := loadDocuments(ctx, countryID, countryCode)
	if err != nil {
		return utils.InternalError(c, "تعذر تحميل المقالات والمنشورات للفحص")
	}

	options := contentquality.DefaultSimilarityOptions()
	report := contentquality.DetectSimilarity(documents, options)
	clusters := buildClusters(report.Clusters, members)
	summary := summarize(clusters, report)

	return utils.Success(c, "success", fiber.Map{
		"clusters": clusters,
		"summary":  summary,
		"policy": fiber.Map{
			"human_review_required": true,
			"automatic_changes":     false,
		},
	})
}

func loadDocuments(ctx context.Context, countryID database.CountryID, countryCode string) ([]contentquality.SimilarityDocument, map[string]Member, error) {
	db := database.DBForCountry(countryID).WithContext(ctx)
	documents := make([]contentquality.SimilarityDocument, 0)
	members := make(map[string]Member)

	var articleRows []contentRow
	if err := db.Raw(`SELECT id, title, content, (status = 1) AS published, updated_at FROM articles`).Scan(&articleRows).Error; err != nil {
		return nil, nil, err
	}
	for _, row := range articleRows {
		key := fmt.Sprintf("article:%d", row.ID)
		members[key] = Member{
			Key: key, ID: row.ID, Type: "article", Title: row.Title, Published: row.Published,
			UpdatedAt: row.UpdatedAt, WordCount: contentquality.SimilarityWordCount(row.Content),
			PublicURL: fmt.Sprintf("/%s/lesson/articles/%d", countryCode, row.ID),
			EditURL:   fmt.Sprintf("/dashboard/articles/%d/edit", row.ID),
		}
		documents = append(documents, contentquality.SimilarityDocument{Key: key, Title: row.Title, Content: row.Content})
	}

	var postRows []contentRow
	if err := db.Raw(`SELECT id, title, content, is_active AS published, updated_at FROM posts`).Scan(&postRows).Error; err != nil {
		return nil, nil, err
	}
	for _, row := range postRows {
		key := fmt.Sprintf("post:%d", row.ID)
		members[key] = Member{
			Key: key, ID: row.ID, Type: "post", Title: row.Title, Published: row.Published,
			UpdatedAt: row.UpdatedAt, WordCount: contentquality.SimilarityWordCount(row.Content),
			PublicURL: fmt.Sprintf("/%s/posts/%d", countryCode, row.ID),
			EditURL:   fmt.Sprintf("/dashboard/posts/%d/edit", row.ID),
		}
		documents = append(documents, contentquality.SimilarityDocument{Key: key, Title: row.Title, Content: row.Content})
	}

	return documents, members, nil
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
		items = append(items, Cluster{
			ID: cluster.ID, Kind: cluster.Kind, KindLabel: kindLabel(cluster.Kind),
			Recommendation: recommendation(cluster.Kind),
			MaxSimilarity:  cluster.MaxSimilarity, MinSimilarity: cluster.MinSimilarity,
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

func summarize(clusters []Cluster, report contentquality.SimilarityReport) ScanSummary {
	summary := ScanSummary{ScannedItems: report.ScannedDocuments, IgnoredItems: report.IgnoredDocuments, TotalClusters: len(clusters)}
	affected := make(map[string]struct{})
	for _, cluster := range clusters {
		switch cluster.Kind {
		case contentquality.SimilarityKindExact:
			summary.ExactClusters++
		case contentquality.SimilarityKindNear:
			summary.NearClusters++
		case contentquality.SimilarityKindTemplate:
			summary.TemplateClusters++
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
		return "تطابق كامل"
	case contentquality.SimilarityKindNear:
		return "تكرار محتوى (تشابه عالٍ)"
	case contentquality.SimilarityKindTemplate:
		return "تشابه قالبي"
	default:
		return kind
	}
}

func recommendation(kind string) string {
	switch kind {
	case contentquality.SimilarityKindExact:
		return "تطابق نصي كامل بعد التطبيع. راجع الصفحات يدويًا قبل أي دمج أو حذف أو إعادة توجيه."
	case contentquality.SimilarityKindNear:
		return "تشابه مرتفع جدًا في المحتوى. راجع الهدف والمرفقات ثم وحّد الصفحات أو أعد كتابة أحدهما."
	case contentquality.SimilarityKindTemplate:
		return "بنية قالبية متشابهة مع اختلافات محدودة. أعد كتابة المحتوى ليحمل قيمة فريدة قبل النشر."
	default:
		return "مراجعة بشرية مطلوبة قبل أي قرار تحرير."
	}
}

func kindPriority(kind string) int {
	switch kind {
	case contentquality.SimilarityKindExact:
		return 3
	case contentquality.SimilarityKindNear:
		return 2
	case contentquality.SimilarityKindTemplate:
		return 1
	default:
		return 0
	}
}

