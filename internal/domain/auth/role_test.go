package auth_test

import (
	"testing"

	"github.com/erfianugrah/composer/internal/domain/auth"
)

func TestRole_Valid(t *testing.T) {
	tests := []struct {
		role  auth.Role
		valid bool
	}{
		{auth.RoleAdmin, true},
		{auth.RoleOperator, true},
		{auth.RoleViewer, true},
		{auth.Role("superadmin"), false},
		{auth.Role(""), false},
	}
	for _, tt := range tests {
		if got := tt.role.Valid(); got != tt.valid {
			t.Errorf("Role(%q).Valid() = %v, want %v", tt.role, got, tt.valid)
		}
	}
}

func TestRole_AtLeast(t *testing.T) {
	tests := []struct {
		role auth.Role
		min  auth.Role
		want bool
	}{
		{auth.RoleAdmin, auth.RoleAdmin, true},
		{auth.RoleAdmin, auth.RoleOperator, true},
		{auth.RoleAdmin, auth.RoleViewer, true},
		{auth.RoleOperator, auth.RoleAdmin, false},
		{auth.RoleOperator, auth.RoleOperator, true},
		{auth.RoleOperator, auth.RoleViewer, true},
		{auth.RoleViewer, auth.RoleAdmin, false},
		{auth.RoleViewer, auth.RoleOperator, false},
		{auth.RoleViewer, auth.RoleViewer, true},
	}
	for _, tt := range tests {
		if got := tt.role.AtLeast(tt.min); got != tt.want {
			t.Errorf("Role(%q).AtLeast(%q) = %v, want %v", tt.role, tt.min, got, tt.want)
		}
	}
}

func TestParseRole(t *testing.T) {
	t.Run("valid roles", func(t *testing.T) {
		for _, s := range []string{"admin", "operator", "viewer"} {
			r, err := auth.ParseRole(s)
			if err != nil {
				t.Errorf("ParseRole(%q) unexpected error: %v", s, err)
			}
			if string(r) != s {
				t.Errorf("ParseRole(%q) = %q, want %q", s, r, s)
			}
		}
	})

	t.Run("invalid roles", func(t *testing.T) {
		for _, s := range []string{"", "root", "ADMIN", "Admin"} {
			_, err := auth.ParseRole(s)
			if err == nil {
				t.Errorf("ParseRole(%q) expected error, got nil", s)
			}
		}
	})
}
