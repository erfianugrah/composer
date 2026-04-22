package middleware

import "testing"

func TestClientIP(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"10.0.0.1:54321", "10.0.0.1"},
		{"10.0.0.1", "10.0.0.1"},
		{"[2001:db8::1]:54321", "2001:db8::1"},
		{"2001:db8::1", "2001:db8::1"},
		{"", ""},
		{"127.0.0.1:0", "127.0.0.1"},
	}

	for _, c := range cases {
		got := ClientIP(c.in)
		if got != c.want {
			t.Errorf("ClientIP(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestRateLimiter_BucketsByIPNotPort confirms the fix: requests from the
// same IP on different source ports should share a bucket, not split.
func TestRateLimiter_BucketsByIPNotPort(t *testing.T) {
	// Burst of 2 so the 3rd call would be rejected IF ports create separate buckets.
	rl := NewRateLimiter(1.0, 2)
	defer rl.Close()

	// Three requests from same IP, different simulated source ports.
	ips := []string{
		ClientIP("10.0.0.5:54321"),
		ClientIP("10.0.0.5:54322"),
		ClientIP("10.0.0.5:54323"),
	}

	allowed := 0
	for _, ip := range ips {
		if rl.Allow(ip) {
			allowed++
		}
	}

	if allowed != 2 {
		t.Fatalf("expected 2 allowed (burst capacity), got %d; client IP was being split across source ports", allowed)
	}
}
