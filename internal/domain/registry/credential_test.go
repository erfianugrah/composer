package registry_test

import (
	"testing"

	"github.com/erfianugrah/composer/internal/domain/registry"
)

func TestCredential_Validate(t *testing.T) {
	cases := []struct {
		name    string
		c       registry.Credential
		wantErr bool
	}{
		{"valid global", registry.Credential{Registry: "ghcr.io", Username: "user", Secret: "pat"}, false},
		{"valid per-stack", registry.Credential{Registry: "ghcr.io", Username: "user", Secret: "pat", StackName: "bonkled"}, false},
		{"missing registry", registry.Credential{Username: "user", Secret: "pat"}, true},
		{"missing username", registry.Credential{Registry: "ghcr.io", Secret: "pat"}, true},
		{"missing secret", registry.Credential{Registry: "ghcr.io", Username: "user"}, true},
		{"whitespace registry", registry.Credential{Registry: "   ", Username: "user", Secret: "pat"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.c.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestResolve_PerStackOverridesGlobal(t *testing.T) {
	global := []*registry.Credential{
		{Registry: "ghcr.io", Username: "shared", Secret: "old"},
		{Registry: "docker.io", Username: "u", Secret: "p"},
	}
	perStack := []*registry.Credential{
		{Registry: "ghcr.io", Username: "stack-user", Secret: "new", StackName: "bonkled"},
		{Registry: "registry.example.com", Username: "u", Secret: "p", StackName: "bonkled"},
	}
	out := registry.Resolve(global, perStack)
	if len(out) != 3 {
		t.Fatalf("len=%d want 3 (ghcr.io merged, docker.io global, example per-stack)", len(out))
	}
	for _, c := range out {
		if c.Registry == "ghcr.io" && c.Username != "stack-user" {
			t.Errorf("ghcr.io username=%q want stack-user (per-stack should override global)", c.Username)
		}
	}
}

func TestResolve_EmptyInputs(t *testing.T) {
	if out := registry.Resolve(nil, nil); len(out) != 0 {
		t.Errorf("Resolve(nil,nil) returned %d entries, want 0", len(out))
	}
}

func TestCredential_IsGlobal(t *testing.T) {
	if !(&registry.Credential{}).IsGlobal() {
		t.Error("empty stack_name should be global")
	}
	if (&registry.Credential{StackName: "x"}).IsGlobal() {
		t.Error("non-empty stack_name should not be global")
	}
}
