package middleware

import (
	"testing"
)

func TestDeriveAction(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   string
	}{
		// Stack operations
		{"POST", "/api/v1/stacks/web-app/up", "stack.up"},
		{"POST", "/api/v1/stacks/web-app/down", "stack.down"},
		{"POST", "/api/v1/stacks/web-app/restart", "stack.restart"},
		{"POST", "/api/v1/stacks/web-app/pull", "stack.pull"},
		{"POST", "/api/v1/stacks", "stack.create"},
		{"PUT", "/api/v1/stacks/web-app", "stack.update"},
		{"DELETE", "/api/v1/stacks/web-app", "stack.delete"},

		// Auth
		{"POST", "/api/v1/auth/login", "auth.login"},
		{"POST", "/api/v1/auth/bootstrap", "auth.bootstrap"},
		{"POST", "/api/v1/auth/logout", "auth.logout"},

		// Users
		{"POST", "/api/v1/users", "user.create"},
		{"PUT", "/api/v1/users/abc123", "user.update"},
		{"DELETE", "/api/v1/users/abc123", "user.delete"},
		{"PUT", "/api/v1/users/abc123/password", "user.password"},

		// Keys
		{"POST", "/api/v1/keys", "key.create"},
		{"DELETE", "/api/v1/keys/ck_abc", "key.delete"},

		// Pipelines
		{"POST", "/api/v1/pipelines", "pipeline.create"},
		{"POST", "/api/v1/pipelines/pl_123/run", "pipeline.run"},
		{"DELETE", "/api/v1/pipelines/pl_123", "pipeline.delete"},

		// Webhooks
		{"POST", "/api/v1/webhooks", "webhook.create"},
		{"DELETE", "/api/v1/webhooks/wh_123", "webhook.delete"},

		// Git
		{"POST", "/api/v1/stacks/infra/sync", "stack.sync"},

		// Containers
		{"POST", "/api/v1/containers/abc123/start", "container.start"},
		{"POST", "/api/v1/containers/abc123/stop", "container.stop"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			got := deriveAction(tt.method, tt.path)
			if got != tt.want {
				t.Errorf("deriveAction(%q, %q) = %q, want %q", tt.method, tt.path, got, tt.want)
			}
		})
	}
}
