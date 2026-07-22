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
	defaultReadTimeout     = 5 * time.Second
	defaultWriteTimeout    = 15 * time.Second
	defaultIdleTimeout     = 60 * time.Second
	defaultBodyLimit       = 1 << 20
	defaultConcurrency     = 1024
)

type Config struct {
	Environment     string
	HTTPAddr        string
	ShutdownTimeout time.Duration
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	BodyLimit       int
	Concurrency     int
}

type LookupEnv func(string) string

// Load reads only non-secret runtime settings. Credentials must be injected by
// future integrations through a documented secret provider, never this config.
func Load(lookup LookupEnv) (Config, error) {
	cfg := Config{
		Environment:     valueOrDefault(lookup("APP_ENV"), defaultEnvironment),
		HTTPAddr:        valueOrDefault(lookup("HTTP_ADDR"), defaultHTTPAddr),
		ShutdownTimeout: defaultShutdownTimeout,
		ReadTimeout:     defaultReadTimeout,
		WriteTimeout:    defaultWriteTimeout,
		IdleTimeout:     defaultIdleTimeout,
		BodyLimit:       defaultBodyLimit,
		Concurrency:     defaultConcurrency,
	}

	if !isEnvironment(cfg.Environment) {
		return Config{}, fmt.Errorf("APP_ENV must be one of development, test, staging, production")
	}
	if err := validateAddress(cfg.HTTPAddr); err != nil {
		return Config{}, fmt.Errorf("HTTP_ADDR: %w", err)
	}

	var err error
	if cfg.ShutdownTimeout, err = durationSetting(lookup, "SHUTDOWN_TIMEOUT", cfg.ShutdownTimeout, time.Second, 2*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.ReadTimeout, err = durationSetting(lookup, "HTTP_READ_TIMEOUT", cfg.ReadTimeout, time.Second, 30*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.WriteTimeout, err = durationSetting(lookup, "HTTP_WRITE_TIMEOUT", cfg.WriteTimeout, time.Second, 2*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.IdleTimeout, err = durationSetting(lookup, "HTTP_IDLE_TIMEOUT", cfg.IdleTimeout, 5*time.Second, 5*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.WriteTimeout < cfg.ReadTimeout {
		return Config{}, fmt.Errorf("HTTP_WRITE_TIMEOUT must be greater than or equal to HTTP_READ_TIMEOUT")
	}
	if cfg.IdleTimeout < cfg.ReadTimeout {
		return Config{}, fmt.Errorf("HTTP_IDLE_TIMEOUT must be greater than or equal to HTTP_READ_TIMEOUT")
	}
	if cfg.BodyLimit, err = intSetting(lookup, "HTTP_BODY_LIMIT_BYTES", cfg.BodyLimit, 1024, 8<<20); err != nil {
		return Config{}, err
	}
	if cfg.Concurrency, err = intSetting(lookup, "HTTP_CONCURRENCY", cfg.Concurrency, 64, 8192); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func durationSetting(lookup LookupEnv, key string, fallback, minimum, maximum time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(lookup(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	if value < minimum || value > maximum {
		return 0, fmt.Errorf("%s must be between %s and %s", key, minimum, maximum)
	}
	return value, nil
}

func intSetting(lookup LookupEnv, key string, fallback, minimum, maximum int) (int, error) {
	raw := strings.TrimSpace(lookup(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	if value < minimum || value > maximum {
		return 0, fmt.Errorf("%s must be between %d and %d", key, minimum, maximum)
	}
	return value, nil
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
