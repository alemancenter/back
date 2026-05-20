package contentaudit

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchingTermsFindsArabicAndEnglishTerms(t *testing.T) {
	text := normalizeText("هذا محتوى يروج إلى كازينو و casino links")
	matches := matchingTerms(text, []string{"كازينو", "casino", "sex"})

	assert.ElementsMatch(t, []string{"كازينو", "casino"}, matches)
}

func TestMatchingTermsAvoidsShortEnglishSubstrings(t *testing.T) {
	text := normalizeText("Essex county article with context")
	matches := matchingTerms(text, []string{"sex"})

	assert.Empty(t, matches)
}

func TestDangerousLinkReasonsFlagsShortenersAndSuspiciousHosts(t *testing.T) {
	allowed := map[string]struct{}{"example.com": {}}

	reasons := dangerousLinkReasons(
		`safe https://example.com/page short https://bit.ly/x risky https://best-casino.example.net`,
		allowed,
	)

	assert.Len(t, reasons, 2)
	assert.Contains(t, reasons[0]+reasons[1], "bit.ly")
	assert.Contains(t, reasons[0]+reasons[1], "casino")
}

func TestDangerousLinkReasonsIgnoresAllowedSubdomains(t *testing.T) {
	allowed := map[string]struct{}{"example.com": {}}

	reasons := dangerousLinkReasons("https://cdn.example.com/file.pdf", allowed)

	assert.Empty(t, reasons)
}

func TestWordCountStripsHTMLAndScriptBlocks(t *testing.T) {
	count := wordCount(`<p>one two</p><script>alert("three four five")</script> six`)

	assert.Equal(t, 3, count)
}
