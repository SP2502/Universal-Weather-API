// Package platformsvc runs the gateway as a console application, systemd
// service, or Windows Service Control Manager service without new dependencies.
package platformsvc

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// Runner must invoke ready only after successfully binding its listener.
// On cancellation it must shut down its HTTP server and flush resources.
type Runner func(ctx context.Context, ready func()) error

func RunConsole(r Runner) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return r(ctx, func() { notifyReady(ctx) })
}
