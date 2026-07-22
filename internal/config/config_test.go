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
	if cfg.Environment != "development" || cfg.HTTPAddr != ":8080" || cfg.ShutdownTimeout != 10*time.Second {
		t.Fatalf("unexpected defaults: %#v", cfg)
	}
}

func TestLoadOverrides(t *testing.T) {
	values := map[string]string{
		"APP_ENV":          "production",
		"HTTP_ADDR":        "127.0.0.1:9090",
		"SHUTDOWN_TIMEOUT": "30s",
	}
	cfg, err := Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Environment != "production" || cfg.HTTPAddr != "127.0.0.1:9090" || cfg.ShutdownTimeout != 30*time.Second {
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
	}
	for _, values := range tests {
		if _, err := Load(func(key string) string { return values[key] }); err == nil {
			t.Fatalf("Load() accepted invalid values: %#v", values)
		}
	}
}
