package contentaudit

import (
	"context"
	"errors"
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

const inventoryPriorityLimit = 150

type inventoryClassifyRequest struct {
	Decision          string `json:"decision" form:"decision"`
	Notes             string `json:"notes" form:"notes"`
	TargetContentType string `json:"target_content_type" form:"target_content_type"`
	TargetContentID   uint   `json:"target_content_id" form:"target_content_id"`
}

func (h *Handler) ListInventory(c *fiber.Ctx) error {
	country, ok := corruptionCountry(c.Query("country", "jo"))
	if !ok { return utils.BadRequest(c, "invalid country") }
	contentType := strings.ToLower(strings.TrimSpace(c.Query("type", "all")))
	if contentType != "all" && contentType != "article" && contentType != "post" {
		return utils.BadRequest(c, "invalid content type")
	}
	decision := strings.ToLower(strings.TrimSpace(c.Query("decision", "all")))
	if decision != "all" && auditservice.NormalizeEditorialDecision(decision) == "" {
		return utils.BadRequest(c, "invalid editorial decision")
	}
	signal := strings.ToLower(strings.TrimSpace(c.Query("signal", "all")))
	if !validInventorySignal(signal) { return utils.BadRequest(c, "invalid inventory signal") }
	scope := strings.ToLower(strings.TrimSpace(c.Query("scope", "priority")))
	if scope != "priority" && scope != "all" { return utils.BadRequest(c, "invalid inventory scope") }
	search := strings.TrimSpace(c.Query("q"))
	page := queryPositiveInt(c.Query("page"), 1)
	perPage := queryPositiveInt(c.Query("per_page"), 25)
	if perPage > 50 { perPage = 50 }

	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	allItems, err := buildInventoryItems(ctx, country, contentType)
	if err != nil { return utils.InternalError(c, "failed to build content inventory") }
	globalSummary := summarizeInventory(allItems)

	filtered := make([]inventoryItem, 0, len(allItems))
	for _, item := range allItems {
		if inventoryMatches(item, decision, signal, search) { filtered = append(filtered, item) }
	}
	matchedBeforeScope := len(filtered)
	if scope == "priority" && len(filtered) > inventoryPriorityLimit {
		filtered = filtered[:inventoryPriorityLimit]
	}

	total := len(filtered)
	start := (page - 1) * perPage
	if start > total { start = total }
	end := start + perPage
	if end > total { end = total }
	totalPages := 0
	if total > 0 { totalPages = (total + perPage - 1) / perPage }

	return utils.Success(c, "success", fiber.Map{
		"items": filtered[start:end],
		"summary": globalSummary,
		"meta": fiber.Map{
			"page": page, "per_page": perPage, "total": total, "total_pages": totalPages,
			"matched_before_scope": matchedBeforeScope, "scope": scope, "priority_limit": inventoryPriorityLimit,
			"country_code": country,
		},
		"policy": fiber.Map{
			"classification_is_human": true,
			"noindex_is_enforced": true,
			"merge_301_is_pending": true,
			"automatic_delete": false,
			"automatic_redirect": false,
			"priority_is_internal_heuristic": true,
		},
	})
}

func (h *Handler) ClassifyInventoryItem(c *fiber.Ctx) error {
	country, ok := corruptionCountry(c.Query("country", "jo"))
	if !ok { return utils.BadRequest(c, "invalid country") }
	contentType := strings.ToLower(strings.TrimSpace(c.Params("type")))
	if contentType != "article" && contentType != "post" { return utils.BadRequest(c, "invalid content type") }
	id64, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil || id64 == 0 { return utils.BadRequest(c, "invalid content id") }
	id := uint(id64)

	var req inventoryClassifyRequest
	if err := c.BodyParser(&req); err != nil { return utils.BadRequest(c, "invalid classification payload") }
	req.Decision = auditservice.NormalizeEditorialDecision(req.Decision)
	if req.Decision == "" { return utils.BadRequest(c, "invalid editorial decision") }
	req.Notes = strings.TrimSpace(req.Notes)
	if len([]rune(req.Notes)) > 4000 { return utils.BadRequest(c, "notes are too long") }

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	exists, _, err := inventoryContentExists(ctx, country, contentType, id)
	if err != nil { return utils.InternalError(c, "failed to verify content") }
	if !exists { return utils.NotFound(c) }

	var targetType *string
	var targetID *uint
	if req.Decision == models.EditorialDecisionMerge301 {
		target := strings.ToLower(strings.TrimSpace(req.TargetContentType))
		if target != "article" && target != "post" || req.TargetContentID == 0 {
			return utils.BadRequest(c, "MERGE_301 requires a valid target content type and id")
		}
		if target == contentType && req.TargetContentID == id {
			return utils.BadRequest(c, "merge target cannot be the same content item")
		}
		targetExists, targetPublished, verifyErr := inventoryContentExists(ctx, country, target, req.TargetContentID)
		if verifyErr != nil { return utils.InternalError(c, "failed to verify merge target") }
		if !targetExists { return utils.BadRequest(c, "merge target does not exist") }
		if !targetPublished { return utils.BadRequest(c, "merge target must be published before a 301 plan is saved") }
		targetType, targetID = &target, &req.TargetContentID
	}

	decision := &models.ContentEditorialDecision{
		CountryCode: country, ContentType: contentType, ContentID: id, Decision: req.Decision,
		TargetContentType: targetType, TargetContentID: targetID, Notes: req.Notes, CreatedByUserID: currentUserID(c),
	}
	if err := h.svc.SaveEditorialDecision(ctx, decision); err != nil {
		if errors.Is(err, auditservice.ErrInvalidEditorialDecision) { return utils.BadRequest(c, err.Error()) }
		return utils.InternalError(c, "failed to save editorial classification")
	}

	return utils.Created(c, "editorial classification saved", fiber.Map{
		"decision": decision,
		"seo_effect": map[bool]string{true: "noindex", false: "none"}[req.Decision == models.EditorialDecisionNoindex],
		"merge_pending": req.Decision == models.EditorialDecisionMerge301,
		"redirect_applied": false,
		"sitemap_refresh_required": true,
	})
}

func (h *Handler) InventoryHistory(c *fiber.Ctx) error {
	country, ok := corruptionCountry(c.Query("country", "jo"))
	if !ok { return utils.BadRequest(c, "invalid country") }
	contentType := strings.ToLower(strings.TrimSpace(c.Params("type")))
	if contentType != "article" && contentType != "post" { return utils.BadRequest(c, "invalid content type") }
	id64, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil || id64 == 0 { return utils.BadRequest(c, "invalid content id") }
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	history, err := h.svc.EditorialHistory(ctx, contentType, uint(id64), country, 50)
	if err != nil { return utils.InternalError(c, "failed to load editorial history") }
	return utils.Success(c, "success", history)
}

func validInventorySignal(value string) bool {
	switch value {
	case "all", "corruption", contentquality.SimilarityKindExact, contentquality.SimilarityKindNear,
		contentquality.SimilarityKindTemplate, "noindex", "unaudited", models.AIDecisionNeedsFix, "short_file":
		return true
	default:
		return false
	}
}

func inventoryContentExists(ctx context.Context, country, contentType string, id uint) (bool, bool, error) {
	db := database.GetManager().GetByCode(country).WithContext(ctx)
	type row struct { ID uint; Published bool }
	var result row
	var err error
	if contentType == "article" {
		err = db.Raw("SELECT id, (status = 1) AS published FROM articles WHERE id = ? LIMIT 1", id).Scan(&result).Error
	} else if contentType == "post" {
		err = db.Raw("SELECT id, is_active AS published FROM posts WHERE id = ? LIMIT 1", id).Scan(&result).Error
	} else {
		return false, false, gorm.ErrRecordNotFound
	}
	if err != nil { return false, false, err }
	return result.ID != 0, result.Published, nil
}
