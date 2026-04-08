package handler

import "github.com/danielgtaylor/huma/v2"

// internalError returns a generic 500 error without leaking internal details.
// The actual error should be logged server-side; clients see a safe message.
func internalError() error {
	return huma.Error500InternalServerError("an internal error occurred")
}
