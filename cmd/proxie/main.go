// Command proxie starts the proxie HTTP server.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"
	"github.com/schmenniooo/proxie/internal/config"
	"github.com/schmenniooo/proxie/internal/logging"
	"github.com/schmenniooo/proxie/internal/server"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// After the first signal, restore default handling so a second one kills
	// the process instead of waiting for the graceful shutdown.
	go func() {
		<-ctx.Done()
		stop()
	}()

	os.Exit(run(ctx))
}

func run(ctx context.Context) int {
	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "proxie: invalid configuration:\n%v\n", err)
		return 1
	}

	logger, logFile, err := logging.New(cfg.Log, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "proxie: %v\n", err)
		return 1
	}
	// Closed last, so the shutdown log lines still reach the file.
	defer func() { _ = logFile.Close() }()
	slog.SetDefault(logger)
	gin.SetMode(gin.ReleaseMode)

	logger.Info("starting proxie", slog.Any("config", cfg))
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.Addr)
	if err != nil {
		logger.Error("listen failed", slog.String("addr", cfg.Addr), slog.Any("error", err))
		return 1
	}
	logger.Info("listening", slog.String("addr", ln.Addr().String()))

	if err := server.Run(ctx, ln, server.New(logger), logger, cfg.ShutdownTimeout); err != nil {
		logger.Error("server stopped with error", slog.Any("error", err))
		return 1
	}
	logger.Info("proxie stopped")
	return 0
}
