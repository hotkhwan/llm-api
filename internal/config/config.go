package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHTTPAddr        = ":8080"
	defaultEnvironment     = "development"
	defaultShutdownTimeout = 10 * time.Second
)

type Config struct {
	Environment     string
	HTTPAddr        string
	ShutdownTimeout time.Duration
}

type LookupEnv func(string) string

// Load reads only non-secret runtime settings. Credentials must be injected by
// future integrations through a documented secret provider, never this config.
func Load(lookup LookupEnv) (Config, error) {
	cfg := Config{
		Environment:     valueOrDefault(lookup("APP_ENV"), defaultEnvironment),
		HTTPAddr:        valueOrDefault(lookup("HTTP_ADDR"), defaultHTTPAddr),
		ShutdownTimeout: defaultShutdownTimeout,
	}

	if !isEnvironment(cfg.Environment) {
		return Config{}, fmt.Errorf("APP_ENV must be one of development, test, staging, production")
	}
	if err := validateAddress(cfg.HTTPAddr); err != nil {
		return Config{}, fmt.Errorf("HTTP_ADDR: %w", err)
	}
	if raw := strings.TrimSpace(lookup("SHUTDOWN_TIMEOUT")); raw != "" {
		timeout, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("SHUTDOWN_TIMEOUT: %w", err)
		}
		if timeout < time.Second || timeout > 2*time.Minute {
			return Config{}, fmt.Errorf("SHUTDOWN_TIMEOUT must be between 1s and 2m")
		}
		cfg.ShutdownTimeout = timeout
	}

	return cfg, nil
}

func valueOrDefault(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}

func isEnvironment(value string) bool {
	switch value {
	case "development", "test", "staging", "production":
		return true
	default:
		return false
	}
}

func validateAddress(value string) error {
	_, port, err := net.SplitHostPort(value)
	if err != nil {
		return fmt.Errorf("must be a host:port pair: %w", err)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	return nil
}
