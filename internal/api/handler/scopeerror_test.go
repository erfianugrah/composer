package handler

import (
	"errors"
	"fmt"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

// A container that is absent and a container that is out of scope are
// different answers to different questions. Collapsing both into 403 told the
// operator "forbidden" when the real cause was almost always a request aimed
// at the wrong docker host - which is exactly how the multi-host bug presented.
func TestScopeError_StatusPerCause(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"missing container is 404", fmt.Errorf("%w: abc123", errContainerNotFound), 404},
		{"bare not-found is 404", errContainerNotFound, 404},
		{"out-of-scope container is 403", errContainerNotInComposeStack, 403},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var se huma.StatusError
			if !errors.As(scopeError(tt.err), &se) {
				t.Fatalf("scopeError(%v) is not a huma.StatusError", tt.err)
			}
			if got := se.GetStatus(); got != tt.want {
				t.Errorf("status = %d, want %d", got, tt.want)
			}
		})
	}
}

// The id must survive into the message: "container not found" alone gives the
// operator nothing to check against.
func TestScopeError_KeepsContainerID(t *testing.T) {
	err := scopeError(fmt.Errorf("%w: deadbeef1234", errContainerNotFound))
	if got := err.Error(); !contains(got, "deadbeef1234") {
		t.Errorf("message %q lost the container id", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
