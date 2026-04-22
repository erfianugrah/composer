package docker

// Subcommand allowlists for the docker/compose exec endpoints.
//
// These are the sets of commands that may be dispatched to the Docker CLI
// via POST /api/v1/docker/exec and POST /api/v1/stacks/{name}/exec.
// Kept here (vs. duplicated in handlers) so operator/admin boundaries are
// declared in one place and surveyed together during security review.

// ComposeReadOnly are compose subcommands safe for operator+ role.
// These inspect state without modifying containers or images in a
// shell-equivalent way.
var ComposeReadOnly = map[string]bool{
	"ps":      true,
	"logs":    true,
	"top":     true,
	"config":  true,
	"images":  true,
	"port":    true,
	"version": true,
	"ls":      true,
	"events":  true,
	"build":   true,
}

// ComposeAdminOnly are compose subcommands that grant shell-equivalent
// access to containers. These require admin role even though the endpoint
// itself gates on operator+ for read-only calls.
var ComposeAdminOnly = map[string]bool{
	"exec": true,
	"cp":   true,
}

// DockerReadOnly are docker CLI subcommands safe for the admin-only
// /api/v1/docker/exec console. Writes (run, rm, etc.) are intentionally
// excluded — use dedicated endpoints.
var DockerReadOnly = map[string]bool{
	"ps":      true,
	"images":  true,
	"network": true,
	"volume":  true,
	"system":  true,
	"info":    true,
	"version": true,
	"inspect": true,
	"logs":    true,
	"stats":   true,
	"top":     true,
	"port":    true,
	"diff":    true,
	"history": true,
	"search":  true,
	"tag":     true,
}

// ComposeAllowed reports whether `cmd` is allowed for the given role.
// The first return value is true when allowed; the second is the list of
// legal values to include in a 422 error message.
func ComposeAllowed(cmd string, isAdmin bool) (allowed bool, permitted []string) {
	if ComposeReadOnly[cmd] {
		return true, nil
	}
	if isAdmin && ComposeAdminOnly[cmd] {
		return true, nil
	}
	// Build descriptive permitted list
	ro := sortedKeys(ComposeReadOnly)
	if isAdmin {
		ro = append(ro, sortedKeys(ComposeAdminOnly)...)
	}
	return false, ro
}

// DockerAllowed reports whether the docker subcommand is in the read-only
// allowlist (for admin-only /api/v1/docker/exec).
func DockerAllowed(cmd string) (allowed bool, permitted []string) {
	if DockerReadOnly[cmd] {
		return true, nil
	}
	return false, sortedKeys(DockerReadOnly)
}

// IsComposeAdminOnly reports whether a compose subcommand requires admin role.
func IsComposeAdminOnly(cmd string) bool {
	return ComposeAdminOnly[cmd]
}
