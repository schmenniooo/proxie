package server

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/schmenniooo/proxie/internal/auth"
	"github.com/schmenniooo/proxie/internal/config"
)

// tenantKey is the Gin context key of the authenticated tenant.
const tenantKey = "proxie.tenant"

// accessLog logs one line per request. It logs the path only: query strings
// can carry provider keys (for example ?key=), and headers and bodies carry
// keys and prompts, so none of them are logged.
func accessLog(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		level := slog.LevelInfo
		switch {
		case status >= http.StatusInternalServerError:
			level = slog.LevelError
		case status >= http.StatusBadRequest:
			level = slog.LevelWarn
		}
		attrs := []slog.Attr{
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", status),
			slog.Float64("latency_ms", float64(time.Since(start).Microseconds())/1000),
			slog.Int("bytes", max(c.Writer.Size(), 0)),
			slog.String("remote_ip", c.RemoteIP()),
		}
		if t, ok := TenantFrom(c); ok {
			attrs = append(attrs, slog.String("tenant", t.Name))
		}
		logger.LogAttrs(c.Request.Context(), level, "request", attrs...)
	}
}

// recovery turns a panic into a 500 and logs it with its stack. Unlike
// gin.Recovery it never dumps request headers.
func recovery(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			// net/http uses this panic to abort a response on purpose.
			if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(rec)
			}
			logger.ErrorContext(c.Request.Context(), "panic recovered",
				slog.String("error", fmt.Sprint(rec)),
				slog.String("method", c.Request.Method),
				slog.String("path", c.Request.URL.Path),
				slog.String("stack", string(debug.Stack())),
			)
			if c.Writer.Written() {
				c.Abort()
				return
			}
			c.AbortWithStatus(http.StatusInternalServerError)
		}()
		c.Next()
	}
}

// requireProxieKey resolves the tenant from the X-Proxie-Key header and
// answers 401 if the key is missing, unknown or sent more than once. The
// header is removed in every case, so it is never forwarded upstream.
// Provider keys (Authorization, x-api-key) are left untouched.
func requireProxieKey(a *auth.Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		keys := c.Request.Header.Values(auth.Header)
		c.Request.Header.Del(auth.Header)

		if len(keys) != 1 {
			unauthorized(c)
			return
		}
		t, ok := a.Lookup(keys[0])
		if !ok {
			unauthorized(c)
			return
		}
		c.Set(tenantKey, t)
		c.Next()
	}
}

func unauthorized(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing or invalid proxie key"})
}

// TenantFrom returns the tenant that requireProxieKey resolved for the
// request.
func TenantFrom(c *gin.Context) (config.Tenant, bool) {
	v, ok := c.Get(tenantKey)
	if !ok {
		return config.Tenant{}, false
	}
	t, ok := v.(config.Tenant)
	return t, ok
}
