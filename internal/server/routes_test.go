package server_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHelloRouteIsRegistered(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest(t, http.MethodGet, "/hello")

	newServer(t, slog.New(slog.DiscardHandler)).ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHelloRouteRejectsPost(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest(t, http.MethodPost, "/hello")

	newServer(t, slog.New(slog.DiscardHandler)).ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}
