package contentquality

import "strings"

// GenericFillerPhrases are the specific stock phrases the AI content-draft/fix models reliably
// fall back to at the opening/closing of a draft despite being told not to (models default to a
// "this is important, prepare well, don't worry" bookend regardless of instructions) — and,
// critically, the exact same kind of padding the site's PREVIOUS, now-removed AI subsystem
// shipped into already-published content that got the site rejected by AdSense in the first
// place. Living here (not in internal/services, which is AI-generation-specific) lets the
// adsense-policy scan (internal/handlers/adsensepolicy) check EXISTING published content for
// these phrases too — a thin-content check alone (EvaluateDiagnostics) does not catch a
// long-enough article that is nonetheless mostly generic padding with no real subject-matter
// value, which is exactly the AdSense "thin/low-value content" failure mode this phrase list was
// built to catch. Matched against NormalizeForSimilarity'd text so diacritics/spacing/Alef-Ya
// variants don't cause a miss. NormalizeForSimilarity keeps ة (ta marbuta) as-is — it only
// rewrites أ/إ/آ/ٱ→ا, ى→ي, ؤ→و, ئ→ي, and strips diacritics/tatweel. Every phrase below must use ة
// exactly where the real word does (محطة، مهمة، اهمية، فرصة، وسيلة، اساسية، الاسرة، المدرسة) —
// a phrase spelled with ه instead would simply never match and silently defeat this whole check.
var GenericFillerPhrases = []string{
	"يعد من اهم", "تكمن اهمية", "محطة مهمة لقياس", "فرصة مهمة لاظهار",
	"وسيلة اساسية لقياس", "يجب علي الطالب الاستعداد", "يجب علي التلميذ الاستعداد",
	"لا يقل دور الاسرة عن دور المدرسة", "يخفف من التوتر ويرفع التركيز",
	"لا مصدر قلق", "انعكاسا صادقا لجهد",
}

// DetectGenericFillerPhrases scans text for GenericFillerPhrases and returns whichever ones it
// finds (deduplicated, in list order) — empty if none are present.
func DetectGenericFillerPhrases(text string) []string {
	normalized := NormalizeForSimilarity(text)
	found := make([]string, 0, 2)
	for _, phrase := range GenericFillerPhrases {
		if strings.Contains(normalized, phrase) {
			found = append(found, phrase)
		}
	}
	return found
}
