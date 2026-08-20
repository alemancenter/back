package services

import "testing"

func TestParseUserAgentBrowserFamilies(t *testing.T) {
	tests := []struct {
		name    string
		ua      string
		browser string
		os      string
	}{
		{
			name:    "chrome android",
			ua:      "Mozilla/5.0 (Linux; Android 14; Pixel) AppleWebKit/537.36 Chrome/126.0 Mobile Safari/537.36",
			browser: "Chrome",
			os:      "Android",
		},
		{
			name:    "chrome ios",
			ua:      "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 CriOS/126.0 Mobile/15E148 Safari/604.1",
			browser: "Chrome",
			os:      "iOS",
		},
		{
			name:    "facebook android",
			ua:      "Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 [FBAN/FB4A;FBAV/470.0.0.0]",
			browser: "Facebook",
			os:      "Android",
		},
		{
			name:    "google app ios",
			ua:      "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 GSA/321.0 Mobile/15E148 Safari/604.1",
			browser: "Google App",
			os:      "iOS",
		},
		{
			name:    "android webview",
			ua:      "Mozilla/5.0 (Linux; Android 13; Device Build/ABC; wv) AppleWebKit/537.36 Version/4.0",
			browser: "Android WebView",
			os:      "Android",
		},
		{
			name:    "generic mozilla becomes other",
			ua:      "Mozilla/5.0 AppleWebKit/537.36",
			browser: "Other",
			os:      "",
		},
		{
			name:    "unknown nonempty becomes other",
			ua:      "ExampleClient/1.0",
			browser: "Other",
			os:      "",
		},
		{
			name:    "empty stays empty",
			ua:      "",
			browser: "",
			os:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			browser, os := parseUserAgent(tt.ua)

			if browser != tt.browser {
				t.Fatalf(
					"browser = %q, want %q",
					browser,
					tt.browser,
				)
			}

			if os != tt.os {
				t.Fatalf(
					"os = %q, want %q",
					os,
					tt.os,
				)
			}
		})
	}
}

func TestClassifyDeviceType(t *testing.T) {
	tests := []struct {
		name string
		ua   string
		want string
	}{
		{
			name: "android phone",
			ua:   "Mozilla/5.0 (Linux; Android 14) Mobile",
			want: "mobile",
		},
		{
			name: "android tablet",
			ua:   "Mozilla/5.0 (Linux; Android 14)",
			want: "tablet",
		},
		{
			name: "iphone",
			ua:   "Mozilla/5.0 (iPhone)",
			want: "mobile",
		},
		{
			name: "ipad",
			ua:   "Mozilla/5.0 (iPad)",
			want: "tablet",
		},
		{
			name: "windows",
			ua:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
			want: "desktop",
		},
		{
			name: "empty",
			ua:   "",
			want: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyDeviceType(tt.ua); got != tt.want {
				t.Fatalf(
					"classifyDeviceType() = %q, want %q",
					got,
					tt.want,
				)
			}
		})
	}
}
