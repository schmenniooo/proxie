// Command proxie is the proxie CLI. Without a command it runs the proxy.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/schmenniooo/proxie/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// After the first signal, restore default handling so a second one kills
	// the process instead of waiting for the graceful shutdown.
	go func() {
		<-ctx.Done()
		stop()
	}()

	os.Exit(cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
