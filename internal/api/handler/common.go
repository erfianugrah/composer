package handler

import "net/http"

// Common error response sets reused across handlers so the OpenAPI spec
// enumerates the actual codes each endpoint can return. Huma automatically
// appends 422 (when body/paths present) and 500 when Errors is non-empty,
// so these helpers only list operation-specific codes.

// errsViewer is for read-only viewer+ endpoints.
var errsViewer = []int{
	http.StatusUnauthorized,
	http.StatusForbidden,
}

// errsViewerNotFound adds 404 for viewer-level resource lookups.
var errsViewerNotFound = []int{
	http.StatusUnauthorized,
	http.StatusForbidden,
	http.StatusNotFound,
}

// errsOperatorMutation is for operator+ mutations that may conflict or not-find.
var errsOperatorMutation = []int{
	http.StatusUnauthorized,
	http.StatusForbidden,
	http.StatusNotFound,
	http.StatusConflict,
}

// errsAdminMutation is for admin-only mutations.
var errsAdminMutation = []int{
	http.StatusUnauthorized,
	http.StatusForbidden,
	http.StatusNotFound,
}

// errsAuthLogin is for login (rate-limited, can 401 on bad creds).
var errsAuthLogin = []int{
	http.StatusUnauthorized,
	http.StatusTooManyRequests,
}

// errsAuthBootstrap is for first-admin creation (409 once done).
var errsAuthBootstrap = []int{
	http.StatusConflict,
}

// errsDockerDependent is for endpoints that require the Docker daemon.
var errsDockerDependent = []int{
	http.StatusUnauthorized,
	http.StatusForbidden,
	http.StatusNotFound,
	http.StatusServiceUnavailable,
}

// errsPublic is for endpoints with no auth (health, templates).
var errsPublic = []int{}
