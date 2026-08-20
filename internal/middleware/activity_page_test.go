package middleware

import "testing"

func TestNormalizeFrontendPage(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"/jo/lesson/articles/1131", "/jo/lesson/articles/1131"},
		{"/jo/posts/55?utm_source=test", "/jo/posts/55"},
		{"/search?q=math", "/search"},
		{"/", "/"},
		{"/api/articles", ""},
		{"https://evil.example/page", ""},
		{"//evil.example/page", ""},
		{"", ""},
	}

	for _, tc := range tests {
		if got := normalizeFrontendPage(tc.in); got != tc.want {
			t.Fatalf("normalizeFrontendPage(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestFrontendPageFromReferer(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"https://imanjo.com/jo/lesson/articles/1131", "/jo/lesson/articles/1131"},
		{"https://www.imanjo.com/jo/posts/12?q=x", "/jo/posts/12"},
		{"https://imanjo.com/api/articles", ""},
		{"https://example.com/jo/posts/12", ""},
		{"not a url", ""},
	}

	for _, tc := range tests {
		if got := frontendPageFromReferer(tc.in); got != tc.want {
			t.Fatalf("frontendPageFromReferer(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}
