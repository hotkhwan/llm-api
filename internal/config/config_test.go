package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(func(string) string { return "" })
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Environment != "development" || cfg.HTTPAddr != ":8080" || cfg.ShutdownTimeout != 10*time.Second ||
		cfg.ReadTimeout != 5*time.Second || cfg.WriteTimeout != 15*time.Second || cfg.IdleTimeout != 60*time.Second ||
		cfg.BodyLimit != 1<<20 || cfg.Concurrency != 1024 {
		t.Fatalf("unexpected defaults: %#v", cfg)
	}
}

func TestLoadOverrides(t *testing.T) {
	values := map[string]string{
		"APP_ENV":               "production",
		"HTTP_ADDR":             "127.0.0.1:9090",
		"SHUTDOWN_TIMEOUT":      "30s",
		"HTTP_READ_TIMEOUT":     "2s",
		"HTTP_WRITE_TIMEOUT":    "20s",
		"HTTP_IDLE_TIMEOUT":     "90s",
		"HTTP_BODY_LIMIT_BYTES": "2048",
		"HTTP_CONCURRENCY":      "128",
	}
	cfg, err := Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Environment != "production" || cfg.HTTPAddr != "127.0.0.1:9090" || cfg.ShutdownTimeout != 30*time.Second ||
		cfg.ReadTimeout != 2*time.Second || cfg.WriteTimeout != 20*time.Second || cfg.IdleTimeout != 90*time.Second ||
		cfg.BodyLimit != 2048 || cfg.Concurrency != 128 {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	tests := []map[string]string{
		{"APP_ENV": "prod"},
		{"HTTP_ADDR": "8080"},
		{"HTTP_ADDR": ":70000"},
		{"SHUTDOWN_TIMEOUT": "soon"},
		{"SHUTDOWN_TIMEOUT": "500ms"},
		{"HTTP_READ_TIMEOUT": "999ms"},
		{"HTTP_READ_TIMEOUT": "31s"},
		{"HTTP_WRITE_TIMEOUT": "999ms"},
		{"HTTP_WRITE_TIMEOUT": "121s"},
		{"HTTP_IDLE_TIMEOUT": "4s"},
		{"HTTP_IDLE_TIMEOUT": "301s"},
		{"HTTP_READ_TIMEOUT": "10s", "HTTP_WRITE_TIMEOUT": "5s"},
		{"HTTP_READ_TIMEOUT": "10s", "HTTP_IDLE_TIMEOUT": "5s"},
		{"HTTP_BODY_LIMIT_BYTES": "1023"},
		{"HTTP_BODY_LIMIT_BYTES": "8388609"},
		{"HTTP_BODY_LIMIT_BYTES": "lots"},
		{"HTTP_CONCURRENCY": "63"},
		{"HTTP_CONCURRENCY": "8193"},
		{"HTTP_CONCURRENCY": "many"},
		{"LOCAL_LLM_URL": "not-a-url"},
		{"LOCAL_LLM_URL": "http://user:secret@localhost:18080/v1"},
	}
	for _, values := range tests {
		if _, err := Load(func(key string) string { return values[key] }); err == nil {
			t.Fatalf("Load() accepted invalid values: %#v", values)
		}
	}
}

func TestLoadAcceptsResourceBoundaries(t *testing.T) {
	tests := []map[string]string{
		{"HTTP_READ_TIMEOUT": "1s"},
		{"HTTP_READ_TIMEOUT": "30s", "HTTP_WRITE_TIMEOUT": "30s", "HTTP_IDLE_TIMEOUT": "30s"},
		{"HTTP_WRITE_TIMEOUT": "1s", "HTTP_READ_TIMEOUT": "1s"},
		{"HTTP_WRITE_TIMEOUT": "2m"},
		{"HTTP_IDLE_TIMEOUT": "5s", "HTTP_READ_TIMEOUT": "5s"},
		{"HTTP_IDLE_TIMEOUT": "5m"},
		{"HTTP_BODY_LIMIT_BYTES": "1024"},
		{"HTTP_BODY_LIMIT_BYTES": "8388608"},
		{"HTTP_CONCURRENCY": "64"},
		{"HTTP_CONCURRENCY": "8192"},
	}
	for _, values := range tests {
		if _, err := Load(func(key string) string { return values[key] }); err != nil {
			t.Fatalf("Load() rejected boundary values %#v: %v", values, err)
		}
	}
}
