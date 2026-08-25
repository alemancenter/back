package rulesregistry

import "github.com/imanjo/fiber-api/internal/models"

// ReadinessState is the single governance status shown to editors — see plan
// §1. It replaces the three unrelated status vocabularies that used to coexist
// (ContentAIDecision.Decision, the readiness Level, and the editorial
// Decision): this is a value COMPUTED at read time from data that already
// exists, never a new stored column, so there is exactly one place that
// decides what "ready" means.
type ReadinessState string

const (
	// StateCriticalBlocker: an official Google/AdSense requirement is not met,
	// or the content quality gate has marked the page non-indexable. Must be
	// resolved before anything else.
	StateCriticalBlocker ReadinessState = "critical_blocker"
	// StateNeedsImprovement: no official blocker, but at least one of this
	// site's own editorial standards isn't met for this content type.
	StateNeedsImprovement ReadinessState = "needs_improvement"
	// StateHumanDecisionPending: a matched rule is flagged RequiresHumanDecision
	// and has no resolution recorded yet — e.g. a title rewrite, canonical
	// change, merge, or rights determination. Not reachable by any rule
	// currently in Registry (the one RequiresHumanDecision rule, policy_blocked,
	// is also an official requirement and resolves to StateCriticalBlocker
	// first) — defined now so a future rule can opt into it without a second
	// state model being invented later.
	StateHumanDecisionPending ReadinessState = "human_decision_pending"
	// StateInternallyReady: no known blocker. Not a claim that Google has
	// indexed the page or that AdSense will approve it — see the separate,
	// independent GSC index-status badge (models.GSCIndexStatus*).
	StateInternallyReady ReadinessState = "internally_ready"
)

var registryByCode = buildRegistryIndex()

func buildRegistryIndex() map[string]int {
	index := make(map[string]int, len(Registry))
	for i, rule := range Registry {
		index[rule.Code] = i
	}
	return index
}

// DeriveReadinessState computes the governance state for one content item from
// its content-quality gate result and the rule codes matched against it (e.g.
// unifiedReadinessItem.Problems' codes from handlers/contentaudit). indexable
// should come directly from contentquality.Gate.Indexable — a false value is
// always a critical blocker regardless of which (if any) problem codes fired,
// since it is the audit engine's own authoritative indexability signal.
func DeriveReadinessState(indexable bool, matchedCodes []string) ReadinessState {
	if !indexable {
		return StateCriticalBlocker
	}

	hasEditorialStandardMatch := false
	hasUnresolvedHumanDecision := false
	for _, code := range matchedCodes {
		idx, ok := registryByCode[code]
		if !ok {
			continue
		}
		rule := Registry[idx]
		if rule.RuleType == models.RuleTypeOfficialGoogleRequirement {
			return StateCriticalBlocker
		}
		if rule.RuleType == models.RuleTypeSiteEditorialStandard {
			hasEditorialStandardMatch = true
		}
		if rule.RequiresHumanDecision {
			hasUnresolvedHumanDecision = true
		}
	}

	if hasEditorialStandardMatch {
		return StateNeedsImprovement
	}
	if hasUnresolvedHumanDecision {
		return StateHumanDecisionPending
	}
	return StateInternallyReady
}
