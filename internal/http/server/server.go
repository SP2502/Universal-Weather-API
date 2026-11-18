package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/universal-api/platform/internal/config"
	"github.com/universal-api/platform/internal/health"
	"github.com/universal-api/platform/internal/http/middleware"
)

type Server struct {
	httpServer *http.Server
	cfg        *config.Config
	logger     *slog.Logger
}

func New(cfg *config.Config, logger *slog.Logger, healthHandler *health.Handler) *Server {
	mux := http.NewServeMux()

	// Probes
	mux.HandleFunc("GET /health", healthHandler.Health)
	mux.HandleFunc("GET /readiness", healthHandler.Readiness)
	mux.HandleFunc("GET /liveness", healthHandler.Liveness)

	// Middleware chain: Recovery -> RequestID -> Logger
	var handler http.Handler = mux
	handler = middleware.Logger(logger)(handler)
	handler = middleware.RequestID(handler)
	handler = middleware.Recovery(logger)(handler)

	httpSrv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      handler,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	return &Server{
		httpServer: httpSrv,
		cfg:        cfg,
		logger:     logger,
	}
}

func (s *Server) Start() error {
	s.logger.Info("Starting HTTP Server",
		"port", s.cfg.Port,
		"env", s.cfg.Environment,
		"version", s.cfg.Version,
	)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("http server failed: %w", err)
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down HTTP server cleanly...")
	return s.httpServer.Shutdown(ctx)
}
