// Package server wires up the HTTP server.
package server

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/schmenniooo/proxie/internal/auth"
	"github.com/schmenniooo/proxie/internal/proxy"
)

// New returns a Gin engine with middleware and all routes registered. Every
// route requires a valid proxie key; every path is forwarded to the tenant's
// upstream by fwd, with request bodies above maxRequestBytes streamed through
// unbuffered.
func New(logger *slog.Logger, a *auth.Authenticator, fwd *proxy.Forwarder, maxRequestBytes int64) *gin.Engine {
	r := gin.New()
	r.Use(accessLog(logger), recovery(logger), requireProxieKey(a))
	registerRoutes(r, proxyHandler(logger, fwd, maxRequestBytes))
	return r
}
