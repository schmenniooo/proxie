package server_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/schmenniooo/proxie/internal/auth"
	"github.com/schmenniooo/proxie/internal/config"
	"github.com/schmenniooo/proxie/internal/proxy"
	"github.com/schmenniooo/proxie/internal/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testKey is the proxie key of tenant "acme" in newServer.
const testKey = "acme-key"

const (
	defaultTimeout         = 120 * time.Second
	defaultMaxRequestBytes = 10485760
)

// fakeUpstream answers 200 "ok" on every path except /nope, which answers
// 404 "not found".
var fakeUpstream = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/nope" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	_, _ = io.WriteString(w, "ok")
})

// newServer returns the engine with one tenant, "acme", using testKey and
// forwarding to fakeUpstream.
func newServer(t *testing.T, logger *slog.Logger) *gin.Engine {
	t.Helper()
	return newServerWithUpstream(t, logger, fakeUpstream, defaultTimeout, defaultMaxRequestBytes)
}

// newServerWithUpstream returns the engine with tenant "acme" forwarding to
// a test server running upstream.
func newServerWithUpstream(t *testing.T, logger *slog.Logger, upstream http.Handler, timeout time.Duration, maxRequestBytes int64) *gin.Engine {
	t.Helper()
	srv := httptest.NewServer(upstream)
	t.Cleanup(srv.Close)
	return newServerWithURL(t, logger, srv.URL, timeout, maxRequestBytes)
}

// newServerWithURL returns the engine with tenant "acme" forwarding to
// rawURL.
func newServerWithURL(t *testing.T, logger *slog.Logger, rawURL string, timeout time.Duration, maxRequestBytes int64) *gin.Engine {
	t.Helper()
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	a, err := auth.New(map[string]config.Tenant{"acme": {Name: "acme", Keys: []string{testKey}, UpstreamURL: u}})
	require.NoError(t, err)
	return server.New(logger, a, proxy.New(proxy.Options{Timeout: timeout}), maxRequestBytes)
}

// newRequest returns a request that carries testKey.
func newRequest(t *testing.T, method, target string) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, target, nil)
	req.Header.Set(auth.Header, testKey)
	return req
}

func TestUnknownRouteGetsUpstream404(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest(t, http.MethodGet, "/upstream/v1/nope")

	newServer(t, slog.New(slog.DiscardHandler)).ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "not found\n", w.Body.String())
}

func TestUnknownRouteWithoutKeyReturns401(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nope", nil)

	newServer(t, slog.New(slog.DiscardHandler)).ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
