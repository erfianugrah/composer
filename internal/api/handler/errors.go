package handler

import (
	"log/slog"

	"github.com/danielgtaylor/huma/v2"
)

// serverError returns a generic 500 to the client and logs the actual error
// server-side. Never leaks internal details (DB paths, Docker errors, etc.)
// to API consumers.
func serverError(err error) error {
	if err != nil {
		slog.Error("internal server error", "error", err.Error())
	}
	return huma.Error500InternalServerError("an internal error occurred")
}
