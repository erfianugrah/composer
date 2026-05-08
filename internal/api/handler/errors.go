package handler

import (
	"context"
	"log/slog"
	"strings"

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

// dockerError surfaces Docker daemon error messages to authenticated operators.
// Container lifecycle errors (start/stop/restart) are operational diagnostics —
// port conflicts, missing GPU runtime, OCI failures — not security-sensitive.
// The full error is still logged server-side for correlation.
func dockerError(ctx context.Context, err error) error {
	reqID := chimiddleware.GetReqID(ctx)
	if err != nil {
		slog.Error("docker operation failed", "error", err.Error(), "request_id", reqID)
	}

	msg := err.Error()

	// Docker SDK errors are prefixed with "Error response from daemon: "
	// Strip the prefix to surface just the operational message.
	if after, ok := strings.CutPrefix(msg, "Error response from daemon: "); ok {
		msg = after
	}

	return huma.Error500InternalServerError(msg)
}
