// Package host defines the DockerHost aggregate: a named remote docker
// daemon endpoint composerd can manage stacks on. The DEFAULT host (the
// daemon composerd was configured with via COMPOSER_DOCKER_HOST / socket
// auto-detection) is implicit and has no row - stacks.host_id NULL means
// default. Rows in docker_hosts are ADDITIONAL remotes.
package host

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// DefaultName is the display name for the implicit default host.
const DefaultName = "local"

// Host is a named remote docker daemon endpoint.
type Host struct {
	ID        int64
	Name      string
	Endpoint  string // tcp://host:2376 | tcp://host:2375 | unix:///path.sock
	CertDir   string // dir holding ca.pem/cert.pem/key.pem; "" = no mTLS
	CreatedAt time.Time
	UpdatedAt time.Time
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

// Validate checks the host fields are valid. Name must be a lowercase
// dns-label-ish string (a-z0-9_-, max 63). The reserved name "local" is
// rejected. Endpoint must start with tcp://, unix://, or ssh://.
func (h *Host) Validate() error {
	if !nameRe.MatchString(h.Name) {
		return fmt.Errorf("host name %q: must be lowercase dns-label-ish (a-z0-9_-, max 63)", h.Name)
	}
	if h.Name == DefaultName {
		return fmt.Errorf("host name %q is reserved for the default host", DefaultName)
	}
	if h.Endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}
	switch {
	case strings.HasPrefix(h.Endpoint, "tcp://"),
		strings.HasPrefix(h.Endpoint, "unix://"),
		strings.HasPrefix(h.Endpoint, "ssh://"):
	default:
		return fmt.Errorf("endpoint %q: scheme must be tcp://, unix://, or ssh://", h.Endpoint)
	}
	return nil
}
