package middleware

import "net"

// ClientIP extracts just the IP from a `host:port` `r.RemoteAddr` string.
// Chi's `RealIP` middleware replaces RemoteAddr with a bare IP (no port) when
// enabled; this helper handles both forms so rate-limiter buckets and audit
// rows key by IP alone — never IP+port.
//
// Behaviour:
//   - "10.0.0.1:54321"           -> "10.0.0.1"
//   - "10.0.0.1"                 -> "10.0.0.1"
//   - "[2001:db8::1]:54321"      -> "2001:db8::1"
//   - "2001:db8::1"              -> "2001:db8::1"
//   - ""                         -> ""
func ClientIP(remoteAddr string) string {
	if remoteAddr == "" {
		return ""
	}
	// Try host:port split first — works for "1.2.3.4:port" and "[::1]:port".
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	// Not host:port — assume it's already a bare IP.
	return remoteAddr
}
