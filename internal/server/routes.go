package server

import "github.com/gin-gonic/gin"

// registerRoutes maps endpoints to handlers.
func registerRoutes(r *gin.Engine) {
	r.GET("/hello", helloHandler)
	r.NoRoute(notFoundHandler)
}
