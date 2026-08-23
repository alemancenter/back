package contentaudit

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/imanjo/fiber-api/internal/contentquality"
	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/models"
)

func loadInventorySources(ctx context.Context, country, contentType string) ([]inventorySource, error) {
	db := database.GetManager().GetByCode(country)
	articleFiles, postFiles := readinessFileCountMaps(ctx, db)
	items := make([]inventorySource, 0, 512)

	if contentType == "all" || contentType == "article" {
		var articles []models.Article
		if err := db.WithContext(ctx).
			Select("id", "title", "content", "meta_description", "status", "visit_count", "created_at", "updated_at").
			Find(&articles).Error; err != nil {
			return nil, err
		}
		for _, article := range articles {
			meta := ""
			if article.MetaDescription != nil {
				meta = *article.MetaDescription
			}
			items = append(items, inventorySource{
				ID: article.ID, Type: "article", Title: article.Title, Content: article.Content,
				MetaDescription: meta, Published: article.Status == 1, Visits: article.VisitCount,
				CreatedAt: article.CreatedAt, UpdatedAt: article.UpdatedAt, FilesCount: articleFiles[article.ID],
			})
		}
	}

	if contentType == "all" || contentType == "post" {
		var posts []models.Post
		if err := db.WithContext(ctx).
			Select("id", "title", "content", "meta_description", "keywords", "is_active", "views", "created_at", "updated_at").
			Find(&posts).Error; err != nil {
			return nil, err
		}
		for _, post := range posts {
			meta, keywords := "", ""
			if post.MetaDescription != nil {
				meta = *post.MetaDescription
			}
			if post.Keywords != nil {
				keywords = *post.Keywords
			}
			items = append(items, inventorySource{
				ID: post.ID, Type: "post", Title: post.Title, Content: post.Content,
				MetaDescription: meta, Keywords: keywords, Published: post.IsActive, Visits: post.Views,
				CreatedAt: post.CreatedAt, UpdatedAt: post.UpdatedAt, FilesCount: postFiles[post.ID],
			})
		}
	}
	return items, nil
}

func latestEditorialDecisionMap(ctx context.Context, country, contentType string) (map[uint]*models.ContentEditorialDecision, error) {
	var rows []models.ContentEditorialDecision
	if err := database.DB().WithContext(ctx).
		Where("country_code = ? AND content_type = ?", country, contentType).
		Order("created_at DESC, id DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	latest := make(map[uint]*models.ContentEditorialDecision, len(rows))
	for i := range rows {
		row := &rows[i]
		if row.ContentID == 0 {
			continue
		}
		if _, exists := latest[row.ContentID]; exists {
			continue
		}
		latest[row.ContentID] = row
	}
	return latest, nil
}

func inventorySimilarityMap(ctx context.Context, country, contentType string) (map[string]inventorySimilaritySignal, error) {
	documents, _, err := loadSimilarityDocuments(ctx, country, contentType)
	if err != nil {
		return nil, err
	}
	report := contentquality.DetectSimilarity(documents, contentquality.DefaultSimilarityOptions())
	signals := make(map[string]inventorySimilaritySignal)
	for _, cluster := range report.Clusters {
		for _, key := range cluster.Members {
			candidate := inventorySimilaritySignal{Kind: cluster.Kind, Similarity: cluster.MaxSimilarity, ClusterID: cluster.ID}
			current, exists := signals[key]
			if !exists || similarityClusterPriority(candidate.Kind) > similarityClusterPriority(current.Kind) ||
				(candidate.Kind == current.Kind && candidate.Similarity > current.Similarity) {
				signals[key] = candidate
			}
		}
	}
	return signals, nil
}

func buildInventoryItems(ctx context.Context, country, contentType string) ([]inventoryItem, error) {
	sources, err := loadInventorySources(ctx, country, contentType)
	if err != nil {
		return nil, err
	}
	similarity, err := inventorySimilarityMap(ctx, country, contentType)
	if err != nil {
		return nil, err
	}

	articleDecisions, postDecisions := map[uint]*models.ContentAIDecision{}, map[uint]*models.ContentAIDecision{}
	articleEditorial, postEditorial := map[uint]*models.ContentEditorialDecision{}, map[uint]*models.ContentEditorialDecision{}
	if contentType == "all" || contentType == "article" {
		articleDecisions, err = latestReadinessDecisions(ctx, "article", country)
		if err != nil { return nil, err }
		articleEditorial, err = latestEditorialDecisionMap(ctx, country, "article")
		if err != nil { return nil, err }
	}
	if contentType == "all" || contentType == "post" {
		postDecisions, err = latestReadinessDecisions(ctx, "post", country)
		if err != nil { return nil, err }
		postEditorial, err = latestEditorialDecisionMap(ctx, country, "post")
		if err != nil { return nil, err }
	}

	items := make([]inventoryItem, 0, len(sources))
	for _, source := range sources {
		var auditDecision *models.ContentAIDecision
		var editorial *models.ContentEditorialDecision
		if source.Type == "article" {
			auditDecision, editorial = articleDecisions[source.ID], articleEditorial[source.ID]
		} else {
			auditDecision, editorial = postDecisions[source.ID], postEditorial[source.ID]
		}

		gate := readinessGate(auditDecision, source.Title, source.Content, source.MetaDescription, source.Keywords)
		if editorial != nil {
			gate = contentquality.ApplyEditorialDecision(gate, editorial.Decision)
		}
		artifacts := contentquality.DetectReplacementArtifacts(
			contentquality.TextField{Name: "title", Value: source.Title},
			contentquality.TextField{Name: "content", Value: source.Content},
			contentquality.TextField{Name: "meta_description", Value: source.MetaDescription},
			contentquality.TextField{Name: "keywords", Value: source.Keywords},
		)
		readiness := buildUnifiedReadinessItem(source.Title, source.Content, source.MetaDescription, source.Keywords, source.FilesCount, source.Published, source.Type, source.ID, country, gate, nil)
		key := fmt.Sprintf("%s:%d", source.Type, source.ID)
		item := inventoryItem{
			ID: source.ID, Type: source.Type, Title: source.Title, Published: source.Published,
			Visits: source.Visits, WordCount: readiness.WordCount, FilesCount: source.FilesCount,
			ShouldIndex: readiness.ShouldIndex, AdsEligible: readiness.ShouldShowAds, Audited: readiness.Audited,
			AuditDecision: readiness.Decision, Risk: readiness.AdSenseRisk, Corrupted: len(artifacts) > 0,
			Similarity: similarity[key], Editorial: editorial, CreatedAt: source.CreatedAt, UpdatedAt: source.UpdatedAt,
		}
		if source.Type == "article" {
			item.PublicURL = fmt.Sprintf("/%s/lesson/articles/%d", country, source.ID)
			item.EditURL = fmt.Sprintf("/dashboard/articles/%d/edit", source.ID)
		} else {
			item.PublicURL = fmt.Sprintf("/%s/posts/%d", country, source.ID)
			item.EditURL = fmt.Sprintf("/dashboard/posts/%d/edit", source.ID)
		}
		item.PriorityScore, item.PriorityReasons, item.SuggestedDecision = inventoryPriority(item)
		items = append(items, item)
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].PriorityScore != items[j].PriorityScore { return items[i].PriorityScore > items[j].PriorityScore }
		if items[i].Visits != items[j].Visits { return items[i].Visits > items[j].Visits }
		if !items[i].UpdatedAt.Equal(items[j].UpdatedAt) { return items[i].UpdatedAt.After(items[j].UpdatedAt) }
		if items[i].Type != items[j].Type { return items[i].Type < items[j].Type }
		return items[i].ID < items[j].ID
	})
	return items, nil
}

