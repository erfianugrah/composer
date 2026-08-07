package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

// Huma's default validation error puts the offending value into
// ErrorDetail.Value. For a body-level failure ("unexpected property") that
// value is the WHOLE request body - so any credential the caller submitted is
// reflected straight back into the response, and from there into terminals,
// logs and transcripts. Several request bodies carry credentials by design
// (ConvertToGitInputBody alone has token, password, ssh_key, age_key), so this
// is reachable through normal use, not a corner case.

type detailer struct{ d *huma.ErrorDetail }

func (e detailer) Error() string                  { return e.d.Message }
func (e detailer) ErrorDetail() *huma.ErrorDetail { return e.d }

func redactedJSON(t *testing.T, value any) string {
	t.Helper()
	err := huma.NewError(422, "validation failed", detailer{&huma.ErrorDetail{
		Message:  "unexpected property",
		Location: "body.auth_method",
		Value:    value,
	}})
	b, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("marshal: %v", marshalErr)
	}
	return string(b)
}

func TestValidationErrorRedactsCredentialFields(t *testing.T) {
	body := map[string]any{
		"repo_url":     "https://github.com/o/r.git",
		"branch":       "main",
		"auth_method":  "token",
		"token":        "gho_realtokenvalue0000000000000000000000",
		"password":     "hunter2",
		"ssh_key":      "-----BEGIN OPENSSH PRIVATE KEY-----",
		"age_key":      "AGE-SECRET-KEY-1QQQQ",
		"ssh_key_file": "/home/composer/.ssh/id_gh",
	}
	got := redactedJSON(t, body)

	for _, secret := range []string{
		"gho_realtokenvalue0000000000000000000000",
		"hunter2",
		"BEGIN OPENSSH PRIVATE KEY",
		"AGE-SECRET-KEY-1QQQQ",
	} {
		if strings.Contains(got, secret) {
			t.Errorf("credential leaked into the error response: %q\nfull body: %s", secret, got)
		}
	}

	// Non-credential fields must survive - the point of echoing the value is to
	// tell the caller what they sent wrong.
	for _, keep := range []string{"repo_url", "https://github.com/o/r.git", "branch", "main", "auth_method"} {
		if !strings.Contains(got, keep) {
			t.Errorf("redaction ate a non-credential field %q\nfull body: %s", keep, got)
		}
	}
	// A path is not a secret; the filename is useful for debugging.
	if !strings.Contains(got, "/home/composer/.ssh/id_gh") {
		t.Errorf("ssh_key_file is a path, not a credential, and should survive\nfull body: %s", got)
	}
}

func TestValidationErrorRedactsNestedAndSliced(t *testing.T) {
	got := redactedJSON(t, map[string]any{
		"stack": map[string]any{"env": map[string]any{"token": "gho_nested"}},
		"list":  []any{map[string]any{"password": "deep"}},
	})
	for _, secret := range []string{"gho_nested", "deep"} {
		if strings.Contains(got, secret) {
			t.Errorf("credential leaked from a nested value: %q\nfull body: %s", secret, got)
		}
	}
}

func TestValidationErrorLeavesPlainValuesAlone(t *testing.T) {
	got := redactedJSON(t, "not-an-object")
	if !strings.Contains(got, "not-an-object") {
		t.Errorf("a scalar value must pass through untouched: %s", got)
	}
}

func TestNonValidationErrorsStillCarryTheirMessage(t *testing.T) {
	err := huma.NewError(500, "boom")
	b, _ := json.Marshal(err)
	if !strings.Contains(string(b), "boom") {
		t.Errorf("plain errors must be unaffected: %s", string(b))
	}
}
