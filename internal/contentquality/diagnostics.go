package contentquality

import "strings"

const (
	// The values below power reviewer-facing diagnostics. The same minimums are
	// also consumed by ApplyAdReadinessRequirements as a one-way ads safety guard;
	// they never grant eligibility and never change search indexing.
	DiagnosticTitleMinChars     = 20
	DiagnosticReviewMinWords    = 120
	DiagnosticShortFileMaxWords = 180
	// DiagnosticStrongMinWords is the "this is already good, no reinforcement needed" floor —
	// at or above this, EvaluateDiagnostics stops emitting the "متوسط الطول وقد يحتاج تعزيزًا"
	// signal and BuildQualitySignal reports QualityLengthAdequate instead of
	// QualityLengthNeedsEnrichment. Lowered from 300 to 250 at the site owner's explicit
	// judgment call: 250+ words of real content is strong enough on its own and shouldn't keep
	// surfacing as something to fix.
	DiagnosticStrongMinWords = 250
	DiagnosticMetaMinChars   = 80
)

// Diagnostics contains reviewer-facing signals only. It deliberately has no
// Indexable, AdsEligible, Ready, or pass/fail field so callers cannot accidentally
// turn these heuristics into a second enforcement policy.
type Diagnostics struct {
	WordCount  int      `json:"word_count"`
	CharCount  int      `json:"char_count"`
	FilesCount int      `json:"files_count"`
	Signals    []string `json:"signals"`
}

func EvaluateDiagnostics(title, plainText, meta string, filesCount int, published bool) Diagnostics {
	plainText = strings.TrimSpace(plainText)
	words := 0
	if plainText != "" {
		words = len(strings.Fields(plainText))
	}

	signals := make([]string, 0, 5)
	if len([]rune(strings.TrimSpace(title))) < DiagnosticTitleMinChars {
		signals = append(signals, "إشارة تحريرية: العنوان قصير ويستحق المراجعة.")
	}
	if words < DiagnosticReviewMinWords {
		signals = append(signals, "إشارة تحريرية: المحتوى قصير جدًا ويحتاج مراجعة بشرية.")
	} else if words < DiagnosticStrongMinWords {
		signals = append(signals, "إشارة تحريرية: المحتوى متوسط الطول وقد يحتاج تعزيزًا.")
	}
	if len([]rune(strings.TrimSpace(meta))) < DiagnosticMetaMinChars {
		signals = append(signals, "إشارة تحريرية: الوصف التعريفي قصير أو مفقود.")
	}
	if filesCount <= 0 {
		signals = append(signals, "إشارة تحريرية: لا توجد مرفقات مرتبطة بهذا المحتوى.")
	}
	if !published {
		signals = append(signals, "حالة النشر: العنصر غير منشور أو غير فعال.")
	}

	return Diagnostics{
		WordCount:  words,
		CharCount:  len([]rune(plainText)),
		FilesCount: filesCount,
		Signals:    signals,
	}
}
