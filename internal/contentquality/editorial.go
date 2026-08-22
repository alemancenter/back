package contentquality

import (
	"strings"

	"github.com/imanjo/fiber-api/internal/models"
)

// ApplyEditorialDecision applies only decisions that are safe to enforce
// immediately. A human NOINDEX decision blocks indexing and ads. KEEP and
// IMPROVE remain workflow labels, while MERGE_301 stays review-only until a
// real redirect target is validated and the redirect is implemented.
func ApplyEditorialDecision(gate Gate, decision string) Gate {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case models.EditorialDecisionNoindex:
		gate.Indexable = false
		gate.AdsEligible = false
		gate.Reasons = appendUniqueReason(gate.Reasons, "قرار تحريري يدوي: NOINDEX حتى تتم معالجة الصفحة أو إعادة تصنيفها.")
	}
	return gate
}

func appendUniqueReason(reasons []string, reason string) []string {
	for _, existing := range reasons {
		if existing == reason {
			return reasons
		}
	}
	return append(reasons, reason)
}
