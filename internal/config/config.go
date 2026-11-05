package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

type Config struct {
	Port            string
	DatabaseURL     string
	LogLevel        slog.Level
	ShutdownTimeout time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		Port:            getenv("PORT", "8080"),
		DatabaseURL:     strings.TrimSpace(os.Getenv("DATABASE_URL")),
		LogLevel:        slog.LevelInfo,
		ShutdownTimeout: 10 * time.Second,
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if raw := strings.TrimSpace(os.Getenv("LOG_LEVEL")); raw != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(raw)); err != nil {
			return Config{}, fmt.Errorf("invalid LOG_LEVEL: %w", err)
		}
	}
	if raw := strings.TrimSpace(os.Getenv("SHUTDOWN_TIMEOUT")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid SHUTDOWN_TIMEOUT: %w", err)
		}
		if d <= 0 {
			return Config{}, errors.New("SHUTDOWN_TIMEOUT must be positive")
		}
		cfg.ShutdownTimeout = d
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
