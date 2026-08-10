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

// The frontend renders article/post content via set:html, meaning this sanitizer is the
// *only* thing standing between whatever an editor account can submit and what runs in every
// visitor's browser (no other cleanup layer exists between here and set:html). This suite
// verifies each specific attack shape a security review asked to see proof against, rather
// than trusting the richTextPolicy doc comment on faith.
func TestSanitizeHTML_StripsImgOnerror(t *testing.T) {
	got := SanitizeHTML(`<img src="x" onerror="alert(document.cookie)">`)
	assert.NotContains(t, got, "onerror")
}

func TestSanitizeHTML_StripsJavascriptHref(t *testing.T) {
	got := SanitizeHTML(`<a href="javascript:alert(1)">click me</a>`)
	assert.NotContains(t, got, "javascript:")
	assert.Contains(t, got, "click me") // link text is not executable — fine to keep
}

func TestSanitizeHTML_StripsUntrustedIframe(t *testing.T) {
	// Not just "src stripped" — the whole element must go, since a bare <iframe></iframe>
	// left behind is still an unwanted embed surface (e.g. about:blank framing tricks).
	got := SanitizeHTML(`<iframe src="https://evil.com/phishing"></iframe>`)
	assert.NotContains(t, got, "evil.com")
	assert.NotContains(t, got, "<iframe")
}

func TestSanitizeHTML_StripsJavascriptIframeSrc(t *testing.T) {
	got := SanitizeHTML(`<iframe src="javascript:alert(1)"></iframe>`)
	assert.NotContains(t, got, "javascript:")
	assert.NotContains(t, got, "<iframe")
}

func TestSanitizeHTML_StripsSvgOnload(t *testing.T) {
	got := SanitizeHTML(`<svg onload="alert(1)"></svg>`)
	assert.NotContains(t, got, "onload")
	assert.NotContains(t, got, "alert(1)")
}

func TestSanitizeHTML_StripsCSSJavascriptURL(t *testing.T) {
	got := SanitizeHTML(`<p style="color:red;background:url(javascript:alert(1))">x</p>`)
	assert.NotContains(t, got, "javascript:")
}

func TestSanitizeHTML_StripsScriptInDataURI(t *testing.T) {
	got := SanitizeHTML(`<img src="data:text/html,<script>alert(1)</script>">`)
	assert.NotContains(t, got, "<script")
}

// Attack-shape coverage above is only half the picture — a policy that strips everything
// dangerous by also stripping everything the editor legitimately produces isn't a fix, it's
// data loss with extra steps. These confirm the allowlist side keeps working.
func TestSanitizeHTML_KeepsLegitimateFormatting(t *testing.T) {
	input := `<h2>Title</h2><p><strong>bold</strong> and <em>italic</em></p><ul><li>item</li></ul>`
	got := SanitizeHTML(input)
	assert.Contains(t, got, "<h2>Title</h2>")
	assert.Contains(t, got, "<strong>bold</strong>")
	assert.Contains(t, got, "<em>italic</em>")
	assert.Contains(t, got, "<li>item</li>")
}

func TestSanitizeHTML_KeepsTrustedIframe(t *testing.T) {
	input := `<iframe src="https://www.youtube.com/embed/abc123" width="560" height="315"></iframe>`
	got := SanitizeHTML(input)
	assert.Contains(t, got, "youtube.com/embed/abc123")
}
