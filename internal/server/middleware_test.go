package server_test

import (
	"bytes"
	"encoding/json"
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

func bufferLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, nil)), &buf
}

func TestAccessLogFields(t *testing.T) {
	logger, buf := bufferLogger()
	w := httptest.NewRecorder()
	req := newRequest(t, http.MethodGet, "/hello")

	newServer(t, logger).ServeHTTP(w, req)

	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line))
	assert.Equal(t, "INFO", line["level"])
	assert.Equal(t, "request", line["msg"])
	assert.Equal(t, "GET", line["method"])
	assert.Equal(t, "/hello", line["path"])
	assert.InDelta(t, http.StatusOK, line["status"], 0)
	assert.Contains(t, line, "latency_ms")
	assert.InDelta(t, w.Body.Len(), line["bytes"], 0)
}

func TestAccessLogLevelFollowsStatus(t *testing.T) {
	logger, buf := bufferLogger()
	w := httptest.NewRecorder()
	req := newRequest(t, http.MethodGet, "/nope")

	newServer(t, logger).ServeHTTP(w, req)

	assert.Contains(t, buf.String(), `"level":"WARN"`)
}

func TestAccessLogCountsNotFoundBytes(t *testing.T) {
	logger, buf := bufferLogger()
	w := httptest.NewRecorder()
	req := newRequest(t, http.MethodPost, "/hello")

	newServer(t, logger).ServeHTTP(w, req)

	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line))
	assert.InDelta(t, http.StatusNotFound, line["status"], 0)
	assert.InDelta(t, w.Body.Len(), line["bytes"], 0)
	assert.Positive(t, w.Body.Len())
}

func TestAccessLogOmitsQueryAndHeaders(t *testing.T) {
	logger, buf := bufferLogger()
	w := httptest.NewRecorder()
	req := newRequest(t, http.MethodGet, "/hello?key=querysecret")
	req.Header.Set("Authorization", "Bearer authsecret")
	req.Header.Set("X-Api-Key", "apisecret")
	req.Header.Set("X-Proxie-Key", "proxiesecret")

	newServer(t, logger).ServeHTTP(w, req)

	for _, secret := range []string{"querysecret", "authsecret", "apisecret", "proxiesecret"} {
		assert.NotContains(t, buf.String(), secret)
	}
}

func TestRecoveryReturns500AndLogsWithoutHeaders(t *testing.T) {
	logger, buf := bufferLogger()
	r := newServer(t, logger)
	r.GET("/panic", func(*gin.Context) { panic("boom") })
	w := httptest.NewRecorder()
	req := newRequest(t, http.MethodGet, "/panic")
	req.Header.Set("X-Api-Key", "apisecret")

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, buf.String(), `"msg":"panic recovered"`)
	assert.Contains(t, buf.String(), `"error":"boom"`)
	assert.Contains(t, buf.String(), `"status":500`)
	assert.NotContains(t, buf.String(), "apisecret")
}

func TestRecoveryRepanicsAbortHandler(t *testing.T) {
	logger, buf := bufferLogger()
	r := newServer(t, logger)
	r.GET("/abort", func(*gin.Context) { panic(http.ErrAbortHandler) })
	req := newRequest(t, http.MethodGet, "/abort")

	assert.PanicsWithError(t, http.ErrAbortHandler.Error(), func() {
		r.ServeHTTP(httptest.NewRecorder(), req)
	})
	assert.NotContains(t, buf.String(), "panic recovered")
}

func TestRequireProxieKeyRejects(t *testing.T) {
	for name, keys := range map[string][]string{
		"missing":   nil,
		"empty":     {""},
		"unknown":   {"wrong-key"},
		"duplicate": {testKey, testKey},
	} {
		t.Run(name, func(t *testing.T) {
			logger, buf := bufferLogger()
			w := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/hello", nil)
			for _, k := range keys {
				req.Header.Add(auth.Header, k)
			}

			newServer(t, logger).ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.JSONEq(t, `{"error":"missing or invalid proxie key"}`, w.Body.String())
			assert.NotContains(t, buf.String(), "wrong-key")
			assert.NotContains(t, buf.String(), testKey)
			assert.NotContains(t, buf.String(), `"tenant"`)
		})
	}
}

func TestRequireProxieKeyResolvesTenantAndStripsHeader(t *testing.T) {
	logger, buf := bufferLogger()
	r := newServer(t, logger)
	var (
		tenant     config.Tenant
		found      bool
		seenHeader http.Header
	)
	r.GET("/echo", func(c *gin.Context) {
		tenant, found = server.TenantFrom(c)
		seenHeader = c.Request.Header.Clone()
	})
	w := httptest.NewRecorder()
	req := newRequest(t, http.MethodGet, "/echo")
	req.Header.Set("Authorization", "Bearer provider-key")
	req.Header.Set("X-Api-Key", "provider-key")

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.True(t, found)
	assert.Equal(t, "acme", tenant.Name)
	assert.Empty(t, seenHeader.Values(auth.Header))
	assert.Equal(t, "Bearer provider-key", seenHeader.Get("Authorization"))
	assert.Equal(t, "provider-key", seenHeader.Get("X-Api-Key"))
	assert.Contains(t, buf.String(), `"tenant":"acme"`)
	assert.NotContains(t, buf.String(), testKey)
}
