package app

import (
	"reflect"
	"testing"
)

// dockerExecArgv is unexported so tests live in the same package.

func TestDockerExecArgv_CmdSlice(t *testing.T) {
	// map[string]any sourced from JSON decoding — []any not []string
	cfg := map[string]any{
		"container": "wafctl",
		"cmd":       []any{"wget", "-qO-", "http://localhost/deploy"},
	}
	argv, err := dockerExecArgv(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"wget", "-qO-", "http://localhost/deploy"}
	if !reflect.DeepEqual(argv, want) {
		t.Errorf("argv = %q, want %q", argv, want)
	}
}

func TestDockerExecArgv_CmdStringSlice(t *testing.T) {
	// Go-native []string form — covers callers that skip JSON round-trip
	cfg := map[string]any{
		"container": "wafctl",
		"cmd":       []string{"env"},
	}
	argv, err := dockerExecArgv(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(argv, []string{"env"}) {
		t.Errorf("argv = %q, want [env]", argv)
	}
}

func TestDockerExecArgv_CommandString(t *testing.T) {
	// Fallback form — wrapped in sh -c for shell-operator support
	cfg := map[string]any{
		"container": "wafctl",
		"command":   "echo hi && echo bye",
	}
	argv, err := dockerExecArgv(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"sh", "-c", "echo hi && echo bye"}
	if !reflect.DeepEqual(argv, want) {
		t.Errorf("argv = %q, want %q", argv, want)
	}
}

func TestDockerExecArgv_CmdPreferredOverCommand(t *testing.T) {
	// Both forms present: cmd takes precedence (quote-safe)
	cfg := map[string]any{
		"container": "wafctl",
		"cmd":       []any{"true"},
		"command":   "false",
	}
	argv, err := dockerExecArgv(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(argv, []string{"true"}) {
		t.Errorf("argv = %q, want [true]", argv)
	}
}

func TestDockerExecArgv_EmptyCmdFallsThrough(t *testing.T) {
	// Empty cmd slice means "not provided" — fall back to command
	cfg := map[string]any{
		"container": "wafctl",
		"cmd":       []any{},
		"command":   "fallback",
	}
	argv, err := dockerExecArgv(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(argv, []string{"sh", "-c", "fallback"}) {
		t.Errorf("argv = %q, want [sh -c fallback]", argv)
	}
}

func TestDockerExecArgv_MissingBoth(t *testing.T) {
	cfg := map[string]any{
		"container": "wafctl",
	}
	_, err := dockerExecArgv(cfg)
	if err == nil {
		t.Fatal("expected error when neither cmd nor command is provided")
	}
}

func TestDockerExecArgv_NonStringCmdEntry(t *testing.T) {
	// []any with a non-string entry — should fail with a clear message
	cfg := map[string]any{
		"cmd": []any{"wget", 42, "-qO-"},
	}
	_, err := dockerExecArgv(cfg)
	if err == nil {
		t.Fatal("expected error when cmd entry is not a string")
	}
}
