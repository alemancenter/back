package contentaudit

import (
	"context"
	"strconv"
	"time"

	auditservice "github.com/imanjo/fiber-api/internal/services/contentaudit"
	"github.com/imanjo/fiber-api/internal/utils"
	"github.com/imanjo/fiber-api/pkg/logger"
	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

type publicQualityStatusPayload struct {
	Eligible    bool     `json:"eligible"`
	AdsEligible bool     `json:"ads_eligible"`
	Indexable   bool     `json:"indexable"`
	Audited     bool     `json:"audited"`
	Decision    string   `json:"decision"`
	AdSenseRisk string   `json:"adsense_risk"`
	Score       int      `json:"score"`
	Reasons     []string `json:"reasons"`
	DecisionID  *uint    `json:"decision_id,omitempty"`
}

func qualityGatePublicPayload(gate auditservice.ContentQualityGate) publicQualityStatusPayload {
	return publicQualityStatusPayload{
		Eligible:    gate.AdsEligible, // backwards-compatible alias for the existing frontend
		AdsEligible: gate.AdsEligible,
		Indexable:   gate.Indexable,
		Audited:     gate.Audited,
		Decision:    gate.Decision,
		AdSenseRisk: gate.Risk,
		Score:       gate.Score,
		Reasons:     gate.Reasons,
		DecisionID:  gate.DecisionID,
	}
}

// PublicArticleQualityStatus exposes the central Content Quality Gate for article pages.
func (h *Handler) PublicArticleQualityStatus(c *fiber.Ctx) error {
	return h.publicQualityStatus(c, "article")
}

// PublicPostQualityStatus exposes the central Content Quality Gate for post pages.
func (h *Handler) PublicPostQualityStatus(c *fiber.Ctx) error {
	return h.publicQualityStatus(c, "post")
}

func (h *Handler) publicQualityStatus(c *fiber.Ctx, contentType string) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return utils.BadRequest(c, "invalid id")
	}

	countryCode := c.Get("X-Country-Code", c.Query("country", "jo"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	gate, err := h.svc.QualityGate(ctx, contentType, strconv.FormatUint(id, 10), countryCode)
	if err != nil {
		logger.Error("failed to load public content quality gate",
			zap.String("content_type", contentType),
			zap.Uint64("content_id", id),
			zap.String("country_code", countryCode),
			zap.Error(err),
		)
		return utils.InternalError(c, "failed to load content quality status")
	}

	return utils.Success(c, "success", qualityGatePublicPayload(gate))
}
