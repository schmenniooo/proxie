package doctor_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/schmenniooo/proxie/internal/config"
	"github.com/schmenniooo/proxie/internal/doctor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

// closedAddr returns a local address nothing listens on.
func closedAddr(t *testing.T) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	return addr
}

// newConfig returns a valid config with one tenant per upstream URL and a
// Redis at redisAddr.
func newConfig(t *testing.T, redisAddr string, upstreams ...string) config.Config {
	t.Helper()
	tenants := map[string]config.Tenant{}
	for i, raw := range upstreams {
		name := string(rune('a' + i))
		tenants[name] = config.Tenant{Name: name, Keys: []string{"key-secret-" + name}, UpstreamURL: mustURL(t, raw)}
	}
	return config.Config{
		Tenants:      tenants,
		RedisURL:     config.URL{URL: mustURL(t, "redis://"+redisAddr)},
		RedisTimeout: 200 * time.Millisecond,
		Log:          config.Log{Format: "json", FileMaxMB: 1},
	}
}

func byCheck(results []doctor.Result) map[string]doctor.Result {
	m := map[string]doctor.Result{}
	for _, r := range results {
		m[r.Check] = r
	}
	return m
}

func TestRunConfigError(t *testing.T) {
	t.Parallel()

	results := doctor.Run(t.Context(), config.Config{}, errors.New("a: required\nb: required"))

	assert.Equal(t, []doctor.Result{{Check: "config", Status: doctor.Fail, Detail: "a: required\nb: required"}}, results)
	assert.True(t, doctor.Failed(results))
}

func TestRunAllPassOrWarn(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	upstream := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(upstream.Close)
	cfg := newConfig(t, mr.Addr(), upstream.URL, upstream.URL)
	cfg.Log.File = filepath.Join(t.TempDir(), "proxie.log")

	results := doctor.Run(t.Context(), cfg, nil)
	got := byCheck(results)

	assert.False(t, doctor.Failed(results))
	assert.Equal(t, doctor.Pass, got["config"].Status)
	assert.Equal(t, "2 tenant(s)", got["config"].Detail)
	assert.Equal(t, doctor.Pass, got["log file"].Status)
	assert.Equal(t, doctor.Pass, got["redis"].Status)
	// miniredis has no CONFIG command.
	assert.Equal(t, doctor.Warn, got["redis maxmemory-policy"].Status)
	// Both tenants share the upstream, so it is checked once.
	assert.Len(t, results, 5)
	assert.Equal(t, doctor.Pass, got["upstream "+upstream.URL].Status)
	assert.Contains(t, got["upstream "+upstream.URL].Detail, "HTTP 404")
}

func TestRunUpstreamAuthRequiredIsExpected(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	status := func(code int) *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }))
		t.Cleanup(srv.Close)
		return srv
	}
	unauthorized, forbidden := status(http.StatusUnauthorized), status(http.StatusForbidden)
	cfg := newConfig(t, mr.Addr(), unauthorized.URL, forbidden.URL)

	got := byCheck(doctor.Run(t.Context(), cfg, nil))

	for code, srv := range map[int]*httptest.Server{401: unauthorized, 403: forbidden} {
		r := got["upstream "+srv.URL]
		assert.Equal(t, doctor.Pass, r.Status, code)
		assert.Contains(t, r.Detail, fmt.Sprintf("reachable, auth required: HTTP %d in ", code))
	}
}

func TestRunFailures(t *testing.T) {
	t.Parallel()

	tlsUpstream := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(tlsUpstream.Close)
	down := "http://" + closedAddr(t)
	cfg := newConfig(t, closedAddr(t), tlsUpstream.URL, down, "http://proxie-doctor-test.invalid")
	cfg.Log.File = filepath.Join(t.TempDir(), "missing", "proxie.log")

	results := doctor.Run(t.Context(), cfg, nil)
	got := byCheck(results)

	assert.True(t, doctor.Failed(results))
	assert.Equal(t, doctor.Fail, got["log file"].Status)
	assert.Equal(t, doctor.Fail, got["redis"].Status)
	assert.NotContains(t, got, "redis maxmemory-policy", "skipped when Redis is down")
	assert.Equal(t, doctor.Fail, got["upstream "+tlsUpstream.URL].Status)
	assert.Contains(t, got["upstream "+tlsUpstream.URL].Detail, "TLS:", "self-signed certificate")
	assert.Equal(t, doctor.Fail, got["upstream "+down].Status)
	assert.Contains(t, got["upstream "+down].Detail, "HTTP:")
	assert.Equal(t, doctor.Fail, got["upstream http://proxie-doctor-test.invalid"].Status)
	assert.Contains(t, got["upstream http://proxie-doctor-test.invalid"].Detail, "DNS:")
}

func TestRunHidesSecrets(t *testing.T) {
	t.Parallel()

	addr := closedAddr(t)
	cfg := newConfig(t, closedAddr(t), "http://user:upstreamsecret@"+addr+"/v1?key=querysecret")
	cfg.RedisURL = config.URL{URL: mustURL(t, "redis://:redissecret@"+closedAddr(t))}
	cfg.RedisPassword = "envsecret"

	var buf bytes.Buffer
	require.NoError(t, doctor.WriteJSON(&buf, doctor.Run(t.Context(), cfg, nil)))

	for _, secret := range []string{"upstreamsecret", "querysecret", "redissecret", "envsecret", "key-secret"} {
		assert.NotContains(t, buf.String(), secret)
	}
	assert.Contains(t, buf.String(), addr+"/v1")
}

func TestWriteTable(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	require.NoError(t, doctor.WriteTable(&buf, []doctor.Result{
		{Check: "config", Status: doctor.Fail, Detail: "a: required\nb: required"},
		{Check: "redis", Status: doctor.Pass, Detail: "PING in 1ms"},
	}))

	assert.Equal(t, ""+
		"STATUS  CHECK   DETAIL\n"+
		"fail    config  a: required\n"+
		"                b: required\n"+
		"pass    redis   PING in 1ms\n", buf.String())
}

func TestWriteJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	require.NoError(t, doctor.WriteJSON(&buf, []doctor.Result{{Check: "config", Status: doctor.Pass}}))

	var got []map[string]string
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Equal(t, []map[string]string{{"check": "config", "status": "pass"}}, got)
}
