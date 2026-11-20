package server_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/schmenniooo/proxie/internal/auth"
	"github.com/schmenniooo/proxie/internal/config"
	"github.com/schmenniooo/proxie/internal/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testKey is the proxie key of tenant "acme" in newServer.
const testKey = "acme-key"

// newServer returns the engine with one tenant, "acme", using testKey.
func newServer(t *testing.T, logger *slog.Logger) *gin.Engine {
	t.Helper()
	a, err := auth.New(map[string]config.Tenant{"acme": {Name: "acme", Keys: []string{testKey}}})
	require.NoError(t, err)
	return server.New(logger, a)
}

// newRequest returns a request that carries testKey.
func newRequest(t *testing.T, method, target string) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, target, nil)
	req.Header.Set(auth.Header, testKey)
	return req
}

func TestUnknownRouteReturns404(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest(t, http.MethodGet, "/nope")

	newServer(t, slog.New(slog.DiscardHandler)).ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUnknownRouteWithoutKeyReturns401(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nope", nil)

	newServer(t, slog.New(slog.DiscardHandler)).ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
