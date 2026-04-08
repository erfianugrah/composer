package container_test

import (
	"testing"

	"github.com/erfianugrah/composer/internal/domain/container"
)

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
