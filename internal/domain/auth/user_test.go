package auth_test

import (
	"testing"
	"time"

	"github.com/erfianugrah/composer/internal/domain/auth"
)

func TestNewUser(t *testing.T) {
	u, err := auth.NewUser("test@example.com", "strongpassword1", auth.RoleAdmin)
	if err != nil {
		t.Fatalf("NewUser() error: %v", err)
	}

	if u.ID == "" {
		t.Error("expected non-empty ID")
	}
	if u.Email != "test@example.com" {
		t.Errorf("email = %q, want %q", u.Email, "test@example.com")
	}
	if u.Role != auth.RoleAdmin {
		t.Errorf("role = %q, want %q", u.Role, auth.RoleAdmin)
	}
	if u.PasswordHash == "" {
		t.Error("expected non-empty password hash")
	}
	if u.PasswordHash == "strongpassword1" {
		t.Error("password hash should not equal plaintext")
	}
	if u.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
}

func TestNewUser_EmailNormalization(t *testing.T) {
	u, err := auth.NewUser("Test@EXAMPLE.COM", "strongpassword1", auth.RoleViewer)
	if err != nil {
		t.Fatalf("NewUser() error: %v", err)
	}
	if u.Email != "test@example.com" {
		t.Errorf("email = %q, want lowercase %q", u.Email, "test@example.com")
	}
}

func TestNewUser_Validation(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		password string
		role     auth.Role
	}{
		{"empty email", "", "strongpassword1", auth.RoleAdmin},
		{"no at sign", "testexample.com", "strongpassword1", auth.RoleAdmin},
		{"short password", "test@example.com", "short", auth.RoleAdmin},
		{"empty password", "test@example.com", "", auth.RoleAdmin},
		{"invalid role", "test@example.com", "strongpassword1", auth.Role("root")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := auth.NewUser(tt.email, tt.password, tt.role)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestUser_VerifyPassword(t *testing.T) {
	u, err := auth.NewUser("test@example.com", "correctpassword", auth.RoleAdmin)
	if err != nil {
		t.Fatalf("NewUser() error: %v", err)
	}

	if !u.VerifyPassword("correctpassword") {
		t.Error("VerifyPassword(correct) = false, want true")
	}
	if u.VerifyPassword("wrongpassword") {
		t.Error("VerifyPassword(wrong) = true, want false")
	}
	if u.VerifyPassword("") {
		t.Error("VerifyPassword(empty) = true, want false")
	}
}

func TestUser_ChangePassword(t *testing.T) {
	u, err := auth.NewUser("test@example.com", "oldpassword123", auth.RoleAdmin)
	if err != nil {
		t.Fatalf("NewUser() error: %v", err)
	}

	oldHash := u.PasswordHash
	oldUpdated := u.UpdatedAt

	// Small sleep to ensure timestamp differs
	time.Sleep(time.Millisecond)

	err = u.ChangePassword("oldpassword123", "newpassword456")
	if err != nil {
		t.Fatalf("ChangePassword() error: %v", err)
	}

	if u.PasswordHash == oldHash {
		t.Error("password hash should have changed")
	}
	if !u.VerifyPassword("newpassword456") {
		t.Error("new password should verify")
	}
	if u.VerifyPassword("oldpassword123") {
		t.Error("old password should no longer verify")
	}
	if !u.UpdatedAt.After(oldUpdated) {
		t.Error("UpdatedAt should have advanced")
	}
}

func TestUser_ChangePassword_WrongOld(t *testing.T) {
	u, err := auth.NewUser("test@example.com", "realpassword99", auth.RoleAdmin)
	if err != nil {
		t.Fatalf("NewUser() error: %v", err)
	}

	err = u.ChangePassword("wrongpassword", "newpassword456")
	if err == nil {
		t.Error("expected error for wrong old password")
	}
}

func TestUser_UpdateRole(t *testing.T) {
	u, err := auth.NewUser("test@example.com", "strongpassword1", auth.RoleViewer)
	if err != nil {
		t.Fatalf("NewUser() error: %v", err)
	}

	err = u.UpdateRole(auth.RoleOperator)
	if err != nil {
		t.Fatalf("UpdateRole() error: %v", err)
	}
	if u.Role != auth.RoleOperator {
		t.Errorf("role = %q, want %q", u.Role, auth.RoleOperator)
	}

	err = u.UpdateRole(auth.Role("invalid"))
	if err == nil {
		t.Error("expected error for invalid role")
	}
}
