package server_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/schmenniooo/proxie/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captured is what the upstream received.
type captured struct {
	mu     sync.Mutex
	path   string
	query  string
	header http.Header
	body   string
}

func (c *captured) handler(status int, respBody string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.path, c.query, c.header, c.body = r.URL.Path, r.URL.RawQuery, r.Header.Clone(), string(b)
		c.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "r1")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	})
}

func newBodyRequest(t *testing.T, method, target, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
	req.Header.Set(auth.Header, testKey)
	return req
}

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestProxyForwardsRequest(t *testing.T) {
	var c captured
	w := httptest.NewRecorder()
	req := newBodyRequest(t, http.MethodPost, "/v1/messages?x=1", `{"model":"m"}`)
	req.Header.Set("Authorization", "Bearer provider-key")

	newServerWithUpstream(t, discard(), c.handler(http.StatusCreated, `{"id":"1"}`), defaultTimeout, defaultMaxRequestBytes).
		ServeHTTP(w, req)

	c.mu.Lock()
	defer c.mu.Unlock()
	assert.Equal(t, "/v1/messages", c.path)
	assert.Equal(t, "x=1", c.query)
	assert.JSONEq(t, `{"model":"m"}`, c.body)
	assert.Equal(t, "Bearer provider-key", c.header.Get("Authorization"))
	assert.Empty(t, c.header.Values(auth.Header))
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "r1", w.Header().Get("X-Request-Id"))
	assert.JSONEq(t, `{"id":"1"}`, w.Body.String())
}

func TestProxyPassesUpstreamErrors(t *testing.T) {
	var c captured
	w := httptest.NewRecorder()

	newServerWithUpstream(t, discard(), c.handler(http.StatusTooManyRequests, `{"error":"slow down"}`), defaultTimeout, defaultMaxRequestBytes).
		ServeHTTP(w, newRequest(t, http.MethodPost, "/v1/messages"))

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.JSONEq(t, `{"error":"slow down"}`, w.Body.String())
}

func TestProxyForwardsOversizeBody(t *testing.T) {
	var c captured
	body := strings.Repeat("x", 25)
	w := httptest.NewRecorder()

	newServerWithUpstream(t, discard(), c.handler(http.StatusOK, "{}"), defaultTimeout, 10).
		ServeHTTP(w, newBodyRequest(t, http.MethodPost, "/v1/messages", body))

	assert.Equal(t, http.StatusOK, w.Code)
	c.mu.Lock()
	defer c.mu.Unlock()
	assert.Equal(t, body, c.body)
}

func TestProxyRejectsDotSegments(t *testing.T) {
	var c captured
	w := httptest.NewRecorder()

	newServerWithUpstream(t, discard(), c.handler(http.StatusOK, "{}"), defaultTimeout, defaultMaxRequestBytes).
		ServeHTTP(w, newRequest(t, http.MethodGet, "/v1/%2e%2e/admin"))

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.JSONEq(t, `{"error":"invalid request path"}`, w.Body.String())
	c.mu.Lock()
	defer c.mu.Unlock()
	assert.Empty(t, c.path)
}

// sleepyUpstream optionally sends part of a response, then sleeps until the
// test ends or 500ms pass.
func sleepyUpstream(t *testing.T, partial bool) http.Handler {
	t.Helper()
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if partial {
			w.Header().Set("Content-Length", "100")
			_, _ = io.WriteString(w, "0123456789")
			w.(http.Flusher).Flush()
		}
		select {
		case <-time.After(500 * time.Millisecond):
		case <-done:
		}
	})
}

func TestProxyUpstreamTimeoutReturns504(t *testing.T) {
	w := httptest.NewRecorder()

	newServerWithUpstream(t, discard(), sleepyUpstream(t, false), 50*time.Millisecond, defaultMaxRequestBytes).
		ServeHTTP(w, newRequest(t, http.MethodPost, "/v1/messages"))

	assert.Equal(t, http.StatusGatewayTimeout, w.Code)
	assert.JSONEq(t, `{"error":"upstream timeout"}`, w.Body.String())
}

func TestProxyUpstreamDownReturns502(t *testing.T) {
	logger, buf := bufferLogger()
	w := httptest.NewRecorder()

	newServerWithURL(t, logger, "http://127.0.0.1:1/?key=s3cret", defaultTimeout, defaultMaxRequestBytes).
		ServeHTTP(w, newRequest(t, http.MethodPost, "/v1/messages"))

	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.JSONEq(t, `{"error":"upstream unavailable"}`, w.Body.String())
	assert.Contains(t, buf.String(), `"msg":"upstream request failed"`)
	assert.Contains(t, buf.String(), `"tenant":"acme"`)
	assert.NotContains(t, buf.String(), "s3cret")
}

func TestProxyTimeoutMidBodyBreaksConnection(t *testing.T) {
	engine := newServerWithUpstream(t, discard(), sleepyUpstream(t, true), 50*time.Millisecond, defaultMaxRequestBytes)
	proxie := httptest.NewServer(engine)
	t.Cleanup(proxie.Close)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, proxie.URL+"/v1/x", nil)
	require.NoError(t, err)
	req.Header.Set(auth.Header, testKey)

	// Depending on buffering, the cut shows up before the headers or while
	// reading the body. Either way the caller must see an error, never a
	// clean end.
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		defer func() { _ = resp.Body.Close() }()
		_, err = io.ReadAll(resp.Body)
	}

	assert.Error(t, err)
}

func TestProxyPassesEmptyUpstream404(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusNotFound)
	})
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			w := httptest.NewRecorder()

			newServerWithUpstream(t, discard(), upstream, defaultTimeout, defaultMaxRequestBytes).
				ServeHTTP(w, newRequest(t, method, "/v1/missing"))

			assert.Equal(t, http.StatusNotFound, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
			assert.Empty(t, w.Body.String())
		})
	}
}

func TestProxyCallerGoneLogs499(t *testing.T) {
	logger, buf := bufferLogger()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	req := newRequest(t, http.MethodPost, "/v1/messages").WithContext(ctx)

	newServer(t, logger).ServeHTTP(httptest.NewRecorder(), req)

	assert.Contains(t, buf.String(), `"status":499`)
	assert.NotContains(t, buf.String(), `"status":404`)
}
