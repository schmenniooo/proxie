package server_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/schmenniooo/proxie/internal/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	return ln
}

// slowHandler signals when a request arrived and answers once release closes.
func slowHandler(arrived chan<- struct{}, release <-chan struct{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		arrived <- struct{}{}
		<-release
		_, _ = io.WriteString(w, "done")
	})
}

func get(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	return string(body), err
}

func TestRunFinishesInFlightRequestsOnShutdown(t *testing.T) {
	ln := listen(t)
	arrived, release := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	runErr := make(chan error, 1)
	go func() {
		runErr <- server.Run(ctx, ln, slowHandler(arrived, release), slog.New(slog.DiscardHandler), 5*time.Second)
	}()

	body := make(chan string, 1)
	go func() {
		b, err := get(t.Context(), "http://"+ln.Addr().String())
		assert.NoError(t, err)
		body <- b
	}()
	<-arrived
	cancel()
	close(release)

	assert.Equal(t, "done", <-body)
	require.NoError(t, <-runErr)
}

func TestRunStopsAcceptingAfterShutdown(t *testing.T) {
	ln := listen(t)
	ctx, cancel := context.WithCancel(t.Context())
	runErr := make(chan error, 1)
	go func() {
		runErr <- server.Run(ctx, ln, http.NotFoundHandler(), slog.New(slog.DiscardHandler), time.Second)
	}()

	cancel()
	require.NoError(t, <-runErr)

	_, err := get(t.Context(), "http://"+ln.Addr().String())
	assert.Error(t, err)
}

func TestRunReturnsErrorWhenShutdownTimesOut(t *testing.T) {
	ln := listen(t)
	arrived, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(release) })
	ctx, cancel := context.WithCancel(t.Context())
	runErr := make(chan error, 1)
	go func() {
		runErr <- server.Run(ctx, ln, slowHandler(arrived, release), slog.New(slog.DiscardHandler), 50*time.Millisecond)
	}()

	go func() { _, _ = get(t.Context(), "http://"+ln.Addr().String()) }()
	<-arrived
	cancel()

	err := <-runErr
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRunReturnsErrorWhenListenerFails(t *testing.T) {
	ln := listen(t)
	require.NoError(t, ln.Close())

	err := server.Run(t.Context(), ln, http.NotFoundHandler(), slog.New(slog.DiscardHandler), time.Second)

	assert.ErrorIs(t, err, net.ErrClosed)
}
