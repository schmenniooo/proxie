package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const greeting = "Hello, World!"

// Handlers are the starting point of business logic.

func helloHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": greeting})
}

// notFoundHandler answers unknown routes. Gin's built-in 404 is written after
// the middleware chain, so the access log would miss its bytes.
func notFoundHandler(c *gin.Context) {
	c.String(http.StatusNotFound, "404 page not found")
}
