package contentquality

import "strings"

const (
	// The values below are editorial diagnostics only. They help reviewers find
	// thin or incomplete pages, but they MUST NOT decide indexing or ad eligibility.
	// Enforcement belongs exclusively to Gate in gate.go.
	DiagnosticTitleMinChars = 20
	DiagnosticReviewMinWords = 120
	DiagnosticStrongMinWords = 300
	DiagnosticMetaMinChars = 80
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
