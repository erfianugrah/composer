package handler

import (
	"context"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// serverError returns a generic 500 to the client and logs the actual error
// server-side. Never leaks internal details (DB paths, Docker errors, etc.)
// to API consumers.
//
// When a request ID is present in the context (set by chi.RequestID middleware),
// it's included in the client-facing message so users can correlate failures
// with server logs without exposing the underlying error.
func serverError(ctx context.Context, err error) error {
	reqID := chimiddleware.GetReqID(ctx)
	if err != nil {
		slog.Error("internal server error", "error", err.Error(), "request_id", reqID)
	}
	if reqID != "" {
		return huma.Error500InternalServerError("an internal error occurred (request_id: " + reqID + ")")
	}
	return huma.Error500InternalServerError("an internal error occurred")
}
