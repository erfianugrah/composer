package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/erfianugrah/composer/internal/api/middleware"
)

func TestStoreRemoteIP(t *testing.T) {
	var gotIP string
	handler := middleware.StoreRemoteIP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIP = middleware.RemoteIPFromContext(r.Context())
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:54321"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, "192.168.1.100:54321", gotIP)
}

func TestRemoteIPFromContext_Empty(t *testing.T) {
	// Without middleware, should return empty string
	req := httptest.NewRequest("GET", "/test", nil)
	ip := middleware.RemoteIPFromContext(req.Context())
	assert.Empty(t, ip)
}

func TestExtendWriteDeadline_SSEPath(t *testing.T) {
	var called bool
	handler := middleware.ExtendWriteDeadline(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/v1/sse/events", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.True(t, called, "handler should be called for SSE path")
	assert.Equal(t, 200, w.Code)
}

func TestExtendWriteDeadline_WSPath(t *testing.T) {
	var called bool
	handler := middleware.ExtendWriteDeadline(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/v1/ws/terminal/abc123", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.True(t, called)
}

func TestExtendWriteDeadline_RegularPath(t *testing.T) {
	var called bool
	handler := middleware.ExtendWriteDeadline(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/v1/stacks", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.True(t, called)
	assert.Equal(t, 200, w.Code)
}
