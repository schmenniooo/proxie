package proxy_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/schmenniooo/proxie/internal/config"
	"github.com/schmenniooo/proxie/internal/proxy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seen is what the fake upstream received.
type seen struct {
	method        string
	requestURI    string
	host          string
	header        http.Header
	body          string
	contentLength int64
	te            []string
}

// recordingUpstream starts an upstream that records every request and
// answers 200 "ok".
func recordingUpstream(t *testing.T) (*httptest.Server, *[]seen) {
	t.Helper()
	var got []seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, seen{
			method:        r.Method,
			requestURI:    r.RequestURI,
			host:          r.Host,
			header:        r.Header.Clone(),
			body:          string(b),
			contentLength: r.ContentLength,
			te:            r.TransferEncoding,
		})
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func tenantFor(t *testing.T, rawURL string) config.Tenant {
	t.Helper()
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	return config.Tenant{Name: "acme", UpstreamURL: u}
}

// forward forwards r and returns the upstream status and body.
func forward(t *testing.T, f *proxy.Forwarder, tenant config.Tenant, r *http.Request) (int, string) {
	t.Helper()
	resp, err := f.Forward(r.Context(), tenant, r)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(b)
}

// forwardErr forwards r and returns only the error, for calls expected to fail.
func forwardErr(ctx context.Context, f *proxy.Forwarder, tenant config.Tenant, r *http.Request) error {
	resp, err := f.Forward(ctx, tenant, r)
	if resp != nil {
		_ = resp.Body.Close()
	}
	return err
}

func newForwarder() *proxy.Forwarder {
	return proxy.New(proxy.Options{Timeout: 5 * time.Second})
}

func TestForwardJoinsPathAndQuery(t *testing.T) {
	tests := []struct {
		base, target, want string
	}{
		{"", "/chat?a=b", "/chat?a=b"},
		{"/", "/chat?a=b", "/chat?a=b"},
		{"/v1", "/chat?a=b", "/v1/chat?a=b"},
		{"/v1/", "/chat?a=b", "/v1/chat?a=b"},
		{"/v1?api-version=1", "/chat?a=b", "/v1/chat?api-version=1&a=b"},
		{"/v1?api-version=1", "/chat", "/v1/chat?api-version=1"},
		{"/v1", "/chat", "/v1/chat"},
	}
	for _, tt := range tests {
		t.Run(tt.base+" "+tt.target, func(t *testing.T) {
			srv, got := recordingUpstream(t)
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.target, nil)

			forward(t, newForwarder(), tenantFor(t, srv.URL+tt.base), r)

			require.Len(t, *got, 1)
			assert.Equal(t, tt.want, (*got)[0].requestURI)
		})
	}
}

func TestForwardKeepsEncodedPath(t *testing.T) {
	srv, got := recordingUpstream(t)
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/a%2Fb", nil)

	forward(t, newForwarder(), tenantFor(t, srv.URL), r)

	require.Len(t, *got, 1)
	assert.Equal(t, "/v1/a%2Fb", (*got)[0].requestURI)
}

func TestForwardHeaders(t *testing.T) {
	srv, got := recordingUpstream(t)
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/x", strings.NewReader("{}"))
	r.Header.Set("Authorization", "Bearer provider-key")
	r.Header.Set("X-Api-Key", "provider-key")
	r.Header.Set("Anthropic-Version", "2023-06-01")
	r.Header.Set("Connection", "X-Custom")
	r.Header.Set("X-Custom", "hop")
	r.Header.Set("Keep-Alive", "timeout=5")
	r.Header.Set("Te", "trailers")
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Proxy-Authorization", "Basic secret")
	r.Header.Set("Accept-Encoding", "gzip")

	forward(t, newForwarder(), tenantFor(t, srv.URL), r)

	require.Len(t, *got, 1)
	h := (*got)[0].header
	assert.Equal(t, "Bearer provider-key", h.Get("Authorization"))
	assert.Equal(t, "provider-key", h.Get("X-Api-Key"))
	assert.Equal(t, "2023-06-01", h.Get("Anthropic-Version"))
	for _, name := range []string{"X-Custom", "Keep-Alive", "Te", "Upgrade", "Proxy-Authorization", "X-Forwarded-For"} {
		assert.Empty(t, h.Values(name), name)
	}
	assert.Equal(t, "identity", h.Get("Accept-Encoding"))
	assert.Equal(t, strings.TrimPrefix(srv.URL, "http://"), (*got)[0].host)
}

