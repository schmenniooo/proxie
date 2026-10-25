// Package server wires up the HTTP server.
package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const greeting = "Hello, World!"

// New returns a Gin engine with all routes registered.
func New() *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.GET("/hello", helloHandler)
	return r
}

func helloHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": greeting})
}
