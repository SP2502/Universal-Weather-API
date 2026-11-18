//go:build !linux && !windows

package platformsvc

import (
	"context"
	"errors"
)

func RunService(_ string, _ Runner) error {
	return errors.New("native service mode is unsupported on this operating system")
}
func notifyReady(_ context.Context) {}
