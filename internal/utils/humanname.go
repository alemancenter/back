package utils

import (
	"regexp"
	"strings"
	"unicode"
)

// The registration and profile "name" field was being abused to inject promotional
// spam sentences and URL-shortener links ("Поздравляем! … https://tinyurl.com/…").
// These rules keep the field to something that can plausibly be a person's display
// name: short, single-line, letters from the scripts this site actually uses
// (Arabic + Latin), and no links.

var (
	nameLinkRE  = regexp.MustCompile(`(?i)(https?://|www\.|\bt\.me\b|\btinyurl\b|\bbit\.ly\b|\bt\.co\b|\bis\.gd\b|\bcutt\.ly\b|[a-z0-9-]{2,}\.(?:com|net|org|ru|info|xyz|top|vip|club|online|site|link|click|shop|store|live)(?:/|\b)|</?a[\s>]|\[url|\{link)`)
	nameJunkRE  = regexp.MustCompile(`[!?#$%*()\[\]{}<>|\\~^=+]{2,}|[!?]{3,}`)
	nameSpaceRE = regexp.MustCompile(`\s+`)
)

// spamPhraseMarkers are lowercase substrings from the Latin-script variants of the
// promo-spam wave. Cyrillic / other-script names are already rejected by the script
// check in ValidateHumanName, so this list only needs the English forms.
var spamPhraseMarkers = []string{
	"congratulation", "you have won", "you won", "you've won", "claim your",
	"claim now", "gift card", "click here", "free gift", "reward is waiting",
	"reward waiting", "bonus code", "promo code", "limited offer", "act now",
}

// NormalizeHumanName collapses whitespace, converts line breaks/tabs to spaces, drops
// control characters, and trims. Run it before both validation and storage.
func NormalizeHumanName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		default:
			return r
		}
	}, name)
	return strings.TrimSpace(nameSpaceRE.ReplaceAllString(name, " "))
}

// ValidateHumanName reports whether name is a plausible person/display name and, when
// not, a short Arabic reason suitable for a field error. Call NormalizeHumanName first
// (this also normalizes defensively).
func ValidateHumanName(name string) (bool, string) {
	n := NormalizeHumanName(name)
	runeCount := len([]rune(n))

	if runeCount < 2 {
		return false, "الاسم قصير جدًا"
	}
	if runeCount > 60 {
		return false, "الاسم طويل جدًا، استخدم اسمك فقط"
	}
	if strings.Contains(n, "@") {
		return false, "لا يمكن أن يحتوي الاسم على بريد إلكتروني"
	}
	if nameLinkRE.MatchString(n) {
		return false, "لا يمكن أن يحتوي الاسم على روابط"
	}
	if nameJunkRE.MatchString(n) {
		return false, "الاسم يحتوي على رموز غير مسموحة"
	}

	lower := strings.ToLower(n)
	for _, marker := range spamPhraseMarkers {
		if strings.Contains(lower, marker) {
			return false, "الاسم غير صالح"
		}
	}

	// Script check: every letter must be Arabic or Latin, and there must be at least
	// one letter. A name written in Cyrillic, Greek, CJK, etc. on this site is spam.
	var letters, allowed int
	for _, r := range n {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		if unicode.Is(unicode.Latin, r) || unicode.Is(unicode.Arabic, r) {
			allowed++
		}
	}
	if letters == 0 {
		return false, "يجب أن يحتوي الاسم على حروف"
	}
	if allowed < letters {
		return false, "يرجى كتابة الاسم بالعربية أو الإنجليزية"
	}
	// Letters must dominate — blocks "J0hn_123 !!! ##" style filler.
	if letters*2 < runeCount {
		return false, "الاسم غير صالح"
	}

	return true, ""
}
