package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	openapi "devices-api/api"
	"devices-api/internal/config"
	"devices-api/internal/device"
	"devices-api/internal/notify"
	"devices-api/internal/platform"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration error:", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg, logger); err != nil {
		logger.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	pool, err := platform.OpenPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := platform.Migrate(ctx, cfg.DatabaseURL); err != nil {
		return err
	}

	broker := notify.NewBroker()
	repository := device.NewPostgresRepository(pool)
	service := device.NewService(repository, broker)
	handler := device.NewHandler(service, broker, logger)

	mux := http.NewServeMux()
	handler.Register(mux)
	mux.HandleFunc("GET /healthz", platform.Healthz)
	mux.HandleFunc("GET /readyz", platform.Readyz(pool))
	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(openapi.OpenAPISpec)
	})

	httpHandler := platform.RequestID(platform.RequestLogger(logger, platform.Recoverer(logger, mux)))

	baseCtx, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           httpHandler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		// SSE connections are long-lived, so the write timeout is disabled.
		WriteTimeout: 0,
		IdleTimeout:  60 * time.Second,
		BaseContext:  func(net.Listener) context.Context { return baseCtx },
		ErrorLog:     slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("server started", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		logger.Info("shutdown requested")
	}

	// Cancel long-lived SSE streams before waiting for Shutdown.
	cancelBase()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	logger.Info("server stopped")
	return nil
}
