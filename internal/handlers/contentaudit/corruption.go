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
	auditservice "github.com/imanjo/fiber-api/internal/services/contentaudit"
	"github.com/imanjo/fiber-api/internal/utils"
)

type corruptionCandidate struct {
	ID              uint      `gorm:"column:id"`
	Title           string    `gorm:"column:title"`
	Content         string    `gorm:"column:content"`
	MetaDescription string    `gorm:"column:meta_description"`
	Keywords        string    `gorm:"column:keywords"`
	Published       bool      `gorm:"column:published"`
	UpdatedAt       time.Time `gorm:"column:updated_at"`
}

type corruptionItem struct {
	ID          uint                                 `json:"id"`
	Type        string                               `json:"type"`
	Title       string                               `json:"title"`
	Published   bool                                 `json:"published"`
	CountryCode string                               `json:"country_code"`
	UpdatedAt   time.Time                            `json:"updated_at"`
	MatchCount  int                                  `json:"match_count"`
	Matches     []contentquality.ReplacementArtifact `json:"matches"`
	PublicURL   string                               `json:"public_url"`
	EditURL     string                               `json:"edit_url"`
}

type corruptionSummary struct {
	Total     int `json:"total"`
	Articles  int `json:"articles"`
	Posts     int `json:"posts"`
	Published int `json:"published"`
	Drafts    int `json:"drafts"`
	Matches   int `json:"matches"`
}

