package server_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/schmenniooo/proxie/internal/server"
	"github.com/stretchr/testify/assert"
)

func TestHelloHandlerBody(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/hello", nil)

	server.New(slog.New(slog.DiscardHandler)).ServeHTTP(w, req)

	assert.JSONEq(t, `{"message":"Hello, World!"}`, w.Body.String())
}

func TestNotFoundHandlerBody(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nope", nil)

	server.New(slog.New(slog.DiscardHandler)).ServeHTTP(w, req)

	assert.Equal(t, "404 page not found", w.Body.String())
}
