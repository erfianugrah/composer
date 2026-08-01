package container_test

import (
	"testing"

	"github.com/erfianugrah/composer/internal/domain/container"
)

func TestContainer_IsOneOff(t *testing.T) {
	tests := []struct {
		policy string
		want   bool
	}{
		// Unknown policy must NOT read as one-off. The container list API
		// cannot supply a restart policy, so treating "" as one-off labelled
		// every stopped service "completed".
		{"", false},
		{"no", true},
		{"on-failure", true},
		{"always", false},
		{"unless-stopped", false},
	}
	for _, tt := range tests {
		c := &container.Container{RestartPolicy: tt.policy}
		if got := c.IsOneOff(); got != tt.want {
			t.Errorf("Container{RestartPolicy:%q}.IsOneOff() = %v, want %v", tt.policy, got, tt.want)
		}
	}
}

// The compose label is the only one-off signal available on the list path, so
// it wins over the (usually absent) restart policy.
func TestContainer_IsOneOff_ComposeLabelWins(t *testing.T) {
	tests := []struct {
		name   string
		label  string
		policy string
		want   bool
	}{
		{"compose-run task, no policy known", "True", "", true},
		{"compose lowercases nothing, tolerate case", "true", "", true},
		{"service container, no policy known", "False", "", false},
		{"service container outranks restart:no", "False", "no", false},
		{"unlabelled falls back to policy", "", "no", true},
	}
	for _, tt := range tests {
		c := &container.Container{ComposeOneOff: tt.label, RestartPolicy: tt.policy}
		if got := c.IsOneOff(); got != tt.want {
			t.Errorf("%s: IsOneOff() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// Regression: the exact shape observed live - a stopped `unless-stopped`
// service arriving from the list path with no restart policy and compose's
// oneoff=False label must render as exited, not "completed".
func TestContainer_StoppedServiceIsNotCompleted(t *testing.T) {
	c := &container.Container{
		Status:        container.StatusExited,
		ExitCode:      0,
		RestartPolicy: "", // list API cannot supply this
		ComposeOneOff: "False",
	}
	if c.IsCompletedOneOff() {
		t.Error("stopped service reported as a completed one-off task")
	}
}

func TestContainer_IsCompletedOneOff(t *testing.T) {
	tests := []struct {
		name string
		c    container.Container
		want bool
	}{
		{"exited-0-no-policy", container.Container{Status: container.StatusExited, ExitCode: 0, RestartPolicy: "no"}, true},
		{"exited-0-empty-policy", container.Container{Status: container.StatusExited, ExitCode: 0, RestartPolicy: ""}, false},
		{"exited-0-compose-run", container.Container{Status: container.StatusExited, ExitCode: 0, ComposeOneOff: "True"}, true},
		{"exited-0-on-failure", container.Container{Status: container.StatusExited, ExitCode: 0, RestartPolicy: "on-failure"}, true},
		{"exited-nonzero", container.Container{Status: container.StatusExited, ExitCode: 1, RestartPolicy: "no"}, false},
		{"exited-0-always", container.Container{Status: container.StatusExited, ExitCode: 0, RestartPolicy: "always"}, false},
		{"running", container.Container{Status: container.StatusRunning, ExitCode: 0, RestartPolicy: ""}, false},
	}
	for _, tt := range tests {
		c := &tt.c
		if got := c.IsCompletedOneOff(); got != tt.want {
			t.Errorf("%s: IsCompletedOneOff() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestContainer_IsRunning(t *testing.T) {
	tests := []struct {
		status container.ContainerStatus
		want   bool
	}{
		{container.StatusRunning, true},
		{container.StatusExited, false},
		{container.StatusCreated, false},
		{container.StatusPaused, false},
		{container.StatusDead, false},
		{container.StatusRestarting, false},
		{container.StatusRemoving, false},
	}
	for _, tt := range tests {
		c := &container.Container{Status: tt.status}
		if got := c.IsRunning(); got != tt.want {
			t.Errorf("Container{Status:%q}.IsRunning() = %v, want %v", tt.status, got, tt.want)
		}
	}
}
