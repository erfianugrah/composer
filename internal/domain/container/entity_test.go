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
		{"", true},
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

func TestContainer_IsCompletedOneOff(t *testing.T) {
	tests := []struct {
		name   string
		c      container.Container
		want   bool
	}{
		{"exited-0-no-policy", container.Container{Status: container.StatusExited, ExitCode: 0, RestartPolicy: "no"}, true},
		{"exited-0-empty-policy", container.Container{Status: container.StatusExited, ExitCode: 0, RestartPolicy: ""}, true},
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
