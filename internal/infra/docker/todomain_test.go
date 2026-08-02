package docker

import (
	"testing"

	"github.com/docker/docker/api/types/container"
)

// TestToDomainContainer_OneOffLabel pins the list-path mapping that decides
// whether the UI calls a stopped container "completed" or "exited".
//
// The regression this guards: the mapping used to read a
// `com.docker.compose.restart` label, which docker compose does not emit. The
// field was therefore always empty, the one-off heuristic treated empty as
// "task", and every service that exited cleanly rendered as a successful
// completed job. Verified against live containers: compose emits
// `com.docker.compose.oneoff` ("False" for service containers, "True" for
// `docker compose run`) and no restart label at all.
func TestToDomainContainer_OneOffLabel(t *testing.T) {
	tests := []struct {
		name          string
		labels        map[string]string
		state         string
		status        string
		wantOneOff    string
		wantCompleted bool
	}{
		{
			name: "stopped service container is not a completed task",
			labels: map[string]string{
				"com.docker.compose.project": "servarr",
				"com.docker.compose.service": "sonarr",
				"com.docker.compose.oneoff":  "False",
			},
			state:         "exited",
			status:        "Exited (0) 2 minutes ago",
			wantOneOff:    "False",
			wantCompleted: false,
		},
		{
			name: "compose run task that exited cleanly is completed",
			labels: map[string]string{
				"com.docker.compose.project": "servarr",
				"com.docker.compose.service": "migrate",
				"com.docker.compose.oneoff":  "True",
			},
			state:         "exited",
			status:        "Exited (0) 5 seconds ago",
			wantOneOff:    "True",
			wantCompleted: true,
		},
		{
			name: "one-off that failed is not completed",
			labels: map[string]string{
				"com.docker.compose.oneoff": "True",
			},
			state:         "exited",
			status:        "Exited (1) 5 seconds ago",
			wantOneOff:    "True",
			wantCompleted: false,
		},
		{
			name:          "unlabelled container is not assumed to be a task",
			labels:        map[string]string{},
			state:         "exited",
			status:        "Exited (0) 1 minute ago",
			wantOneOff:    "",
			wantCompleted: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toDomainContainer(container.Summary{
				ID:     "0123456789abcdef",
				Names:  []string{"/probe"},
				State:  tt.state,
				Status: tt.status,
				Labels: tt.labels,
			})
			if got.ComposeOneOff != tt.wantOneOff {
				t.Errorf("ComposeOneOff = %q, want %q", got.ComposeOneOff, tt.wantOneOff)
			}
			if completed := got.IsCompletedOneOff(); completed != tt.wantCompleted {
				t.Errorf("IsCompletedOneOff() = %v, want %v", completed, tt.wantCompleted)
			}
		})
	}
}

// The list API cannot report a restart policy (it returns only
// HostConfig.NetworkMode), so the mapping must not invent one.
func TestToDomainContainer_LeavesRestartPolicyEmpty(t *testing.T) {
	got := toDomainContainer(container.Summary{
		ID:     "0123456789abcdef",
		Names:  []string{"/probe"},
		State:  "running",
		Labels: map[string]string{"com.docker.compose.project": "servarr"},
	})
	if got.RestartPolicy != "" {
		t.Errorf("RestartPolicy = %q, want empty - the list API cannot supply it", got.RestartPolicy)
	}
}
