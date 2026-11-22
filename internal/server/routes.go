package server

import "github.com/gin-gonic/gin"

// upstreamPrefix is where the proxy lives. Everything after it is forwarded
// to the tenant's upstream, so a caller sets its SDK base URL to
// http://<proxie>/v1/upstream. The version comes first, so later routes of
// proxie's own API sit next to it under /v1 (and a /v2 can follow).
const upstreamPrefix = "/v1/upstream"

// registerRoutes maps endpoints to handlers.
func registerRoutes(r *gin.Engine, proxy gin.HandlerFunc) {
	r.Any(upstreamPrefix+"/*path", proxy)
	r.NoRoute(notFoundHandler)
}
