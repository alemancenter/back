package contentquality

import "strings"

const (
	QualityMetaMissing  = "missing"
	QualityMetaTooShort = "too_short"
	QualityMetaOK       = "ok"

	QualityKeywordsMissing = "missing"
	QualityKeywordsOK      = "ok"

	QualityLengthThin            = "thin"
	QualityLengthNeedsEnrichment = "needs_enrichment"
	QualityLengthAdequate        = "adequate"
)

// ContentQualitySignal is the editor-facing SEO/quality readout returned on every
// article/post create and update, so gaps are visible at the moment of writing
// instead of only surfacing later in a background audit queue. It is informational
// only — never a save/publish blocker — and reuses the exact same thresholds as
// Diagnostics/ApplyAdReadinessRequirements so there is one definition of "thin" or
// "short meta" across the save path, the audit engine, and the ads gate.
type ContentQualitySignal struct {
	MetaDescription string `json:"meta_description_status"`
	Keywords        string `json:"keywords_status"`
	ContentLength   string `json:"content_length_signal"`
}

// BuildQualitySignal derives the editor-facing signal from the same inputs the audit
// engine already scores. hasKeywords must reflect the final saved state (e.g. a
// non-empty submitted value, or previously-saved keywords that were left untouched).
func BuildQualitySignal(meta string, hasKeywords bool, wordCount int) ContentQualitySignal {
	signal := ContentQualitySignal{}

	metaLen := len([]rune(strings.TrimSpace(meta)))
	switch {
	case metaLen == 0:
		signal.MetaDescription = QualityMetaMissing
	case metaLen < DiagnosticMetaMinChars:
		signal.MetaDescription = QualityMetaTooShort
	default:
		signal.MetaDescription = QualityMetaOK
	}

	if hasKeywords {
		signal.Keywords = QualityKeywordsOK
	} else {
		signal.Keywords = QualityKeywordsMissing
	}

	switch {
	case wordCount < DiagnosticReviewMinWords:
		signal.ContentLength = QualityLengthThin
	case wordCount < DiagnosticStrongMinWords:
		signal.ContentLength = QualityLengthNeedsEnrichment
	default:
		signal.ContentLength = QualityLengthAdequate
	}

	return signal
}
