package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHTTPAddr        = ":8080"
	defaultEnvironment     = "development"
	defaultShutdownTimeout = 10 * time.Second
	defaultReadTimeout     = 5 * time.Second
	defaultWriteTimeout    = 120 * time.Second
	defaultIdleTimeout     = 60 * time.Second
	defaultLocalLLMTimeout = 105 * time.Second
	defaultBodyLimit       = 64 << 20
	defaultConcurrency     = 1024
)

type Config struct {
	Environment                string
	HTTPAddr                   string
	ShutdownTimeout            time.Duration
	ReadTimeout                time.Duration
	WriteTimeout               time.Duration
	IdleTimeout                time.Duration
	BodyLimit                  int
	Concurrency                int
	BasePath                   string
	LocalLLMURL                string
	LocalLLMModel              string
	LocalLLMAPIKey             string
	LocalLLMTimeout            time.Duration
	MongoURI                   string
	MongoDatabase              string
	S3Endpoint                 string
	S3PublicEndpoint           string
	S3Region                   string
	S3Bucket                   string
	S3Prefix                   string
	S3AccessKey                string
	S3SecretKey                string
	OIDCIssuer                 string
	OIDCAudience               string
	AllowTrustedIdentityHeader bool
	ShotVLURL                  string
	ShotVLModel                string
	ShotVLModelRevision        string
	ShotVLAPIKey               string
	ShotVLThreshold            float64
	FidelityVLMURL             string
	FidelityVLMModel           string
	FidelityVLMModelRevision   string
	FidelityVLMAPIKey          string
	FidelityVLMThreshold       float64
	VeoBaseURL                 string
	VeoModel                   string
	VeoAPIKey                  string
	SeedanceBaseURL            string
	SeedanceModel              string
	SeedanceAPIKey             string
}

type LookupEnv func(string) string

