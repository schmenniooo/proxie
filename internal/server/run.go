package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// readHeaderTimeout bounds how long a client may take to send request
// headers, which protects against slowloris style connections.
const readHeaderTimeout = 10 * time.Second

// Run serves handler on ln until ctx is cancelled, then shuts down
// gracefully: it stops accepting connections and waits up to shutdownTimeout
// for in-flight requests before closing the rest. It returns nil after a
// clean shutdown.
func Run(ctx context.Context, ln net.Listener, handler http.Handler, logger *slog.Logger, shutdownTimeout time.Duration) error {
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down", slog.String("timeout", shutdownTimeout.String()))
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return errors.Join(fmt.Errorf("shutdown: %w", err), srv.Close())
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
