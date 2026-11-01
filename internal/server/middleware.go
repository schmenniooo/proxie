package server

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
)

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
		logger.LogAttrs(c.Request.Context(), level, "request",
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", status),
			slog.Float64("latency_ms", float64(time.Since(start).Microseconds())/1000),
			slog.Int("bytes", max(c.Writer.Size(), 0)),
			slog.String("remote_ip", c.RemoteIP()),
		)
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
