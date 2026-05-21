package contentaudit

import (
	"math"
	"strings"
	"time"

	"github.com/alemancenter/fiber-api/internal/database"
	"github.com/alemancenter/fiber-api/internal/models"
	"github.com/alemancenter/fiber-api/internal/utils"
	"github.com/gofiber/fiber/v2"
)

type contentAIReviewQueueItem struct {
	ID               uint       `json:"id"`
	DecisionID       uint       `json:"decision_id"`
	ContentType      string     `json:"content_type"`
	ContentID        string     `json:"content_id"`
	CountryCode      string     `json:"country_code"`
	OriginalTitle    string     `json:"original_title"`
	FixedTitle       string     `json:"fixed_title"`
	FixSummary       string     `json:"fix_summary"`
	Status           string     `json:"status"`
	Score            int        `json:"score"`
	Decision         string     `json:"decision"`
	AdSenseRisk      string     `json:"adsense_risk"`
	Model            string     `json:"model"`
	ModelStrategy    string     `json:"model_strategy,omitempty"`
	ProcessingTimeMS int64      `json:"processing_time_ms"`
	AppliedAt        *time.Time `json:"applied_at,omitempty"`
	RejectedAt       *time.Time `json:"rejected_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type contentAIModelCostSummary struct {
	TotalRuns     int64   `json:"total_runs"`
	SuccessRuns   int64   `json:"success_runs"`
	FailedRuns    int64   `json:"failed_runs"`
	InputTokens   int64   `json:"input_tokens"`
	OutputTokens  int64   `json:"output_tokens"`
	EstimatedCost float64 `json:"estimated_cost_usd"`
	AvgDurationMS float64 `json:"avg_duration_ms"`
}

type contentAIModelCostByModel struct {
	Model         string  `json:"model"`
	Strategy      string  `json:"model_strategy"`
	TaskType      string  `json:"task_type"`
	Runs          int64   `json:"runs"`
	SuccessRuns   int64   `json:"success_runs"`
	FailedRuns    int64   `json:"failed_runs"`
	InputTokens   int64   `json:"input_tokens"`
	OutputTokens  int64   `json:"output_tokens"`
	EstimatedCost float64 `json:"estimated_cost_usd"`
	AvgDurationMS float64 `json:"avg_duration_ms"`
}

type contentAIModelCostResponse struct {
	Summary contentAIModelCostSummary   `json:"summary"`
	Models  []contentAIModelCostByModel `json:"models"`
}

func contentAIReviewQueueSelect() string {
	parts := []string{
		"p.id",
		"p.decision_id",
		"p.content_type",
		"p.content_id",
		"p.country_code",
		"p.original_title",
		"p.fixed_title",
		"p.fix_summary",
		"p.status",
		"p.applied_at",
		"p.rejected_at",
		"p.created_at",
		"p.updated_at",
	}
	column := func(name string) bool {
		return database.DB().Migrator().HasColumn(&models.ContentAIDecision{}, name)
	}
	if column("score") {
		parts = append(parts, "COALESCE(d.score, 0) AS score")
	} else {
		parts = append(parts, "0 AS score")
	}
	if column("decision") {
		parts = append(parts, "COALESCE(d.decision, '') AS decision")
	} else {
		parts = append(parts, "'' AS decision")
	}
	if column("adsense_risk") {
		parts = append(parts, "COALESCE(d.adsense_risk, '') AS adsense_risk")
	} else {
		parts = append(parts, "'' AS adsense_risk")
	}
	if column("model") {
		parts = append(parts, "COALESCE(d.model, '') AS model")
	} else {
		parts = append(parts, "'' AS model")
	}
	if column("prompt_version") {
		parts = append(parts, "COALESCE(d.prompt_version, '') AS model_strategy")
	} else {
		parts = append(parts, "'' AS model_strategy")
	}
	if column("processing_time_ms") {
		parts = append(parts, "COALESCE(d.processing_time_ms, 0) AS processing_time_ms")
	} else {
		parts = append(parts, "0 AS processing_time_ms")
	}
	return strings.Join(parts, ", ")
}

func (h *Handler) ListReviewQueue(c *fiber.Ctx) error {
	page := c.QueryInt("page", 1)
	if page < 1 {
		page = 1
	}
	perPage := c.QueryInt("per_page", 25)
	if perPage < 1 {
		perPage = 25
	}
	if perPage > 100 {
		perPage = 100
	}
	status := strings.ToLower(strings.TrimSpace(c.Query("status", models.AIFixStatusPreviewed)))
	if status != "all" && status != models.AIFixStatusPreviewed && status != models.AIFixStatusApplied && status != models.AIFixStatusRejected && status != "failed" && status != "pending" {
		status = models.AIFixStatusPreviewed
	}
	contentType := strings.ToLower(strings.TrimSpace(c.Query("content_type", "all")))
	if contentType != "article" && contentType != "post" && contentType != "all" {
		contentType = "all"
	}
	q := strings.TrimSpace(c.Query("q"))

	query := database.DB().Table("content_ai_fix_previews AS p").
		Select(contentAIReviewQueueSelect()).
		Joins("LEFT JOIN content_ai_decisions d ON d.id = p.decision_id")
	if status != "all" {
		query = query.Where("p.status = ?", status)
	}
	if contentType != "all" {
		query = query.Where("p.content_type = ?", contentType)
	}
	if q != "" {
		like := "%" + q + "%"
		query = query.Where("p.original_title LIKE ? OR p.fixed_title LIKE ? OR p.content_id LIKE ?", like, like, like)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return utils.InternalError(c, "failed to count review queue")
	}
	var items []contentAIReviewQueueItem
	if err := query.Order("p.created_at DESC, p.id DESC").Limit(perPage).Offset((page - 1) * perPage).Scan(&items).Error; err != nil {
		return utils.InternalError(c, "failed to load review queue")
	}
	lastPage := int(math.Ceil(float64(total) / float64(perPage)))
	if lastPage < 1 {
		lastPage = 1
	}
	from := 0
	to := 0
	if total > 0 {
		from = (page-1)*perPage + 1
		to = from + len(items) - 1
	}
	return utils.Paginated(c, "success", items, utils.PaginationMeta{CurrentPage: page, PerPage: perPage, Total: total, LastPage: lastPage, From: from, To: to})
}

func (h *Handler) ModelCostSummary(c *fiber.Ctx) error {
	days := c.QueryInt("days", 7)
	if days < 1 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	since := time.Now().AddDate(0, 0, -days)
	base := database.DB().Model(&models.ContentAIModelRun{}).Where("created_at >= ?", since)
	var summary contentAIModelCostSummary
	if err := base.Select(`COUNT(*) AS total_runs,
		SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END) AS success_runs,
		SUM(CASE WHEN status <> 'success' THEN 1 ELSE 0 END) AS failed_runs,
		COALESCE(SUM(input_tokens), 0) AS input_tokens,
		COALESCE(SUM(output_tokens), 0) AS output_tokens,
		COALESCE(SUM(estimated_cost_usd), 0) AS estimated_cost,
		COALESCE(AVG(duration_ms), 0) AS avg_duration_ms`).Scan(&summary).Error; err != nil {
		return utils.InternalError(c, "failed to load model cost summary")
	}
	var modelsSummary []contentAIModelCostByModel
	if err := database.DB().Model(&models.ContentAIModelRun{}).
		Where("created_at >= ?", since).
		Select(`model, model_strategy, task_type, COUNT(*) AS runs,
			SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END) AS success_runs,
			SUM(CASE WHEN status <> 'success' THEN 1 ELSE 0 END) AS failed_runs,
			COALESCE(SUM(input_tokens), 0) AS input_tokens,
			COALESCE(SUM(output_tokens), 0) AS output_tokens,
			COALESCE(SUM(estimated_cost_usd), 0) AS estimated_cost,
			COALESCE(AVG(duration_ms), 0) AS avg_duration_ms`).
		Group("model, model_strategy, task_type").
		Order("estimated_cost DESC, runs DESC").
		Limit(50).
		Scan(&modelsSummary).Error; err != nil {
		return utils.InternalError(c, "failed to load model cost by model")
	}
	return utils.Success(c, "success", contentAIModelCostResponse{Summary: summary, Models: modelsSummary})
}
