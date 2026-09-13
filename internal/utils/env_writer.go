package utils

import (
	"fmt"
	"os"
	"strings"
)

// UpdateEnvFile updates the given key=value pairs in the .env file at envPath.
// Lines with matching keys are replaced in place; the rest of the file is preserved.
func UpdateEnvFile(envPath string, updates map[string]string) error {
	// A value containing a raw CR/LF becomes its own physical line once written — since
	// quoting only kicks in below for values with a space/tab, an unquoted value like
	// "x\nSOME_KEY=attacker-value" is indistinguishable on disk from two real assignments,
	// and the .env parser reads it as such on the next restart. Reject outright rather than
	// stripping, so a caller who forgot to validate its own input fails loudly instead of the
	// file silently gaining an unintended line.
	for key, value := range updates {
		if strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("env writer: value for %q contains a line break or control character", key)
		}
	}

	raw, err := os.ReadFile(envPath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(raw), "\n")
	applied := make(map[string]bool)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		if newVal, ok := updates[key]; ok {
			// Preserve any inline comment after the value
			rest := line[idx+1:]
			commentIdx := -1
			inQuote := false
			quoteChar := byte(0)
			for j := 0; j < len(rest); j++ {
				c := rest[j]
				if inQuote {
					if c == quoteChar {
						inQuote = false
					}
				} else {
					if c == '"' || c == '\'' {
						inQuote = true
						quoteChar = c
					} else if c == '#' {
						commentIdx = j
						break
					}
				}
			}
			comment := ""
			if commentIdx >= 0 {
				comment = " " + rest[commentIdx:]
			}
			// Quote values that contain spaces and are not already quoted
			formatted := newVal
			if strings.ContainsAny(newVal, " \t") && !strings.HasPrefix(newVal, `"`) && !strings.HasPrefix(newVal, `'`) {
				formatted = `"` + newVal + `"`
			}
			lines[i] = key + "=" + formatted + comment
			applied[key] = true
		}
	}

	// Append any keys that were not present in the file
	added := false
	for key, val := range updates {
		if applied[key] {
			continue
		}
		if !added {
			lines = append(lines, "")
			added = true
		}
		formatted := val
		if strings.ContainsAny(val, " \t") && !strings.HasPrefix(val, `"`) {
			formatted = `"` + val + `"`
		}
		lines = append(lines, key+"="+formatted)
	}

	return os.WriteFile(envPath, []byte(strings.Join(lines, "\n")), 0644)
}
