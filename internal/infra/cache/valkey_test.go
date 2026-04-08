package cache

import (
	"testing"
)

func TestParseValkeyAddr(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"valkey://localhost:6379", "localhost:6379"},
		{"redis://myhost:6380", "myhost:6380"},
		{"rediss://secure.host:6381", "secure.host:6381"},
		{"localhost:6379", "localhost:6379"},
		{"valkey://myhost", "myhost:6379"}, // adds default port
		{"redis://myhost", "myhost:6379"},  // adds default port
		{"myhost", "myhost:6379"},          // bare host
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := parseValkeyAddr(tt.url)
			if got != tt.want {
				t.Errorf("parseValkeyAddr(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}
