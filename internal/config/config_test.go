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
		cfg.ReadTimeout != 5*time.Second || cfg.WriteTimeout != 120*time.Second || cfg.IdleTimeout != 60*time.Second || cfg.LocalLLMTimeout != 105*time.Second ||
		cfg.BodyLimit != 64<<20 || cfg.Concurrency != 1024 {
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
		"LOCAL_LLM_TIMEOUT":     "19s",
		"MONGO_URI":             "mongodb://mongo.invalid:27017",
		"S3_ENDPOINT":           "http://s3.invalid:9000",
		"S3_PRESIGN_ENDPOINT":   "https://site-s3.invalid",
		"S3_ACCESS_KEY":         "test-access",
		"S3_SECRET_KEY":         "test-secret",
		"OIDC_ISSUER":           "https://identity.invalid/realms/project",
		"OIDC_AUDIENCE":         "kwanni-api",
	}
	cfg, err := Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Environment != "production" || cfg.HTTPAddr != "127.0.0.1:9090" || cfg.ShutdownTimeout != 30*time.Second ||
		cfg.ReadTimeout != 2*time.Second || cfg.WriteTimeout != 20*time.Second || cfg.IdleTimeout != 90*time.Second ||
		cfg.BodyLimit != 2048 || cfg.Concurrency != 128 || cfg.LocalLLMTimeout != 19*time.Second {
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
		{"HTTP_BODY_LIMIT_BYTES": "268435457"},
		{"APP_BASE_PATH": "relative"},
		{"APP_BASE_PATH": "/dev/../secret"},
		{"HTTP_BODY_LIMIT_BYTES": "lots"},
		{"HTTP_CONCURRENCY": "63"},
		{"HTTP_CONCURRENCY": "8193"},
		{"HTTP_CONCURRENCY": "many"},
		{"LOCAL_LLM_URL": "not-a-url"},
		{"LOCAL_LLM_URL": "http://user:secret@localhost:18080/v1"},
		{"LOCAL_LLM_TIMEOUT": "4s"},
		{"LOCAL_LLM_TIMEOUT": "111s"},
		{"LOCAL_LLM_URL": "http://localhost:18080/v1", "HTTP_WRITE_TIMEOUT": "30s", "LOCAL_LLM_TIMEOUT": "30s"},
		{"SHOTVL_THRESHOLD": "1.1"},
		{"SHOTVL_THRESHOLD": "fast"},
		{"SHOTVL_URL": "not-a-url"},
		{"WAN_URL": "not-a-url", "WAN_API_KEY": "test"},
		{"WAN_URL": "http://wan.invalid"},
		{"WAN_TIMEOUT": "30s"},
		{"WAN_TIMEOUT": "61m"},
		{"HUNYUAN_URL": "not-a-url", "HUNYUAN_API_KEY": "test"},
		{"HUNYUAN_URL": "http://hunyuan.invalid"},
		{"HUNYUAN_API_KEY": "test"},
		{"HUNYUAN_TIMEOUT": "30s"},
		{"LTX_URL": "http://user:secret@ltx.invalid", "LTX_API_KEY": "test"},
		{"LTX_URL": "http://ltx.invalid"},
		{"LTX_API_KEY": "test"},
		{"LTX_TIMEOUT": "61m"},
	}
	for _, values := range tests {
		if _, err := Load(func(key string) string { return values[key] }); err == nil {
			t.Fatalf("Load() accepted invalid values: %#v", values)
		}
	}
}

func TestLoadAcceptsPrivateHunyuanAndLTXRuntimes(t *testing.T) {
	values := map[string]string{
		"HUNYUAN_URL": "http://kwanni-hunyuan.dev.svc.cluster.local:8091", "HUNYUAN_API_KEY": "hunyuan-test", "HUNYUAN_TIMEOUT": "15m",
		"LTX_URL": "http://kwanni-ltx.dev.svc.cluster.local:8092", "LTX_API_KEY": "ltx-test", "LTX_TIMEOUT": "20m",
	}
	cfg, err := Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HunyuanTimeout != 15*time.Minute || cfg.LTXTimeout != 20*time.Minute || cfg.HunyuanURL == "" || cfg.LTXURL == "" {
		t.Fatalf("unexpected local runtime config: %#v", cfg)
	}
}

func TestLoadAcceptsPrivateWanRuntime(t *testing.T) {
	values := map[string]string{"WAN_URL": "http://kwanni-wan.dev.svc.cluster.local:8090", "WAN_API_KEY": "test-only", "WAN_TIMEOUT": "30m"}
	cfg, err := Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WanTimeout != 30*time.Minute || cfg.WanURL == "" || cfg.WanAPIKey == "" {
		t.Fatalf("unexpected Wan config: %#v", cfg)
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
		{"HTTP_BODY_LIMIT_BYTES": "268435456"},
		{"APP_BASE_PATH": "/dev/llm-api"},
		{"HTTP_CONCURRENCY": "64"},
		{"HTTP_CONCURRENCY": "8192"},
	}
	for _, values := range tests {
		if _, err := Load(func(key string) string { return values[key] }); err != nil {
			t.Fatalf("Load() rejected boundary values %#v: %v", values, err)
		}
	}
}
