package contentaudit

import (
	"context"
	"sort"

	"github.com/imanjo/fiber-api/internal/contentquality"
	"github.com/imanjo/fiber-api/internal/database"
	"github.com/imanjo/fiber-api/internal/models"
)

func (h *Handler) selectQualityBatchTargets(ctx context.Context, req contentQualityBatchRequest) ([]unifiedReadinessItem, error) {
	db := database.GetManager().GetByCode(req.CountryCode)
	articleFileCounts, postFileCounts := readinessFileCountMaps(ctx, db)
	rows := make([]unifiedReadinessRow, 0, req.Limit)

	if req.ContentType == "all" || req.ContentType == "article" {
		decisions, err := latestReadinessDecisions(ctx, "article", req.CountryCode)
		if err != nil {
			return nil, err
		}
		var articles []models.Article
		q := db.WithContext(ctx).
			Select("id", "title", "content", "meta_description", "status", "created_at").
			Order("created_at DESC")
		if req.Query != "" {
			q = q.Where("title LIKE ?", "%"+req.Query+"%")
		}
		if err := q.Find(&articles).Error; err != nil {
			return nil, err
		}
		for _, a := range articles {
			meta := ""
			if a.MetaDescription != nil {
				meta = *a.MetaDescription
			}
			gate := readinessGate(decisions[a.ID], a.Title, a.Content, meta, "")
			item := buildUnifiedReadinessItem(a.Title, a.Content, meta, "", articleFileCounts[a.ID], a.Status == 1, "article", a.ID, req.CountryCode, gate)
			if shouldIncludeQualityTarget(item, req) {
				rows = append(rows, unifiedReadinessRow{Item: item, CreatedAt: a.CreatedAt})
			}
		}
	}

	if req.ContentType == "all" || req.ContentType == "post" {
		decisions, err := latestReadinessDecisions(ctx, "post", req.CountryCode)
		if err != nil {
			return nil, err
		}
		var posts []models.Post
		q := db.WithContext(ctx).
			Select("id", "title", "content", "meta_description", "keywords", "is_active", "created_at").
			Order("created_at DESC")
		if req.Query != "" {
			q = q.Where("title LIKE ?", "%"+req.Query+"%")
		}
		if err := q.Find(&posts).Error; err != nil {
			return nil, err
		}
		for _, p := range posts {
			meta := ""
			if p.MetaDescription != nil {
				meta = *p.MetaDescription
			}
			keywords := ""
			if p.Keywords != nil {
				keywords = *p.Keywords
			}
			gate := readinessGate(decisions[p.ID], p.Title, p.Content, meta, keywords)
			item := buildUnifiedReadinessItem(p.Title, p.Content, meta, keywords, postFileCounts[p.ID], p.IsActive, "post", p.ID, req.CountryCode, gate)
			if shouldIncludeQualityTarget(item, req) {
				rows = append(rows, unifiedReadinessRow{Item: item, CreatedAt: p.CreatedAt})
			}
		}
	}

	sort.SliceStable(rows, func(i, j int) bool {
		leftPriority := qualityTargetPriority(rows[i].Item, req)
		rightPriority := qualityTargetPriority(rows[j].Item, req)
		if leftPriority == rightPriority {
			if rows[i].Item.Score == rows[j].Item.Score {
				return rows[i].CreatedAt.After(rows[j].CreatedAt)
			}
			return rows[i].Item.Score < rows[j].Item.Score
		}
		return leftPriority > rightPriority
	})
	if len(rows) > req.Limit {
		rows = rows[:req.Limit]
	}
	targets := make([]unifiedReadinessItem, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, row.Item)
	}
	return targets, nil
}

// Batch presets choose editorial work; they do not make SEO/ad decisions.
// should_index/should_show_ads already come from the canonical Content Quality Gate.
func shouldIncludeQualityTarget(item unifiedReadinessItem, req contentQualityBatchRequest) bool {
	if req.Source != "adsense_readiness" || req.Preset == "custom_filter" {
		return item.Level == req.Level
	}

	switch req.Preset {
	case "indexed_weak":
		// Historically "weak" came from a separate score. Now target pages that
		// remain indexable but are not ad eligible and still need editorial work.
		return item.ShouldIndex && !item.ShouldShowAds && (!item.Audited || len(item.DiagnosticSignals) > 0)
	case "short_file_pages":
		return item.FilesCount > 0 && item.WordCount < contentquality.DiagnosticShortFileMaxWords && !item.ShouldShowAds
	case "weak_first":
		return !item.ShouldShowAds
	default:
		return item.Level == req.Level
	}
}

func qualityTargetPriority(item unifiedReadinessItem, req contentQualityBatchRequest) int {
	priority := 0
	if !item.ShouldIndex {
		priority += 100
	}
	if !item.ShouldShowAds {
		priority += 50
	}
	if !item.Audited {
		priority += 35
	}
	priority += len(item.DiagnosticSignals) * 10
	if item.FilesCount > 0 {
		priority += 10
	}

	switch req.Preset {
	case "indexed_weak":
		if item.ShouldIndex && !item.ShouldShowAds {
			priority += 60
		}
	case "short_file_pages":
		if item.FilesCount > 0 && item.WordCount < contentquality.DiagnosticShortFileMaxWords {
			priority += 70
		}
	case "weak_first":
		if item.Level == "weak" {
			priority += 50
		}
	}
	return priority
}