func inventoryDecisionName(item inventoryItem) string {
	if item.Editorial == nil || strings.TrimSpace(item.Editorial.Decision) == "" {
		return models.EditorialDecisionUnclassified
	}
	return item.Editorial.Decision
}

func inventoryMatches(item inventoryItem, decision, signal, search string) bool {
	if decision != "all" && inventoryDecisionName(item) != decision { return false }
	switch signal {
	case "corruption":
		if !item.Corrupted { return false }
	case contentquality.SimilarityKindExact, contentquality.SimilarityKindNear, contentquality.SimilarityKindTemplate:
		if item.Similarity.Kind != signal { return false }
	case "noindex":
		if item.ShouldIndex { return false }
	case "unaudited":
		if item.Audited { return false }
	case models.AIDecisionNeedsFix:
		if item.AuditDecision != models.AIDecisionNeedsFix { return false }
	case "short_file":
		if item.FilesCount == 0 || item.WordCount >= 180 { return false }
	}
	if search != "" {
		search = strings.ToLower(strings.TrimSpace(search))
		id := strconv.FormatUint(uint64(item.ID), 10)
		if !strings.Contains(strings.ToLower(item.Title), search) && !strings.Contains(id, search) { return false }
	}
	return true
}

func summarizeInventory(items []inventoryItem) inventorySummary {
	summary := inventorySummary{TotalContent: len(items)}
	for index, item := range items {
		if index < 150 { summary.PriorityQueue++ }
		switch inventoryDecisionName(item) {
		case models.EditorialDecisionKeep:
			summary.Classified++; summary.Keep++
		case models.EditorialDecisionImprove:
			summary.Classified++; summary.Improve++
		case models.EditorialDecisionNoindex:
			summary.Classified++; summary.Noindex++
		case models.EditorialDecisionMerge301:
			summary.Classified++; summary.Merge301++
		default:
			summary.Unclassified++
		}
		if item.Corrupted { summary.Corrupted++ }
		switch item.Similarity.Kind {
		case contentquality.SimilarityKindExact: summary.ExactDuplicate++
		case contentquality.SimilarityKindNear: summary.NearDuplicate++
		case contentquality.SimilarityKindTemplate: summary.TemplateSimilar++
		}
		if !item.ShouldIndex { summary.NonIndexable++ }
		if item.AdsEligible { summary.AdsEligible++ }
	}
	return summary
}
