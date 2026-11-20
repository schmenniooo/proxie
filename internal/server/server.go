// Package server wires up the HTTP server.
package server

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/schmenniooo/proxie/internal/auth"
)

// New returns a Gin engine with middleware and all routes registered. Every
// route, including unknown ones, requires a valid proxie key.
func New(logger *slog.Logger, a *auth.Authenticator) *gin.Engine {
	r := gin.New()
	r.Use(accessLog(logger), recovery(logger), requireProxieKey(a))
	registerRoutes(r)
	return r
}