// ListCorruption scans current article/post source text for unresolved replacement
// artifacts. The route is registered behind AdminOnly(), so this operational view
// is available only to Admin and Super Admin accounts.
func (h *Handler) ListCorruption(c *fiber.Ctx) error {
	country, ok := corruptionCountry(c.Query("country", "jo"))
	if !ok {
		return utils.BadRequest(c, "invalid country")
	}
	contentType := strings.ToLower(strings.TrimSpace(c.Query("type", "all")))
	if contentType != "all" && contentType != "article" && contentType != "post" {
		return utils.BadRequest(c, "invalid content type")
	}
	q := strings.ToLower(strings.TrimSpace(c.Query("q")))
	page := queryPositiveInt(c.Query("page"), 1)
	perPage := queryPositiveInt(c.Query("per_page"), 25)
	if perPage > 100 {
		perPage = 100
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	items, err := scanCorruptedContent(ctx, country, contentType, q)
	if err != nil {
		return utils.InternalError(c, "failed to scan content corruption")
	}

	summary := summarizeCorruption(items)
	total := len(items)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	pages := 0
	if total > 0 {
		pages = (total + perPage - 1) / perPage
	}
	return utils.Success(c, "success", fiber.Map{
		"items":   items[start:end],
		"summary": summary,
		"meta": fiber.Map{
			"page":         page,
			"per_page":     perPage,
			"total":        total,
			"total_pages":  pages,
			"country_code": country,
		},
	})
}

// ShowCorruption re-checks one content item against the current database source.
// After an editor fixes the text, this endpoint immediately reports clean state.
func (h *Handler) ShowCorruption(c *fiber.Ctx) error {
	country, ok := corruptionCountry(c.Query("country", "jo"))
	if !ok {
		return utils.BadRequest(c, "invalid country")
	}
	contentType := strings.ToLower(strings.TrimSpace(c.Params("type")))
	if contentType != "article" && contentType != "post" {
		return utils.BadRequest(c, "invalid content type")
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil || id == 0 {
		return utils.BadRequest(c, "invalid content id")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	item, exists, err := loadCorruptionItem(ctx, country, contentType, uint(id))
	if err != nil {
		return utils.InternalError(c, "failed to re-check content corruption")
	}
	return utils.Success(c, "success", fiber.Map{"corrupted": exists, "item": item})
}

// AnalyzeCorruption launches the existing AI audit for one currently corrupted
// item, but keeps access under the stricter AdminOnly corruption route.
func (h *Handler) AnalyzeCorruption(c *fiber.Ctx) error {
	country, ok := corruptionCountry(c.Query("country", "jo"))
	if !ok {
		return utils.BadRequest(c, "invalid country")
	}
	contentType := strings.ToLower(strings.TrimSpace(c.Params("type")))
	if contentType != "article" && contentType != "post" {
		return utils.BadRequest(c, "invalid content type")
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil || id == 0 {
		return utils.BadRequest(c, "invalid content id")
	}

	checkCtx, checkCancel := context.WithTimeout(context.Background(), 15*time.Second)
	item, corrupted, checkErr := loadCorruptionItem(checkCtx, country, contentType, uint(id))
	checkCancel()
	if checkErr != nil {
		return utils.InternalError(c, "failed to verify content corruption")
	}
	if !corrupted || item == nil {
		return utils.BadRequest(c, "content no longer contains replacement artifacts")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	decision, err := h.svc.AnalyzeWithAI(ctx, auditservice.AIAnalyzeRequest{
		ContentType: contentType,
		ContentID:   strconv.FormatUint(id, 10),
		CountryCode: country,
	}, currentUserID(c))
	if err != nil {
		return utils.InternalError(c, "failed to analyze corrupted content")
	}
	return utils.Created(c, "AI decision created", fiber.Map{"decision": decision, "corruption": item})
}

func scanCorruptedContent(ctx context.Context, country, contentType, q string) ([]corruptionItem, error) {
	items := make([]corruptionItem, 0)
	if contentType == "all" || contentType == "article" {
		rows, err := corruptionCandidates(ctx, country, "article")
		if err != nil {
			return nil, err
		}
		items = append(items, corruptionItemsFromCandidates(country, "article", rows, q)...)
	}
	if contentType == "all" || contentType == "post" {
		rows, err := corruptionCandidates(ctx, country, "post")
		if err != nil {
			return nil, err
		}
		items = append(items, corruptionItemsFromCandidates(country, "post", rows, q)...)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			if items[i].Type == items[j].Type {
				return items[i].ID > items[j].ID
			}
			return items[i].Type < items[j].Type
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	return items, nil
}

func corruptionCandidates(ctx context.Context, country, contentType string) ([]corruptionCandidate, error) {
	db := database.GetManager().GetByCode(country).WithContext(ctx)
	rows := make([]corruptionCandidate, 0)
	var err error
	if contentType == "article" {
		err = db.Raw(`SELECT id, title, content, COALESCE(meta_description, '') AS meta_description,
			'' AS keywords, (status = 1) AS published, updated_at
			FROM articles
			WHERE title LIKE '%$%' OR content LIKE '%$%' OR COALESCE(meta_description, '') LIKE '%$%'`).Scan(&rows).Error
	} else {
		err = db.Raw(`SELECT id, title, content, COALESCE(meta_description, '') AS meta_description,
			COALESCE(keywords, '') AS keywords, is_active AS published, updated_at
			FROM posts
			WHERE title LIKE '%$%' OR content LIKE '%$%' OR COALESCE(meta_description, '') LIKE '%$%' OR COALESCE(keywords, '') LIKE '%$%'`).Scan(&rows).Error
	}
	return rows, err
}

func corruptionItemsFromCandidates(country, contentType string, rows []corruptionCandidate, q string) []corruptionItem {
	items := make([]corruptionItem, 0)
	for _, row := range rows {
		if q != "" && !strings.Contains(strings.ToLower(row.Title), q) && !strings.Contains(strconv.FormatUint(uint64(row.ID), 10), q) {
			continue
		}
		matches := contentquality.DetectReplacementArtifacts(
			contentquality.TextField{Name: "title", Value: row.Title},
			contentquality.TextField{Name: "content", Value: row.Content},
			contentquality.TextField{Name: "meta_description", Value: row.MetaDescription},
			contentquality.TextField{Name: "keywords", Value: row.Keywords},
		)
		if len(matches) == 0 {
			continue
		}
		item := corruptionItem{
			ID: row.ID, Type: contentType, Title: row.Title, Published: row.Published,
			CountryCode: country, UpdatedAt: row.UpdatedAt, MatchCount: len(matches), Matches: matches,
		}
		if contentType == "article" {
			item.PublicURL = fmt.Sprintf("/%s/lesson/articles/%d", country, row.ID)
			item.EditURL = fmt.Sprintf("/dashboard/articles/%d/edit", row.ID)
		} else {
			item.PublicURL = fmt.Sprintf("/%s/posts/%d", country, row.ID)
			item.EditURL = fmt.Sprintf("/dashboard/posts/%d/edit", row.ID)
		}
		items = append(items, item)
	}
	return items
}

func loadCorruptionItem(ctx context.Context, country, contentType string, id uint) (*corruptionItem, bool, error) {
	db := database.GetManager().GetByCode(country).WithContext(ctx)
	var row corruptionCandidate
	var resultErr error
	if contentType == "article" {
		resultErr = db.Raw(`SELECT id, title, content, COALESCE(meta_description, '') AS meta_description,
			'' AS keywords, (status = 1) AS published, updated_at FROM articles WHERE id = ?`, id).Scan(&row).Error
	} else {
		resultErr = db.Raw(`SELECT id, title, content, COALESCE(meta_description, '') AS meta_description,
			COALESCE(keywords, '') AS keywords, is_active AS published, updated_at FROM posts WHERE id = ?`, id).Scan(&row).Error
	}
	if resultErr != nil {
		return nil, false, resultErr
	}
	if row.ID == 0 {
		return nil, false, nil
	}
	items := corruptionItemsFromCandidates(country, contentType, []corruptionCandidate{row}, "")
	if len(items) == 0 {
		return nil, false, nil
	}
	return &items[0], true, nil
}

func summarizeCorruption(items []corruptionItem) corruptionSummary {
	summary := corruptionSummary{Total: len(items)}
	for _, item := range items {
		if item.Type == "article" {
			summary.Articles++
		} else if item.Type == "post" {
			summary.Posts++
		}
		if item.Published {
			summary.Published++
		} else {
			summary.Drafts++
		}
		summary.Matches += item.MatchCount
	}
	return summary
}

func corruptionCountry(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "jo":
		return "jo", true
	case "sa":
		return "sa", true
	case "eg":
		return "eg", true
	case "ps":
		return "ps", true
	default:
		return "", false
	}
}

func queryPositiveInt(value string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 1 {
		return fallback
	}
	return n
}
