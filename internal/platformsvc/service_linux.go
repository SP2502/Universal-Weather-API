//go:build linux

package platformsvc

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

func RunService(_ string, _ Runner) error {
	return errors.New("--service is for the Windows SCM; use the systemd unit on Linux")
}

func notifyReady(ctx context.Context) {
	notify("READY=1\nSTATUS=Gateway listening")
	microseconds, err := strconv.ParseInt(os.Getenv("WATCHDOG_USEC"), 10, 64)
	if err != nil || microseconds <= 0 {
		return
	}
	if pid := os.Getenv("WATCHDOG_PID"); pid != "" && pid != strconv.Itoa(os.Getpid()) {
		return
	}
	interval := time.Duration(microseconds) * time.Microsecond / 2
	if interval < time.Millisecond {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				notify("STOPPING=1")
				return
			case <-ticker.C:
				notify("WATCHDOG=1")
			}
		}
	}()
}

func notify(message string) {
	socket := os.Getenv("NOTIFY_SOCKET")
	if socket == "" {
		return
	}
	if strings.HasPrefix(socket, "@") {
		socket = "\x00" + socket[1:]
	}
	dst := &net.UnixAddr{Name: socket, Net: "unixgram"}
	conn, err := net.DialUnix("unixgram", nil, dst)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(250 * time.Millisecond))
	_, _ = conn.Write([]byte(message))
}
