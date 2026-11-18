package server

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/config"
	"github.com/universal-api/platform/internal/health"
	"github.com/universal-api/platform/internal/observability/logging"
)

func TestServerLifecycle(t *testing.T) {
	cfg := &config.Config{
		Port:            9999,
		ReadTimeout:     time.Second,
		WriteTimeout:    time.Second,
		IdleTimeout:     time.Second,
		ShutdownTimeout: 2 * time.Second,
		LogLevel:        "INFO",
		Version:         "test",
	}

	logger := logging.NewLogger("INFO", io.Discard)
	healthH := health.NewHandler("test")
	srv := New(cfg, logger, healthH)

	go func() {
		_ = srv.Start()
	}()

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://localhost:9999/health")
	if err != nil {
		t.Fatalf("failed to query server: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}
}
