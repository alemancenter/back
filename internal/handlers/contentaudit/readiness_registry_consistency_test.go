package contentaudit

import (
	"testing"

	"github.com/imanjo/fiber-api/internal/rulesregistry"
)

// TestReadinessProblemsMatchRuleRegistry guards the invariant from
// back/docs/reports/CONTENT_QUALITY_GOVERNANCE_CENTER_PLAN.md §3: every problem
// code the engine can actually produce must have a documented rulesregistry
// entry, and every documented entry must correspond to a code the engine still
// produces. Drift here means either an undocumented rule silently blocking
// readiness, or dead documentation for a rule that no longer exists.
func TestReadinessProblemsMatchRuleRegistry(t *testing.T) {
	engineCodes := make(map[string]bool)
	for code := range readinessProblemCatalog() {
		engineCodes[code] = true
	}

	if missing := rulesregistry.MissingFrom(engineCodes); len(missing) > 0 {
		t.Errorf("rulesregistry documents codes the engine no longer produces: %v", missing)
	}
	if undocumented := rulesregistry.UndocumentedIn(engineCodes); len(undocumented) > 0 {
		t.Errorf("engine produces codes with no rulesregistry entry: %v", undocumented)
	}
}
