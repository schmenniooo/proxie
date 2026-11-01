package server_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
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
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/hello", nil)

	server.New(logger).ServeHTTP(w, req)

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
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nope", nil)

	server.New(logger).ServeHTTP(w, req)

	assert.Contains(t, buf.String(), `"level":"WARN"`)
}

func TestAccessLogCountsNotFoundBytes(t *testing.T) {
	logger, buf := bufferLogger()
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/hello", nil)

	server.New(logger).ServeHTTP(w, req)

	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line))
	assert.InDelta(t, http.StatusNotFound, line["status"], 0)
	assert.InDelta(t, w.Body.Len(), line["bytes"], 0)
	assert.Positive(t, w.Body.Len())
}

func TestAccessLogOmitsQueryAndHeaders(t *testing.T) {
	logger, buf := bufferLogger()
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/hello?key=querysecret", nil)
	req.Header.Set("Authorization", "Bearer authsecret")
	req.Header.Set("X-Api-Key", "apisecret")
	req.Header.Set("X-Proxie-Key", "proxiesecret")

	server.New(logger).ServeHTTP(w, req)

	for _, secret := range []string{"querysecret", "authsecret", "apisecret", "proxiesecret"} {
		assert.NotContains(t, buf.String(), secret)
	}
}

func TestRecoveryReturns500AndLogsWithoutHeaders(t *testing.T) {
	logger, buf := bufferLogger()
	r := server.New(logger)
	r.GET("/panic", func(*gin.Context) { panic("boom") })
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/panic", nil)
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
	r := server.New(logger)
	r.GET("/abort", func(*gin.Context) { panic(http.ErrAbortHandler) })
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/abort", nil)

	assert.PanicsWithError(t, http.ErrAbortHandler.Error(), func() {
		r.ServeHTTP(httptest.NewRecorder(), req)
	})
	assert.NotContains(t, buf.String(), "panic recovered")
}
