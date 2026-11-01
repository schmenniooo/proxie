// Package server wires up the HTTP server.
package server

import (
	"log/slog"

	"github.com/gin-gonic/gin"
)

// New returns a Gin engine with middleware and all routes registered.
func New(logger *slog.Logger) *gin.Engine {
	r := gin.New()
	r.Use(accessLog(logger), recovery(logger))
	registerRoutes(r)
	return r
}