func TestForwardMethodAndBody(t *testing.T) {
	srv, got := recordingUpstream(t)
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/x", strings.NewReader(`{"a":1}`))

	forward(t, newForwarder(), tenantFor(t, srv.URL), r)

	require.Len(t, *got, 1)
	assert.Equal(t, http.MethodPost, (*got)[0].method)
	assert.JSONEq(t, `{"a":1}`, (*got)[0].body)
	assert.EqualValues(t, 7, (*got)[0].contentLength)
}

func TestForwardEmptyBody(t *testing.T) {
	srv, got := recordingUpstream(t)
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/x", nil)

	forward(t, newForwarder(), tenantFor(t, srv.URL), r)

	require.Len(t, *got, 1)
	assert.Zero(t, (*got)[0].contentLength)
	assert.Empty(t, (*got)[0].te)
}

func TestForwardDoesNotFollowRedirects(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/x", nil)

	status, _ := forward(t, newForwarder(), tenantFor(t, srv.URL), r)

	assert.Equal(t, http.StatusFound, status)
	assert.Equal(t, []string{"/v1/x"}, paths)
}

// slowUpstream sleeps before answering. Its sleep ends when the test does.
func slowUpstream(t *testing.T, headersFirst bool) *httptest.Server {
	t.Helper()
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if headersFirst {
			_, _ = io.WriteString(w, "partial")
			w.(http.Flusher).Flush()
		}
		select {
		case <-time.After(500 * time.Millisecond):
		case <-done:
		}
		_, _ = io.WriteString(w, "rest")
	}))
	t.Cleanup(func() { close(done); srv.Close() })
	return srv
}

func TestForwardTimeout(t *testing.T) {
	srv := slowUpstream(t, false)
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/x", nil)

	err := forwardErr(r.Context(), proxy.New(proxy.Options{Timeout: 50 * time.Millisecond}), tenantFor(t, srv.URL), r)

	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestForwardTimeoutCoversBody(t *testing.T) {
	srv := slowUpstream(t, true)
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/x", nil)

	resp, err := proxy.New(proxy.Options{Timeout: 50 * time.Millisecond}).Forward(r.Context(), tenantFor(t, srv.URL), r)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	_, err = io.ReadAll(resp.Body)

	assert.Error(t, err)
}

func TestForwardCallerCancel(t *testing.T) {
	srv, _ := recordingUpstream(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/x", nil)

	err := forwardErr(ctx, newForwarder(), tenantFor(t, srv.URL), r)

	assert.ErrorIs(t, err, context.Canceled)
}

func TestForwardErrorHidesURL(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/x?q=callersecret", nil)

	err := forwardErr(r.Context(), newForwarder(), tenantFor(t, "http://127.0.0.1:1/?key=s3cret"), r)

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "s3cret")
	assert.NotContains(t, err.Error(), "callersecret")
	var ue *url.Error
	assert.NotErrorAs(t, err, &ue)
}

func TestForwardReturnsUpstreamStatusAndBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = io.WriteString(w, "teapot")
	}))
	t.Cleanup(srv.Close)
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/x", nil)

	status, body := forward(t, newForwarder(), tenantFor(t, srv.URL), r)

	assert.Equal(t, http.StatusTeapot, status)
	assert.Equal(t, "teapot", body)
}
