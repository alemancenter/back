package utils

import (
	"fmt"
	"strings"
)

var arabicLatinMap = map[rune]string{
	'ا': "a", 'أ': "a", 'إ': "e", 'آ': "a", 'ٱ': "a",
	'ب': "b", 'ت': "t", 'ث': "th", 'ج': "j",
	'ح': "h", 'خ': "kh", 'د': "d", 'ذ': "dh",
	'ر': "r", 'ز': "z", 'س': "s", 'ش': "sh",
	'ص': "s", 'ض': "d", 'ط': "t", 'ظ': "z",
	'ع': "a", 'غ': "gh", 'ف': "f", 'ق': "q",
	'ك': "k", 'ل': "l", 'م': "m", 'ن': "n",
	'ه': "h", 'و': "w", 'ي': "y", 'ى': "a",
	'ة': "h", 'ء': "a", 'ئ': "y", 'ؤ': "w",
	// Arabic-Indic digits
	'٠': "0", '١': "1", '٢': "2", '٣': "3", '٤': "4",
	'٥': "5", '٦': "6", '٧': "7", '٨': "8", '٩': "9",
}

// Arabic diacritics (tashkeel) — strip silently
var arabicDiacritics = map[rune]bool{
	'ً': true, 'ٌ': true, 'ٍ': true,
	'َ': true, 'ُ': true, 'ِ': true,
	'ّ': true, 'ْ': true, 'ٓ': true,
	'ٔ': true, 'ٕ': true,
}

// GenerateSlug produces a URL-safe ASCII slug from a title.
// Arabic characters are transliterated to Latin equivalents.
func GenerateSlug(title string) string {
	var b strings.Builder
	prevHyphen := true // start true to avoid a leading hyphen

	for _, r := range title {
		if arabicDiacritics[r] {
			continue
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32)
			prevHyphen = false
		case r == ' ' || r == '-' || r == '_':
			if !prevHyphen {
				b.WriteByte('-')
				prevHyphen = true
			}
		default:
			if latin, ok := arabicLatinMap[r]; ok {
				b.WriteString(latin)
				prevHyphen = false
			}
		}
	}

	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "post"
	}
	return slug
}

// NumberedSlug appends "-N" to a base slug for N >= 2.
func NumberedSlug(base string, n int) string {
	if n <= 1 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, n)
}
