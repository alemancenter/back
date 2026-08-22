package contentaudit

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/imanjo/fiber-api/internal/contentquality"
	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/utils"
)

type similarityCandidate struct {
	ID        uint      `gorm:"column:id"`
	Title     string    `gorm:"column:title"`
	Content   string    `gorm:"column:content"`
	Published bool      `gorm:"column:published"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

type similarityMember struct {
	Key         string    `json:"key"`
	ID          uint      `json:"id"`
	Type        string    `json:"type"`
	Title       string    `json:"title"`
	Published   bool      `json:"published"`
	UpdatedAt   time.Time `json:"updated_at"`
	WordCount   int       `json:"word_count"`
	PublicURL   string    `json:"public_url"`
	EditURL     string    `json:"edit_url"`
}

type similarityClusterItem struct {
	ID              string                          `json:"id"`
	Kind            string                          `json:"kind"`
	MaxSimilarity   float64                         `json:"max_similarity"`
	MinSimilarity   float64                         `json:"min_similarity"`
	PairCount       int                             `json:"pair_count"`
	Members         []similarityMember              `json:"members"`
	Pairs           []contentquality.SimilarityPair `json:"pairs"`
	Recommendation  string                          `json:"recommendation"`
	CrossContentType bool                            `json:"cross_content_type"`
}

type similaritySummary struct {
	TotalClusters     int `json:"total_clusters"`
	ExactClusters     int `json:"exact_clusters"`
	NearClusters      int `json:"near_clusters"`
	TemplateClusters  int `json:"template_clusters"`
	AffectedItems     int `json:"affected_items"`
	PublishedItems    int `json:"published_items"`
	CrossTypeClusters int `json:"cross_type_clusters"`
	ScannedItems      int `json:"scanned_items"`
	IgnoredShortItems int `json:"ignored_short_items"`
}

// ListSimilarity performs a deterministic, review-only similarity scan across
// current article/post source content. It never changes indexing, ads, redirects,
// publication state, or source text. Any merge/301 decision remains explicitly
// human-controlled in the later editorial review phase.
func (h *Handler) ListSimilarity(c *fiber.Ctx) error {
	country, ok := corruptionCountry(c.Query("country", "jo"))
	if !ok {
		return utils.BadRequest(c, "invalid country")
	}
	contentType := strings.ToLower(strings.TrimSpace(c.Query("type", "all")))
	if contentType != "all" && contentType != "article" && contentType != "post" {
		return utils.BadRequest(c, "invalid content type")
	}
	kind := strings.ToLower(strings.TrimSpace(c.Query("kind", "all")))
	if kind != "all" && kind != contentquality.SimilarityKindExact && kind != contentquality.SimilarityKindNear && kind != contentquality.SimilarityKindTemplate {
		return utils.BadRequest(c, "invalid similarity kind")
	}
	q := strings.ToLower(strings.TrimSpace(c.Query("q")))
	page := queryPositiveInt(c.Query("page"), 1)
	perPage := queryPositiveInt(c.Query("per_page"), 20)
	if perPage > 100 {
		perPage = 100
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	documents, memberMap, err := loadSimilarityDocuments(ctx, country, contentType)
	if err != nil {
		return utils.InternalError(c, "failed to load content for similarity scan")
	}
	options := contentquality.DefaultSimilarityOptions()
	report := contentquality.DetectSimilarity(documents, options)
	clusters := similarityClustersFromReport(report.Clusters, memberMap, kind, q)
	summary := summarizeSimilarity(clusters, report)

	total := len(clusters)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	totalPages := 0
	if total > 0 {
		totalPages = (total + perPage - 1) / perPage
	}

	return utils.Success(c, "success", fiber.Map{
		"clusters": clusters[start:end],
		"summary":  summary,
		"meta": fiber.Map{
			"page":         page,
			"per_page":     perPage,
			"total":        total,
			"total_pages":  totalPages,
			"country_code": country,
		},
		"policy": fiber.Map{
			"human_review_required": true,
			"automatic_redirects":   false,
			"automatic_deletes":     false,
			"automatic_noindex":     false,
		},
		"thresholds": fiber.Map{
			"minimum_words":               options.MinWords,
			"shingle_size":                options.ShingleSize,
			"near_similarity":             options.NearThreshold,
			"template_similarity":         options.TemplateThreshold,
			"template_containment":        options.TemplateContainment,
			"minimum_rare_similarity":     options.MinRareJaccard,
			"minimum_shared_rare_shingles": options.MinSharedRareShingles,
		},
	})
}

func loadSimilarityDocuments(ctx context.Context, country, contentType string) ([]contentquality.SimilarityDocument, map[string]similarityMember, error) {
	db := database.GetManager().GetByCode(country).WithContext(ctx)
	documents := make([]contentquality.SimilarityDocument, 0)
	members := make(map[string]similarityMember)

	appendRows := func(kind string, rows []similarityCandidate) {
		for _, row := range rows {
			key := fmt.Sprintf("%s:%d", kind, row.ID)
			member := similarityMember{
				Key: key, ID: row.ID, Type: kind, Title: row.Title, Published: row.Published,
				UpdatedAt: row.UpdatedAt, WordCount: contentquality.SimilarityWordCount(row.Content),
			}
			if kind == "article" {
				member.PublicURL = fmt.Sprintf("/%s/lesson/articles/%d", country, row.ID)
				member.EditURL = fmt.Sprintf("/dashboard/articles/%d/edit", row.ID)
			} else {
				member.PublicURL = fmt.Sprintf("/%s/posts/%d", country, row.ID)
				member.EditURL = fmt.Sprintf("/dashboard/posts/%d/edit", row.ID)
			}
			members[key] = member
			documents = append(documents, contentquality.SimilarityDocument{Key: key, Title: row.Title, Content: row.Content})
		}
	}

	if contentType == "all" || contentType == "article" {
		rows := make([]similarityCandidate, 0)
		if err := db.Raw(`SELECT id, title, content, (status = 1) AS published, updated_at FROM articles`).Scan(&rows).Error; err != nil {
			return nil, nil, err
		}
		appendRows("article", rows)
	}
	if contentType == "all" || contentType == "post" {
		rows := make([]similarityCandidate, 0)
		if err := db.Raw(`SELECT id, title, content, is_active AS published, updated_at FROM posts`).Scan(&rows).Error; err != nil {
			return nil, nil, err
		}
		appendRows("post", rows)
	}
	return documents, members, nil
}

func similarityClustersFromReport(clusters []contentquality.SimilarityCluster, memberMap map[string]similarityMember, kind, q string) []similarityClusterItem {
	items := make([]similarityClusterItem, 0, len(clusters))
	for _, cluster := range clusters {
		if kind != "all" && cluster.Kind != kind {
			continue
		}
		members := make([]similarityMember, 0, len(cluster.Members))
		matchesQuery := q == ""
		types := make(map[string]struct{})
		for _, key := range cluster.Members {
			member, ok := memberMap[key]
			if !ok {
				continue
			}
			members = append(members, member)
			types[member.Type] = struct{}{}
			if q != "" && (strings.Contains(strings.ToLower(member.Title), q) || strings.Contains(strconv.FormatUint(uint64(member.ID), 10), q) || strings.Contains(strings.ToLower(member.Key), q)) {
				matchesQuery = true
			}
		}
		if len(members) < 2 || !matchesQuery {
			continue
		}
		sort.SliceStable(members, func(i, j int) bool {
			if members[i].Published != members[j].Published {
				return members[i].Published
			}
			if !members[i].UpdatedAt.Equal(members[j].UpdatedAt) {
				return members[i].UpdatedAt.After(members[j].UpdatedAt)
			}
			if members[i].Type != members[j].Type {
				return members[i].Type < members[j].Type
			}
			return members[i].ID < members[j].ID
		})
		items = append(items, similarityClusterItem{
			ID: cluster.ID, Kind: cluster.Kind, MaxSimilarity: cluster.MaxSimilarity, MinSimilarity: cluster.MinSimilarity,
			PairCount: len(cluster.Pairs), Members: members, Pairs: cluster.Pairs,
			Recommendation: similarityRecommendation(cluster.Kind), CrossContentType: len(types) > 1,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		leftPriority := similarityClusterPriority(items[i].Kind)
		rightPriority := similarityClusterPriority(items[j].Kind)
		if leftPriority != rightPriority {
			return leftPriority > rightPriority
		}
		if items[i].MaxSimilarity != items[j].MaxSimilarity {
			return items[i].MaxSimilarity > items[j].MaxSimilarity
		}
		return items[i].ID < items[j].ID
	})
	return items
}

func summarizeSimilarity(clusters []similarityClusterItem, report contentquality.SimilarityReport) similaritySummary {
	summary := similaritySummary{TotalClusters: len(clusters), ScannedItems: report.ScannedDocuments, IgnoredShortItems: report.IgnoredDocuments}
	affected := make(map[string]similarityMember)
	for _, cluster := range clusters {
		switch cluster.Kind {
		case contentquality.SimilarityKindExact:
			summary.ExactClusters++
		case contentquality.SimilarityKindNear:
			summary.NearClusters++
		case contentquality.SimilarityKindTemplate:
			summary.TemplateClusters++
		}
		if cluster.CrossContentType {
			summary.CrossTypeClusters++
		}
		for _, member := range cluster.Members {
			affected[member.Key] = member
		}
	}
	summary.AffectedItems = len(affected)
	for _, member := range affected {
		if member.Published {
			summary.PublishedItems++
		}
	}
	return summary
}

func similarityRecommendation(kind string) string {
	switch kind {
	case contentquality.SimilarityKindExact:
		return "تطابق نصي كامل بعد التطبيع. راجع الصفحات يدويًا واختر الصفحة الأساسية قبل أي دمج أو 301."
	case contentquality.SimilarityKindNear:
		return "تشابه مرتفع في المحتوى. قارن الهدف التعليمي والمرفقات، ثم وحّد الصفحات أو ميّز المحتوى إذا كانت النية مختلفة."
	case contentquality.SimilarityKindTemplate:
		return "بنية قالبية متشابهة مع اختلافات محدودة. راجع القيمة التعليمية الفريدة وأعد كتابة الصفحات القالبية قبل التفكير في الدمج."
	default:
		return "مراجعة بشرية مطلوبة قبل أي قرار تحرير أو إعادة توجيه."
	}
}

func similarityClusterPriority(kind string) int {
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
