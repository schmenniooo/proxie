package server_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEveryMethodIsProxied(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			var (
				mu   sync.Mutex
				seen string
			)
			upstream := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				seen = r.Method + " " + r.URL.Path
			})
			w := httptest.NewRecorder()

			newServerWithUpstream(t, slog.New(slog.DiscardHandler), upstream, defaultTimeout, defaultMaxRequestBytes).
				ServeHTTP(w, newRequest(t, method, "/v1/x"))

			assert.Equal(t, http.StatusOK, w.Code)
			mu.Lock()
			defer mu.Unlock()
			assert.Equal(t, method+" /v1/x", seen)
		})
	}
}
