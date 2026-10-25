package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const greeting = "Hello, World!"

// registerRoutes maps endpoints to handlers. Business logic starts here.
func registerRoutes(r *gin.Engine) {
	r.GET("/hello", helloHandler)
}

func helloHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": greeting})
}
