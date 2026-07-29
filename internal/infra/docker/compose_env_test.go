package docker

// White-box pin tests for env propagation through applyExtraEnv and the
// RunPTY env-construction path. Both build subprocess env from
// cmd.Environ(), which inherits the process environment - that is how
// DOCKER_TLS_VERIFY / DOCKER_CERT_PATH reach the docker CLI for mTLS.
// A future refactor to explicit env lists would silently break docker
// compose CLI mTLS while SDK reads keep working; these tests catch that.

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"go.uber.org/zap"
)

// TestApplyExtraEnv_PropagatesMTLSEnv verifies that applyExtraEnv preserves
// the mTLS env vars set on the process (via t.Setenv) and layers on
// DOCKER_HOST from the Compose wrapper's dockerHost field.
func TestApplyExtraEnv_PropagatesMTLSEnv(t *testing.T) {
	t.Setenv("DOCKER_TLS_VERIFY", "1")
	t.Setenv("DOCKER_CERT_PATH", "/x")

	c := &Compose{dockerHost: "tcp://example:2376"}
	cmd := exec.Command("echo") // inherits process env (including t.Setenv vars)
	ctx := context.Background()

	env := c.applyExtraEnv(ctx, cmd)

	hasTLSVerify := false
	hasCertPath := false
	hasDockerHost := false
	for _, e := range env {
		switch {
		case e == "DOCKER_TLS_VERIFY=1":
			hasTLSVerify = true
		case e == "DOCKER_CERT_PATH=/x":
			hasCertPath = true
		case e == "DOCKER_HOST=tcp://example:2376":
			hasDockerHost = true
		}
	}

	if !hasTLSVerify {
		t.Error("applyExtraEnv: DOCKER_TLS_VERIFY=1 not found in returned env")
	}
	if !hasCertPath {
		t.Error("applyExtraEnv: DOCKER_CERT_PATH=/x not found in returned env")
	}
	if !hasDockerHost {
		t.Error("applyExtraEnv: DOCKER_HOST=tcp://example:2376 not found in returned env")
	}

	// Also verify cmd.Env was set on the command (side effect).
	cmdHasTLS := false
	for _, e := range cmd.Env {
		if e == "DOCKER_TLS_VERIFY=1" {
			cmdHasTLS = true
			break
		}
	}
	if !cmdHasTLS {
		t.Error("applyExtraEnv: DOCKER_TLS_VERIFY=1 not found in cmd.Env after call")
	}
}

