// Package server wires up the HTTP server.
package server

import "github.com/gin-gonic/gin"

// New returns a Gin engine with middleware and all routes registered.
func New() *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	registerRoutes(r)
	return r
}
