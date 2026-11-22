package server

import "github.com/gin-gonic/gin"

// upstreamPrefix is where the proxy lives. Everything after it is forwarded
// to the tenant's upstream, so a caller sets its SDK base URL to
// http://<proxie>/upstream/v1. The version leaves room for a v2 and keeps
// other paths free for proxie's own routes.
const upstreamPrefix = "/upstream/v1"

// registerRoutes maps endpoints to handlers.
func registerRoutes(r *gin.Engine, proxy gin.HandlerFunc) {
	r.Any(upstreamPrefix+"/*path", proxy)
	r.NoRoute(notFoundHandler)
}
