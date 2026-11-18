package regression

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/cache"
	"github.com/universal-api/platform/internal/quota"
)

// Requires a disposable, task-owned Redis container with AOF enabled.
func TestRealRedisRestartOutageAndReconnect(t *testing.T) {
	addr, container := os.Getenv("UWA_AUDIT_REDIS"), os.Getenv("UWA_AUDIT_REDIS_CONTAINER")
	if addr == "" || container == "" {
		t.Skip("disposable Redis address and owned container name are required")
	}
	if !regexp.MustCompile(`^uwa-audit-redis-[0-9]{8}$`).MatchString(container) {
		t.Fatal("refusing to control a container outside the disposable UWA test namespace")
	}
	command := func(action string) error {
		cmdCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		out, err := exec.CommandContext(cmdCtx, "docker", action, container).CombinedOutput()
		if err != nil {
			return fmt.Errorf("docker %s failed: %w (%s)", action, err, out)
		}
		return nil
	}
	containerAddr := func() string {
		t.Helper()
		portCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		output, err := exec.CommandContext(portCtx, "docker", "port", container, "6379/tcp").Output()
		if err != nil {
			t.Fatalf("Redis published port: %v", err)
		}
		host, port, err := net.SplitHostPort(strings.TrimSpace(string(output)))
		if err != nil || host != "127.0.0.1" {
			t.Fatalf("Redis did not publish a loopback port: %q (%v)", output, err)
		}
		return net.JoinHostPort(host, port)
	}
	addr = containerAddr()
	opts := cache.DefaultRedisOptions(addr)
	opts.FailOpen = false
	opts.Timeout = 250 * time.Millisecond
	store, err := cache.NewRedisStore(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	key := fmt.Sprintf("audit:restart:%d", time.Now().UnixNano())
	group := fmt.Sprintf("audit_restart_%d", time.Now().UnixNano())
	budget := map[string]quota.Budget{"audit_provider": {PerDay: 2, Group: group}}
	manager := quota.NewManager(budget, store)
	if err := store.Set(ctx, key, []byte("persistent"), 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		allowed, err := manager.AllowAttempt(ctx, "audit_provider")
		if err != nil || allowed != (i < 2) {
			t.Fatalf("pre-restart quota attempt %d: allowed=%v err=%v", i, allowed, err)
		}
	}
	if err := command("restart"); err != nil {
		t.Fatal(err)
	}
	opts.Addr = containerAddr()
	var recovered *cache.RedisStore
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		recovered, err = cache.NewRedisStore(opts)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if recovered == nil {
		t.Fatalf("Redis did not recover after restart: %v", err)
	}
	defer recovered.Close()
	if value, hit, err := recovered.Get(ctx, key); err != nil || !hit || string(value) != "persistent" {
		t.Fatalf("AOF cache after restart: value=%q hit=%v err=%v", value, hit, err)
	}
	if allowed, err := quota.NewManager(budget, recovered).AllowAttempt(ctx, "audit_provider"); err != nil || allowed {
		t.Fatalf("quota state after restart: allowed=%v err=%v", allowed, err)
	}
	if err := command("pause"); err != nil {
		t.Fatal(err)
	}
	paused := true
	defer func() {
		if paused {
			_ = command("unpause")
		}
	}()
	outageCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	type admission struct {
		allowed bool
		err     error
	}
	result := make(chan admission, 1)
	go func() {
		allowed, err := quota.NewManager(budget, recovered).AllowAttempt(outageCtx, "audit_provider")
		result <- admission{allowed: allowed, err: err}
	}()
	var outcome admission
	bounded := true
	select {
	case outcome = <-result:
	case <-time.After(3 * time.Second):
		bounded = false
	}
	cancel()
	if err := command("unpause"); err != nil {
		t.Fatal(err)
	}
	paused = false
	if !bounded {
		t.Fatal("Redis quota admission ignored outage deadline")
	}
	if outcome.err == nil || outcome.allowed {
		t.Fatalf("unavailable Redis did not fail quota closed: allowed=%v err=%v", outcome.allowed, outcome.err)
	}
	reconnectCtx, reconnectCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer reconnectCancel()
	if err := recovered.Ping(reconnectCtx); err != nil {
		t.Fatalf("Redis store did not reconnect: %v", err)
	}
	if value, hit, err := recovered.Get(reconnectCtx, key); err != nil || !hit || string(value) != "persistent" {
		t.Fatalf("cache after reconnect: value=%q hit=%v err=%v", value, hit, err)
	}
}
