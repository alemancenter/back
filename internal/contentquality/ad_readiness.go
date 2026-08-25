package contentquality

import (
	"strings"
	"unicode/utf8"
)

// ApplyAdReadinessRequirements is a one-way safety guard for advertisements.
// It can revoke an approval that no longer matches the current page source, but
// it can never grant ad eligibility or change the search-indexing decision.
func ApplyAdReadinessRequirements(gate Gate, title, plainText, meta string) Gate {
	if !gate.AdsEligible {
		return gate
	}

	reasons := make([]string, 0, 3)
	if utf8.RuneCountInString(strings.TrimSpace(title)) < DiagnosticTitleMinChars {
		reasons = append(reasons, "العنوان الحالي أقصر من الحد التحريري الداخلي؛ الإعلانات متوقفة حتى مراجعته.")
	}
	if len(strings.Fields(strings.TrimSpace(plainText))) < DiagnosticStrongMinWords {
		reasons = append(reasons, "المحتوى الحالي أقل من 300 كلمة؛ الإعلانات متوقفة حتى إثرائه ومراجعته.")
	}
	if utf8.RuneCountInString(strings.TrimSpace(meta)) < DiagnosticMetaMinChars {
		reasons = append(reasons, "الوصف التعريفي الحالي مفقود أو أقصر من 80 حرفًا؛ الإعلانات متوقفة حتى إصلاحه.")
	}
	if len(reasons) == 0 {
		return gate
	}

	gate.AdsEligible = false
	gate.Reasons = removeAdEligibleReason(gate.Reasons)
	for _, reason := range reasons {
		gate.Reasons = appendUniqueReason(gate.Reasons, reason)
	}
	return gate
}

func removeAdEligibleReason(reasons []string) []string {
	out := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		if strings.Contains(reason, "مؤهل للإعلانات") {
			continue
		}
		out = append(out, reason)
	}
	return out
}
