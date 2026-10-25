package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/schmenniooo/proxie/internal/server"
	"github.com/stretchr/testify/assert"
)

func TestUnknownRouteReturns404(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/nope", nil)

	server.New().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}
