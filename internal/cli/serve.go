package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/gin-gonic/gin"
	"github.com/schmenniooo/proxie/internal/cache"
	"github.com/schmenniooo/proxie/internal/config"
	"github.com/schmenniooo/proxie/internal/logging"
	"github.com/schmenniooo/proxie/internal/server"
)

// ServeCmd runs the proxy until ctx is cancelled.
type ServeCmd struct{}

// Run starts the server with logging and graceful shutdown.
func (ServeCmd) Run(ctx context.Context, cfg *config.Config, s *streams) error {
	if err := cfg.Complete(s.environ); err != nil {
		return fmt.Errorf("invalid configuration:\n%w", err)
	}

	logger, logFile, err := logging.New(cfg.Log, s.stdout)
	if err != nil {
		return err
	}
	// Closed last, so the shutdown log lines still reach the file.
	defer func() { _ = logFile.Close() }()
	slog.SetDefault(logger)
	gin.SetMode(gin.ReleaseMode)

	logger.Info("starting proxie", slog.String("version", version), slog.Any("config", *cfg))

	c, err := cache.NewRedis(cache.RedisOptions{
		URL:      cfg.RedisURL.URL,
		Password: cfg.RedisPassword,
		Timeout:  cfg.RedisTimeout,
	})
	if err != nil {
		logger.Error("redis setup failed", slog.Any("error", err))
		return exitCode(1)
	}
	defer func() { _ = c.Close() }()
	// Fail open: an unreachable Redis only means requests go uncached.
	if err := c.Ping(ctx); err != nil {
		logger.Warn("redis unreachable, requests will not be cached until it is back", slog.Any("error", err))
	}

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.Addr)
	if err != nil {
		logger.Error("listen failed", slog.String("addr", cfg.Addr), slog.Any("error", err))
		return exitCode(1)
	}
	logger.Info("listening", slog.String("addr", ln.Addr().String()))

	if err := server.Run(ctx, ln, server.New(logger), logger, cfg.ShutdownTimeout); err != nil {
		logger.Error("server stopped with error", slog.Any("error", err))
		return exitCode(1)
	}
	logger.Info("proxie stopped")
	return nil
}
