package contentaudit

import (
	"context"
	"sort"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
)

func (h *Handler) selectQualityBatchTargets(ctx context.Context, req contentQualityBatchRequest) ([]adsenseReadinessItem, error) {
	db := database.GetManager().GetByCode(req.CountryCode)
	articleFileCounts, postFileCounts := buildAdsenseFileCountMaps(ctx, db)
	rows := make([]adsenseReadinessRow, 0, req.Limit)

	if req.ContentType == "all" || req.ContentType == "article" {
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
			item := evaluateAdsenseItem(a.Title, a.Content, meta, articleFileCounts[a.ID], a.Status == 1, "article", a.ID, req.CountryCode)
			if shouldIncludeQualityTarget(item, req) {
				rows = append(rows, adsenseReadinessRow{Item: item, CreatedAt: a.CreatedAt})
			}
		}
	}

	if req.ContentType == "all" || req.ContentType == "post" {
		var posts []models.Post
		q := db.WithContext(ctx).
			Select("id", "title", "content", "meta_description", "is_active", "created_at").
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
			item := evaluateAdsenseItem(p.Title, p.Content, meta, postFileCounts[p.ID], p.IsActive, "post", p.ID, req.CountryCode)
			if shouldIncludeQualityTarget(item, req) {
				rows = append(rows, adsenseReadinessRow{Item: item, CreatedAt: p.CreatedAt})
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
	targets := make([]adsenseReadinessItem, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, row.Item)
	}
	return targets, nil
}

func shouldIncludeQualityTarget(item adsenseReadinessItem, req contentQualityBatchRequest) bool {
	if req.Source != "adsense_readiness" || req.Preset == "custom_filter" {
		return item.Level == req.Level
	}

	switch req.Preset {
	case "indexed_weak":
		return item.Level == "weak" && item.ShouldIndex
	case "short_file_pages":
		return item.FilesCount > 0 && item.WordCount < 180 && item.Level != "ready"
	case "weak_first":
		return item.Level == "weak" || (item.Level == "review" && item.Score < 70)
	default:
		return item.Level == req.Level
	}
}

func qualityTargetPriority(item adsenseReadinessItem, req contentQualityBatchRequest) int {
	priority := 100 - item.Score
	if item.ShouldIndex {
		priority += 35
	}
	if item.ShouldShowAds {
		priority += 20
	}
	if item.FilesCount > 0 {
		priority += 15
	}
	if item.WordCount < 120 {
		priority += 25
	} else if item.WordCount < 220 {
		priority += 12
	}
	if item.Level == "weak" {
		priority += 25
	}

	switch req.Preset {
	case "indexed_weak":
		if item.ShouldIndex {
			priority += 50
		}
	case "short_file_pages":
		if item.FilesCount > 0 && item.WordCount < 180 {
			priority += 70
		}
	case "weak_first":
		priority += 10
	}
	return priority
}