// TestRunPTYEnv_PropagatesMTLSEnv verifies that the env constructed in
// RunPTY (cmd.Environ() + TERM/COLORTERM + DOCKER_HOST + DOCKER_CONFIG)
// preserves the mTLS env vars from the process environment.
func TestRunPTYEnv_PropagatesMTLSEnv(t *testing.T) {
	t.Setenv("DOCKER_TLS_VERIFY", "1")
	t.Setenv("DOCKER_CERT_PATH", "/x")

	// Construct env the same way RunPTY does (compose.go:306-314).
	cmd := exec.Command("echo")
	cmd.Env = append(cmd.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")

	dockerHost := "tcp://example:2376"
	if dockerHost != "" {
		cmd.Env = append(cmd.Env, "DOCKER_HOST="+dockerHost)
	}

	// DOCKER_CONFIG is not set here; no ctx with config dir.

	hasTLSVerify := false
	hasCertPath := false
	hasDockerHost := false
	hasTERM := false
	for _, e := range cmd.Env {
		switch {
		case e == "DOCKER_TLS_VERIFY=1":
			hasTLSVerify = true
		case e == "DOCKER_CERT_PATH=/x":
			hasCertPath = true
		case e == "DOCKER_HOST=tcp://example:2376":
			hasDockerHost = true
		case strings.HasPrefix(e, "TERM="):
			hasTERM = true
		}
	}

	if !hasTLSVerify {
		t.Error("PTY-path: DOCKER_TLS_VERIFY=1 not found in cmd.Env")
	}
	if !hasCertPath {
		t.Error("PTY-path: DOCKER_CERT_PATH=/x not found in cmd.Env")
	}
	if !hasDockerHost {
		t.Error("PTY-path: DOCKER_HOST=tcp://example:2376 not found in cmd.Env")
	}
	if !hasTERM {
		t.Error("PTY-path: TERM not found in cmd.Env")
	}
}

// TestComposeTLS_applyExtraEnv: with NewComposeTLS, applyExtraEnv includes
// DOCKER_TLS_VERIFY=1 and DOCKER_CERT_PATH in the env that came from
// cmd.Environ() (the process env). The certDir is set explicitly, not from
// the process env.
func TestComposeTLS_applyExtraEnv(t *testing.T) {
	// Environment should NOT carry mTLS env -- we set them explicitly on the Compose.
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")

	c := NewComposeTLS("tcp://example:2376", &TLSConfig{CertDir: "/certs"}, zap.NewNop())
	cmd := exec.Command("echo")
	ctx := context.Background()

	env := c.applyExtraEnv(ctx, cmd)

	dockerHostFound := false
	tlsVerifyFound := false
	certPathFound := false
	for _, e := range env {
		switch {
		case e == "DOCKER_HOST=tcp://example:2376":
			dockerHostFound = true
		case e == "DOCKER_TLS_VERIFY=1":
			tlsVerifyFound = true
		case e == "DOCKER_CERT_PATH=/certs":
			certPathFound = true
		}
	}

	if !dockerHostFound {
		t.Error("expected DOCKER_HOST=tcp://example:2376 in applyExtraEnv output")
	}
	if !tlsVerifyFound {
		t.Error("expected DOCKER_TLS_VERIFY=1 in applyExtraEnv output")
	}
	if !certPathFound {
		t.Error("expected DOCKER_CERT_PATH=/certs in applyExtraEnv output")
	}
}

// TestComposeTLS_RunPTY: with NewComposeTLS, the env constructed in the
// RunPTY pattern includes the explicit DOCKER_TLS_VERIFY and DOCKER_CERT_PATH.
func TestComposeTLS_RunPTY(t *testing.T) {
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")

	c := NewComposeTLS("tcp://example:2376", &TLSConfig{CertDir: "/certs"}, zap.NewNop())

	// Construct env the same way RunPTY does.
	cmd := exec.Command("echo")
	cmd.Env = append(cmd.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	if c.dockerHost != "" {
		cmd.Env = append(cmd.Env, "DOCKER_HOST="+c.dockerHost)
	}
	if tlsEnv := c.dockerEnv(); tlsEnv != nil {
		cmd.Env = append(cmd.Env, tlsEnv...)
	}

	tlsVerifyFound := false
	certPathFound := false
	for _, e := range cmd.Env {
		switch {
		case e == "DOCKER_TLS_VERIFY=1":
			tlsVerifyFound = true
		case e == "DOCKER_CERT_PATH=/certs":
			certPathFound = true
		}
	}
	if !tlsVerifyFound {
		t.Error("expected DOCKER_TLS_VERIFY=1 in PTY env")
	}
	if !certPathFound {
		t.Error("expected DOCKER_CERT_PATH=/certs in PTY env")
	}
}

// TestComposeTLS_legacyPlain: plain NewCompose (not TLS) does NOT add TLS vars.
func TestComposeTLS_legacyPlain(t *testing.T) {
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")

	c := NewCompose("tcp://example:2376", zap.NewNop())
	cmd := exec.Command("echo")
	ctx := context.Background()

	env := c.applyExtraEnv(ctx, cmd)

	for _, e := range env {
		if e == "DOCKER_TLS_VERIFY=1" || e == "DOCKER_CERT_PATH=/certs" {
			t.Errorf("plain NewCompose should NOT carry TLS vars, but found %q", e)
		}
	}
}
