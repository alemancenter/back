package contentaudit

import (
	"github.com/gofiber/fiber/v2"
	"github.com/imanjo/fiber-api/internal/rulesregistry"
	"github.com/imanjo/fiber-api/internal/utils"
)

// ListQualityRules returns the compiled-in rule registry (see
// back/docs/reports/CONTENT_QUALITY_GOVERNANCE_CENTER_PLAN.md §3): every rule's
// scope, content types, severity, official/editorial/recommendation
// classification, fix method, and whether auto-apply is allowed. This is the
// same source rulesregistry.Seed writes to each country database at startup, so
// it is identical across countries by design — rules are not country-specific.
func (h *Handler) ListQualityRules(c *fiber.Ctx) error {
	return utils.Success(c, "قواعد الجودة", rulesregistry.Registry)
}
