package utils

import "testing"

func TestNormalizeIP(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "ipv4",
			in:   "8.8.8.8",
			want: "8.8.8.8",
		},
		{
			name: "ipv4 host port",
			in:   "8.8.8.8:443",
			want: "8.8.8.8",
		},
		{
			name: "ipv6",
			in:   "2606:4700:4700::1111",
			want: "2606:4700:4700::1111",
		},
		{
			name: "bracket ipv6 port",
			in:   "[2606:4700:4700::1111]:443",
			want: "2606:4700:4700::1111",
		},
		{
			name: "ipv4 mapped ipv6",
			in:   "::ffff:8.8.8.8",
			want: "8.8.8.8",
		},
		{
			name: "quoted",
			in:   "\"8.8.4.4\"",
			want: "8.8.4.4",
		},
		{
			name: "invalid octet",
			in:   "999.1.1.1",
			want: "",
		},
		{
			name: "garbage",
			in:   "abc123",
			want: "",
		},
		{
			name: "empty",
			in:   "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeIP(tt.in); got != tt.want {
				t.Fatalf(
					"normalizeIP(%q) = %q, want %q",
					tt.in,
					got,
					tt.want,
				)
			}
		})
	}
}

func TestFirstValidForwardedIP(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "single valid",
			in:   "8.8.8.8",
			want: "8.8.8.8",
		},
		{
			name: "invalid then valid",
			in:   "not-an-ip, 8.8.4.4",
			want: "8.8.4.4",
		},
		{
			name: "valid first in chain",
			in:   "1.1.1.1, 8.8.8.8",
			want: "1.1.1.1",
		},
		{
			name: "all invalid",
			in:   "abc, 999.1.1.1",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstValidForwardedIP(tt.in); got != tt.want {
				t.Fatalf(
					"firstValidForwardedIP(%q) = %q, want %q",
					tt.in,
					got,
					tt.want,
				)
			}
		})
	}
}
