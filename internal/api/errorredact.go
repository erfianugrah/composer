package api

import (
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// Huma reports a validation failure by echoing the offending value back in
// ErrorDetail.Value. For a body-level failure ("unexpected property") that
// value is the WHOLE decoded request body, so every credential the caller
// submitted is reflected into the response - and from there into terminals,
// CI logs and pasted transcripts.
//
// That is reachable through ordinary use, not a corner case: several request
// bodies carry credentials by design. ConvertToGitInputBody alone has token,
// password, ssh_key and age_key, and a single misspelled field name is enough
// to trigger the echo (observed live 2026-08-07, with a real GitHub token in
// the 422 body).
//
// installErrorRedaction wraps huma.NewError so credential-shaped keys are
// replaced before serialization. Everything else survives, because the echo is
// genuinely useful for telling a caller what they got wrong.

// credentialKeys are matched case-insensitively as substrings of the JSON key,
// so `token` also covers `api_token`, `refresh_token`, `tokenValue`, and so on.
// A path is deliberately NOT a credential: ssh_key_file names a file on disk
// and is useful in an error, whereas ssh_key is the key material itself.
var credentialKeys = []string{
	"password",
	"passwd",
	"secret",
	"token",
	"api_key",
	"apikey",
	"private_key",
	"privatekey",
	"ssh_key",
	"age_key",
	"credential",
	"authorization",
}

const redactedPlaceholder = "[redacted]"

func isCredentialKey(key string) bool {
	k := strings.ToLower(key)
	// A *_file / *_path key names a location, not the material.
	if strings.HasSuffix(k, "_file") || strings.HasSuffix(k, "_path") {
		return false
	}
	for _, c := range credentialKeys {
		if strings.Contains(k, c) {
			return true
		}
	}
	return false
}

// redactValue walks a decoded JSON value, replacing credential-shaped fields.
// Depth is bounded: a hostile or cyclic-looking body should not spin here.
func redactValue(v any, depth int) any {
	if depth > 8 {
		return v
	}
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if isCredentialKey(k) {
				out[k] = redactedPlaceholder
				continue
			}
			out[k] = redactValue(val, depth+1)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = redactValue(val, depth+1)
		}
		return out
	default:
		return v
	}
}

// installErrorRedaction must run before any handler can produce an error.
// Called from HumaConfig's package init path via server setup.
func installErrorRedaction() {
	inner := huma.NewError
	huma.NewError = func(status int, msg string, errs ...error) huma.StatusError {
		se := inner(status, msg, errs...)
		model, ok := se.(*huma.ErrorModel)
		if !ok {
			return se
		}
		for _, d := range model.Errors {
			if d == nil || d.Value == nil {
				continue
			}
			d.Value = redactValue(d.Value, 0)
		}
		return model
	}
}

func init() { installErrorRedaction() }
