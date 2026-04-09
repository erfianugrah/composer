package handler

import "github.com/danielgtaylor/huma/v2"

// internalError wraps an error in a 500 response.
// Returns the actual error message so users can debug issues.
func internalError() error {
	return huma.Error500InternalServerError("an internal error occurred")
}

// serverError returns a 500 with the actual error message for debugging.
func serverError(err error) error {
	if err == nil {
		return huma.Error500InternalServerError("unknown error")
	}
	return huma.Error500InternalServerError(err.Error())
}
