package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/schmenniooo/proxie/internal/proxy"
)

// Handlers are the starting point of business logic.

// proxyHandler forwards the request to the tenant's upstream and copies the
// response back. Upstream failures become 502, upstream timeouts 504. Errors
// are logged without the URL, headers or body.
func proxyHandler(logger *slog.Logger, fwd *proxy.Forwarder, maxRequestBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		tenant, ok := TenantFrom(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "no tenant"})
			return
		}
		// The result feeds the cache key once caching exists; for now only
		// the side effect of a re-readable body matters.
		if _, err := proxy.ReadBody(c.Request, maxRequestBytes); err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "could not read request body"})
			return
		}

		resp, err := fwd.Forward(ctx, tenant, c.Request)
		if err != nil {
			switch {
			case errors.Is(err, proxy.ErrInvalidPath):
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid request path"})
			case ctx.Err() != nil:
				// The caller is gone; there is no one to answer.
				c.Abort()
			case errors.Is(err, context.DeadlineExceeded):
				logUpstreamError(c, logger, tenant.Name, err)
				c.AbortWithStatusJSON(http.StatusGatewayTimeout, gin.H{"error": "upstream timeout"})
			default:
				logUpstreamError(c, logger, tenant.Name, err)
				c.AbortWithStatusJSON(http.StatusBadGateway, gin.H{"error": "upstream unavailable"})
			}
			return
		}
		// WriteResponse closes the body too; closing twice is harmless and
		// keeps the release visible here.
		defer func() { _ = resp.Body.Close() }()

		if err := proxy.WriteResponse(c.Writer, resp); err != nil {
			logUpstreamError(c, logger, tenant.Name, err)
			// The status is already sent: cut the connection so the caller
			// sees a broken response instead of a truncated one that looks
			// complete.
			panic(http.ErrAbortHandler)
		}
	}
}

func logUpstreamError(c *gin.Context, logger *slog.Logger, tenant string, err error) {
	logger.WarnContext(c.Request.Context(), "upstream request failed",
		slog.String("tenant", tenant),
		slog.String("path", c.Request.URL.Path),
		slog.Any("error", err),
	)
}
