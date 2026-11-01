package server_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/schmenniooo/proxie/internal/server"
	"github.com/stretchr/testify/assert"
)

func TestHelloRouteIsRegistered(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/hello", nil)

	server.New(slog.New(slog.DiscardHandler)).ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHelloRouteRejectsPost(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/hello", nil)

	server.New(slog.New(slog.DiscardHandler)).ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}
