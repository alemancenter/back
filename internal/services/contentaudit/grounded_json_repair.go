package contentaudit

import "strings"

// repairGroundedJSONControlChars fixes only illegal raw JSON control
// characters that appear inside quoted strings. It intentionally does not
// repair structural JSON errors such as missing commas, braces, or quotes.
func repairGroundedJSONControlChars(raw string) string {
	if raw == "" {
		return raw
	}

	var out strings.Builder
	out.Grow(len(raw) + 32)

	inString := false
	escaped := false

	for i := 0; i < len(raw); i++ {
		b := raw[i]

		if !inString {
			out.WriteByte(b)
			if b == '"' {
				inString = true
				escaped = false
			}
			continue
		}

		if escaped {
			out.WriteByte(b)
			escaped = false
			continue
		}

		switch b {
		case '\\':
			out.WriteByte(b)
			escaped = true
		case '"':
			out.WriteByte(b)
			inString = false
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		default:
			if b < 0x20 {
				const hex = "0123456789abcdef"
				out.WriteString(`\u00`)
				out.WriteByte(hex[b>>4])
				out.WriteByte(hex[b&0x0f])
				continue
			}
			out.WriteByte(b)
		}
	}

	return out.String()
}
