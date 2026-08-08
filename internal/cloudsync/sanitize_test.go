package cloudsync

import (
	"strings"
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name    string
		want    string
		wantErr bool
	}{
		{"sub.txt", "sub.txt", false},
		{"dir/sub.txt", "dir/sub.txt", false},
		{"订阅.txt", "订阅.txt", false},
		{"a/b/订阅 1.txt", "a/b/订阅 1.txt", false},
		{"/abs.txt", "abs.txt", false},                              // leading slash trimmed
		{`a\b.txt`, "a/b.txt", false},                               // backslash → slash
		{"a//b.txt", "a/b.txt", false},                              // cleaned
		{"../evil", "", true},                                       // traversal
		{"a/../evil", "", true},                                     // traversal mid-path
		{"..", "", true},                                            // traversal
		{"", "", true},                                              // empty
		{"   ", "", true},                                           // blank
		{"a\x00b", "", true},                                        // NUL
		{"a\x1fb", "", true},                                        // control char
		{strings.Repeat("x", 513), "", true},                        // too long
		{strings.Repeat("y", 512), strings.Repeat("y", 512), false}, // exactly at limit
	}
	for _, tt := range tests {
		got, err := SanitizeFilename(tt.name)
		if (err != nil) != tt.wantErr {
			t.Errorf("SanitizeFilename(%q) error = %v, wantErr %v", tt.name, err, tt.wantErr)
			continue
		}
		if err == nil && got != tt.want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestJoinKey(t *testing.T) {
	tests := []struct {
		in   []string
		want string
	}{
		{[]string{}, ""},
		{[]string{"a"}, "a"},
		{[]string{"sub", "a.txt"}, "sub/a.txt"},
		{[]string{"sub/", "/a.txt"}, "sub/a.txt"},
		{[]string{"subs", "daily", "x.txt"}, "subs/daily/x.txt"},
		{[]string{"", "x.txt"}, "x.txt"},
	}
	for _, tt := range tests {
		if got := JoinKey(tt.in...); got != tt.want {
			t.Errorf("JoinKey(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
