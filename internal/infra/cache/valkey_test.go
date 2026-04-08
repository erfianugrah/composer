package cache

import (
	"testing"
)

func TestParseValkeyURL(t *testing.T) {
	tests := []struct {
		url      string
		wantAddr string
		wantTLS  bool
	}{
		{"valkey://localhost:6379", "localhost:6379", false},
		{"redis://myhost:6380", "myhost:6380", false},
		{"rediss://secure.host:6381", "secure.host:6381", true},
		{"valkey://myhost", "myhost:6379", false},
		{"redis://myhost", "myhost:6379", false},
		{"rediss://myhost", "myhost:6379", true},
		{"redis://user:pass@myhost:6380", "myhost:6380", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			opts := parseValkeyURL(tt.url)
			if len(opts.InitAddress) != 1 || opts.InitAddress[0] != tt.wantAddr {
				t.Errorf("parseValkeyURL(%q).InitAddress = %v, want [%q]", tt.url, opts.InitAddress, tt.wantAddr)
			}
			hasTLS := opts.TLSConfig != nil
			if hasTLS != tt.wantTLS {
				t.Errorf("parseValkeyURL(%q) TLS = %v, want %v", tt.url, hasTLS, tt.wantTLS)
			}
		})
	}
}

func TestParseValkeyURL_Auth(t *testing.T) {
	opts := parseValkeyURL("redis://myuser:mypass@host:6379")
	if opts.Username != "myuser" {
		t.Errorf("expected username 'myuser', got %q", opts.Username)
	}
	if opts.Password != "mypass" {
		t.Errorf("expected password 'mypass', got %q", opts.Password)
	}
}
