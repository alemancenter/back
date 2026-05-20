package utils

import (
	"testing"
)

func TestCacheKey(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		parts    []any
		expected string // Expected prefix at least, since hash is deterministic
	}{
		{
			name:   "Simple string parts",
			prefix: "test",
			parts:  []any{"part1", "part2"},
		},
		{
			name:   "Mixed types",
			prefix: "user",
			parts:  []any{1, "name", true},
		},
		{
			name:   "Structs",
			prefix: "filter",
			parts:  []any{struct{ Name string }{"Test"}},
		},
		{
			name:   "Empty parts",
			prefix: "empty",
			parts:  []any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CacheKey(tt.prefix, tt.parts...)

			// Check if result has the correct prefix
			expectedPrefix := tt.prefix + ":"
			if len(result) <= len(expectedPrefix) || result[:len(expectedPrefix)] != expectedPrefix {
				t.Errorf("CacheKey() = %v, expected prefix %v", result, expectedPrefix)
			}

			// Ensure determinism: Calling it twice with the same arguments should yield the same key
			result2 := CacheKey(tt.prefix, tt.parts...)
			if result != result2 {
				t.Errorf("CacheKey() is not deterministic: %v != %v", result, result2)
			}
		})
	}
}
