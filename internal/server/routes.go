package server

import "github.com/gin-gonic/gin"

// registerRoutes maps endpoints to handlers. There are no fixed routes: every
// method and path goes to the proxy.
func registerRoutes(r *gin.Engine, proxy gin.HandlerFunc) {
	r.NoRoute(proxy)
}
