package contentquality

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ReplacementArtifactTokens are unresolved regular-expression replacement tokens
// that must never reach published educational content. The list is intentionally
// narrow: these are the exact placeholder forms observed in production content.
var ReplacementArtifactTokens = []string{"${1}", "${2}", "${3}", "$1", "$2", "$3"}

// TextField keeps the detector independent from article/post models while
// preserving deterministic field order for dashboards and tests.
type TextField struct {
	Name  string
	Value string
}

// ReplacementArtifact describes one literal unresolved replacement token.
type ReplacementArtifact struct {
	Token   string `json:"token"`
	Field   string `json:"field"`
	Offset  int    `json:"offset"`
	Snippet string `json:"snippet"`
}

// DetectReplacementArtifacts scans text fields for the exact unresolved tokens
// observed in production. Offsets are byte offsets (matching Go string slicing),
// while snippets are built rune-safely so Arabic text is never cut mid-character.
func DetectReplacementArtifacts(fields ...TextField) []ReplacementArtifact {
	artifacts := make([]ReplacementArtifact, 0)
	for _, field := range fields {
		value := field.Value
		if value == "" || !strings.Contains(value, "$") {
			continue
		}
		for _, token := range ReplacementArtifactTokens {
			from := 0
			for from < len(value) {
				rel := strings.Index(value[from:], token)
				if rel < 0 {
					break
				}
				idx := from + rel
				// Do not classify $1/$2/$3 when they are the prefix of a larger
				// numeric amount such as $10 or $250. Braced placeholders remain exact.
				if len(token) == 2 {
					next := idx + len(token)
					if next < len(value) && value[next] >= '0' && value[next] <= '9' {
						from = next
						continue
					}
				}
				artifacts = append(artifacts, ReplacementArtifact{
					Token:   token,
					Field:   field.Name,
					Offset:  idx,
					Snippet: replacementArtifactSnippet(value, idx, len(token)),
				})
				from = idx + len(token)
			}
		}
	}
	return artifacts
}

func ContainsReplacementArtifact(value string) bool {
	return len(DetectReplacementArtifacts(TextField{Name: "content", Value: value})) > 0
}

// ApplyReplacementArtifactGuard is a deterministic hard guard layered on top of
// the AI/content-audit decision. AI can never approve content while a literal
// replacement artifact is still present in the current source text.
func ApplyReplacementArtifactGuard(gate Gate, artifacts []ReplacementArtifact) Gate {
	if len(artifacts) == 0 {
		return gate
	}

	gate.Indexable = false
	gate.AdsEligible = false
	gate.Risk = "critical"
	reason := fmt.Sprintf("تم اكتشاف %d رمز استبدال غير محلول في المحتوى؛ الأرشفة والإعلانات متوقفتان حتى الإصلاح.", len(artifacts))
	for _, existing := range gate.Reasons {
		if existing == reason {
			return gate
		}
	}
	gate.Reasons = append([]string{reason}, gate.Reasons...)
	if len(gate.Reasons) > 6 {
		gate.Reasons = gate.Reasons[:6]
	}
	return gate
}

func replacementArtifactSnippet(value string, byteOffset, tokenLen int) string {
	if byteOffset < 0 || byteOffset > len(value) {
		return ""
	}
	startRune := utf8.RuneCountInString(value[:byteOffset])
	tokenRunes := utf8.RuneCountInString(value[byteOffset:minInt(len(value), byteOffset+tokenLen)])
	runes := []rune(value)
	start := maxInt(0, startRune-36)
	end := minInt(len(runes), startRune+tokenRunes+36)
	snippet := strings.Join(strings.Fields(string(runes[start:end])), " ")
	if start > 0 {
		snippet = "…" + snippet
	}
	if end < len(runes) {
		snippet += "…"
	}
	return snippet
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
