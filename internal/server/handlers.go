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
