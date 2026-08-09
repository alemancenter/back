package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Regression guard for the bug where Quill's style-based direction attributor
// (style="direction:rtl") was silently stripped on save because "direction" was missing
// from richTextPolicy's AllowStyles list — RTL formatting looked correct in the editor but
// vanished after a save/reload round-trip.
func TestSanitizeHTML_AllowsDirectionStyle(t *testing.T) {
	input := `<p style="direction:rtl">مرحبا</p>`
	got := SanitizeHTML(input)

	// bluemonday re-serializes the style attribute (adds a space after the colon), so assert
	// on the property/value rather than the exact original formatting.
	assert.Regexp(t, `style="direction:\s*rtl"`, got)
	assert.Contains(t, got, "مرحبا")
}

// Baseline sanity: an already-allowlisted style keeps working, so a future edit to
// AllowStyles can't silently empty the list without a test noticing.
func TestSanitizeHTML_AllowsColorStyle(t *testing.T) {
	input := `<p style="color:#ff0000">نص ملون</p>`
	got := SanitizeHTML(input)

	assert.Regexp(t, `style="color:\s*#ff0000"`, got)
}

// A style property that is not on the allowlist must still be stripped — "width" specifically,
// since that's the exact property the dashboard's image-resize preset buttons would emit
// (identified during a frontend audit as a real, would-be-silent data-loss path if the
// allowlist were ever loosened without this guard).
func TestSanitizeHTML_StripsDisallowedStyle(t *testing.T) {
	input := `<img src="https://example.com/a.png" style="width:50%">`
	got := SanitizeHTML(input)

	assert.NotContains(t, got, "width")
}

// Baseline XSS regression guard: the sanitizer's core job is stripping executable content
// regardless of any allowlist additions made for legitimate formatting.
func TestSanitizeHTML_StripsScriptsAndEventHandlers(t *testing.T) {
	input := `<p onclick="alert(1)">hello</p><script>alert(2)</script>`
	got := SanitizeHTML(input)

	assert.NotContains(t, got, "onclick")
	assert.NotContains(t, got, "<script")
	assert.NotContains(t, got, "alert(2)")
}