// Load reads only non-secret runtime settings. Credentials must be injected by
// future integrations through a documented secret provider, never this config.
func Load(lookup LookupEnv) (Config, error) {
	cfg := Config{
		Environment:                valueOrDefault(lookup("APP_ENV"), defaultEnvironment),
		HTTPAddr:                   valueOrDefault(lookup("HTTP_ADDR"), defaultHTTPAddr),
		ShutdownTimeout:            defaultShutdownTimeout,
		ReadTimeout:                defaultReadTimeout,
		WriteTimeout:               defaultWriteTimeout,
		IdleTimeout:                defaultIdleTimeout,
		BodyLimit:                  defaultBodyLimit,
		Concurrency:                defaultConcurrency,
		BasePath:                   strings.TrimRight(strings.TrimSpace(lookup("APP_BASE_PATH")), "/"),
		LocalLLMURL:                strings.TrimSpace(lookup("LOCAL_LLM_URL")),
		LocalLLMModel:              valueOrDefault(lookup("LOCAL_LLM_MODEL"), "qwen3.6-27b-q8_0-mtp-16k"),
		LocalLLMAPIKey:             strings.TrimSpace(lookup("LOCAL_LLM_API_KEY")),
		LocalLLMTimeout:            defaultLocalLLMTimeout,
		MongoURI:                   strings.TrimSpace(lookup("MONGO_URI")),
		MongoDatabase:              valueOrDefault(lookup("MONGO_DATABASE"), "kwanni"),
		S3Endpoint:                 strings.TrimSpace(lookup("S3_ENDPOINT")),
		S3PublicEndpoint:           valueOrDefault(lookup("S3_PRESIGN_ENDPOINT"), strings.TrimSpace(lookup("S3_PUBLIC_BASE_URL"))),
		S3Region:                   valueOrDefault(lookup("S3_REGION"), "us-east-1"),
		S3Bucket:                   valueOrDefault(lookup("S3_BUCKET"), "llm-api"),
		S3Prefix:                   valueOrDefault(lookup("S3_PREFIX"), "mission-zero"),
		S3AccessKey:                strings.TrimSpace(lookup("S3_ACCESS_KEY")),
		S3SecretKey:                strings.TrimSpace(lookup("S3_SECRET_KEY")),
		OIDCIssuer:                 strings.TrimRight(strings.TrimSpace(lookup("OIDC_ISSUER")), "/"),
		OIDCAudience:               strings.TrimSpace(lookup("OIDC_AUDIENCE")),
		AllowTrustedIdentityHeader: strings.EqualFold(strings.TrimSpace(lookup("ALLOW_TRUSTED_IDENTITY_HEADER")), "true"),
		ShotVLURL:                  strings.TrimSpace(lookup("SHOTVL_URL")),
		ShotVLModel:                valueOrDefault(lookup("SHOTVL_MODEL"), "ShotVL-7B"),
		ShotVLModelRevision:        valueOrDefault(lookup("SHOTVL_MODEL_REVISION"), "unversioned"),
		ShotVLAPIKey:               strings.TrimSpace(lookup("SHOTVL_API_KEY")),
		ShotVLThreshold:            0.75,
		FidelityVLMURL:             strings.TrimSpace(lookup("FIDELITY_VLM_URL")),
		FidelityVLMModel:           strings.TrimSpace(lookup("FIDELITY_VLM_MODEL")),
		FidelityVLMModelRevision:   strings.TrimSpace(lookup("FIDELITY_VLM_MODEL_REVISION")),
		FidelityVLMAPIKey:          strings.TrimSpace(lookup("FIDELITY_VLM_API_KEY")),
		FidelityVLMThreshold:       0.88,
		VeoBaseURL:                 valueOrDefault(lookup("VEO_BASE_URL"), "https://generativelanguage.googleapis.com/v1beta"),
		VeoModel:                   valueOrDefault(lookup("VEO_MODEL"), "veo-3.1-generate-preview"),
		VeoAPIKey:                  strings.TrimSpace(lookup("VEO_API_KEY")),
		SeedanceBaseURL:            valueOrDefault(lookup("SEEDANCE_BASE_URL"), "https://ark.cn-beijing.volces.com/api/v3"),
		SeedanceModel:              strings.TrimSpace(lookup("SEEDANCE_MODEL")),
		SeedanceAPIKey:             strings.TrimSpace(lookup("SEEDANCE_API_KEY")),
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
	if cfg.LocalLLMTimeout, err = durationSetting(lookup, "LOCAL_LLM_TIMEOUT", cfg.LocalLLMTimeout, 5*time.Second, 110*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.LocalLLMURL != "" && cfg.LocalLLMTimeout >= cfg.WriteTimeout {
		return Config{}, fmt.Errorf("LOCAL_LLM_TIMEOUT must be shorter than HTTP_WRITE_TIMEOUT")
	}
	if cfg.WriteTimeout < cfg.ReadTimeout {
		return Config{}, fmt.Errorf("HTTP_WRITE_TIMEOUT must be greater than or equal to HTTP_READ_TIMEOUT")
	}
	if cfg.IdleTimeout < cfg.ReadTimeout {
		return Config{}, fmt.Errorf("HTTP_IDLE_TIMEOUT must be greater than or equal to HTTP_READ_TIMEOUT")
	}
	if cfg.BodyLimit, err = intSetting(lookup, "HTTP_BODY_LIMIT_BYTES", cfg.BodyLimit, 1024, 256<<20); err != nil {
		return Config{}, err
	}
	if cfg.Concurrency, err = intSetting(lookup, "HTTP_CONCURRENCY", cfg.Concurrency, 64, 8192); err != nil {
		return Config{}, err
	}
	if cfg.LocalLLMURL != "" {
		parsed, parseErr := url.Parse(cfg.LocalLLMURL)
		if parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
			return Config{}, fmt.Errorf("LOCAL_LLM_URL must be an absolute http(s) URL")
		}
	}
	if raw := strings.TrimSpace(lookup("SHOTVL_THRESHOLD")); raw != "" {
		value, parseErr := strconv.ParseFloat(raw, 64)
		if parseErr != nil || value < 0 || value > 1 {
			return Config{}, fmt.Errorf("SHOTVL_THRESHOLD must be between 0 and 1")
		}
		cfg.ShotVLThreshold = value
	}
	if raw := strings.TrimSpace(lookup("FIDELITY_VLM_THRESHOLD")); raw != "" {
		value, parseErr := strconv.ParseFloat(raw, 64)
		if parseErr != nil || value < 0 || value > 1 {
			return Config{}, fmt.Errorf("FIDELITY_VLM_THRESHOLD must be between 0 and 1")
		}
		cfg.FidelityVLMThreshold = value
	}
	if cfg.ShotVLURL != "" {
		parsed, parseErr := url.Parse(cfg.ShotVLURL)
		if parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
			return Config{}, fmt.Errorf("SHOTVL_URL must be an absolute credential-free http(s) URL")
		}
		if cfg.ShotVLModelRevision == "unversioned" {
			return Config{}, fmt.Errorf("SHOTVL_MODEL_REVISION must be immutable when ShotVL is enabled")
		}
	}
	if cfg.FidelityVLMURL != "" {
		parsed, parseErr := url.Parse(cfg.FidelityVLMURL)
		if parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
			return Config{}, fmt.Errorf("FIDELITY_VLM_URL must be an absolute credential-free http(s) URL")
		}
		if cfg.FidelityVLMModel == "" || cfg.FidelityVLMModelRevision == "" {
			return Config{}, fmt.Errorf("FIDELITY_VLM_MODEL and immutable FIDELITY_VLM_MODEL_REVISION are required")
		}
	}
	if (cfg.VeoAPIKey != "" || cfg.SeedanceAPIKey != "") && (cfg.FidelityVLMURL == "" || cfg.ShotVLURL == "" || cfg.LocalLLMURL == "") {
		return Config{}, fmt.Errorf("cloud video providers require Local Qwen, product fidelity VLM and ShotVL")
	}
	if cfg.SeedanceAPIKey != "" && cfg.SeedanceModel == "" {
		return Config{}, fmt.Errorf("SEEDANCE_MODEL is required when Seedance is enabled")
	}
	for key, value := range map[string]string{"VEO_BASE_URL": cfg.VeoBaseURL, "SEEDANCE_BASE_URL": cfg.SeedanceBaseURL} {
		parsed, parseErr := url.Parse(value)
		if parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
			return Config{}, fmt.Errorf("%s must be an absolute credential-free http(s) URL", key)
		}
		if cfg.Environment == "production" && parsed.Scheme != "https" {
			return Config{}, fmt.Errorf("production %s must use https", key)
		}
	}
	if cfg.BasePath != "" && (!strings.HasPrefix(cfg.BasePath, "/") || strings.Contains(cfg.BasePath, "..") || strings.ContainsAny(cfg.BasePath, "?#")) {
		return Config{}, fmt.Errorf("APP_BASE_PATH must be an absolute clean URL path")
	}
	durableValues := []string{cfg.MongoURI, cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey}
	durableCount := 0
	for _, value := range durableValues {
		if value != "" {
			durableCount++
		}
	}
	if durableCount != 0 && durableCount != len(durableValues) {
		return Config{}, fmt.Errorf("MONGO_URI, S3_ENDPOINT, S3_ACCESS_KEY and S3_SECRET_KEY must be configured together")
	}
	if cfg.Environment == "production" && durableCount != len(durableValues) {
		return Config{}, fmt.Errorf("production requires MongoDB and SeaweedFS S3 configuration")
	}
	if cfg.ShotVLURL != "" && durableCount != len(durableValues) {
		return Config{}, fmt.Errorf("SHOTVL_URL requires durable MongoDB and SeaweedFS configuration")
	}
	if cfg.Environment == "production" && cfg.S3PublicEndpoint == "" {
		return Config{}, fmt.Errorf("production requires S3_PRESIGN_ENDPOINT or S3_PUBLIC_BASE_URL for browser-safe signed downloads")
	}
	for key, value := range map[string]string{"S3_ENDPOINT": cfg.S3Endpoint, "S3_PRESIGN_ENDPOINT": cfg.S3PublicEndpoint} {
		if value == "" {
			continue
		}
		parsed, parseErr := url.Parse(value)
		if parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
			return Config{}, fmt.Errorf("%s must be an absolute credential-free http(s) URL", key)
		}
	}
	if cfg.Environment == "production" && !strings.HasPrefix(cfg.S3PublicEndpoint, "https://") {
		return Config{}, fmt.Errorf("production S3 presign endpoint must use https")
	}
	if (cfg.OIDCIssuer == "") != (cfg.OIDCAudience == "") {
		return Config{}, fmt.Errorf("OIDC_ISSUER and OIDC_AUDIENCE must be configured together")
	}
	if cfg.Environment == "production" && (cfg.OIDCIssuer == "" || cfg.AllowTrustedIdentityHeader) {
		return Config{}, fmt.Errorf("production requires OIDC bearer verification and forbids trusted identity headers")
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
