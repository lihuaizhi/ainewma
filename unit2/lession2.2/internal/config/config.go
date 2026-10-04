// Package config loads service settings from the environment and validates
// them once at startup.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds every setting the server needs.
type Config struct {
	// HTTPAddr is the listen address of this service.
	HTTPAddr string
	// LLMAPIURL is either a full chat completions endpoint or a base URL
	// ending in a version segment such as /v1.
	LLMAPIURL string
	// APIKey authenticates against the upstream provider. It may be empty for
	// gateways that do not require it.
	APIKey string
	// Model is the default model used when a request omits the model field.
	Model string
	// OrgID and ProjectID are optional upstream routing hints.
	OrgID     string
	ProjectID string
	// MaxRetries is the upstream retry budget for non-streaming calls.
	MaxRetries int
	// ResponseHeaderTimeout bounds the wait for upstream response headers.
	// Streams must not set this, since tokens arrive over a long period.
	ResponseHeaderTimeout time.Duration
	// MaxRequestBody caps the accepted JSON request body in bytes.
	MaxRequestBody int64
	// ShutdownTimeout bounds graceful shutdown.
	ShutdownTimeout time.Duration
}

// Load reads and validates configuration from the process environment.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:              envString("HTTP_ADDR", ":8080"),
		LLMAPIURL:             envString("LLM_API_URL", "https://api.openai.com/v1"),
		APIKey:                strings.TrimSpace(os.Getenv("LLM_API_KEY")),
		Model:                 envString("LLM_MODEL", "gpt-4o-mini"),
		OrgID:                 strings.TrimSpace(os.Getenv("LLM_ORG_ID")),
		ProjectID:             strings.TrimSpace(os.Getenv("LLM_PROJECT_ID")),
		ResponseHeaderTimeout: 60 * time.Second,
		MaxRequestBody:        1 << 20,
		ShutdownTimeout:       15 * time.Second,
	}

	var err error
	if cfg.ResponseHeaderTimeout, err = envDuration("LLM_RESPONSE_HEADER_TIMEOUT", cfg.ResponseHeaderTimeout); err != nil {
		return Config{}, err
	}
	if cfg.MaxRequestBody, err = envInt64("LLM_MAX_REQUEST_BODY", cfg.MaxRequestBody); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = envDuration("SHUTDOWN_TIMEOUT", cfg.ShutdownTimeout); err != nil {
		return Config{}, err
	}
	if cfg.MaxRetries, err = envInt("LLM_MAX_RETRIES", cfg.MaxRetries); err != nil {
		return Config{}, err
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (cfg Config) validate() error {
	if strings.TrimSpace(cfg.HTTPAddr) == "" {
		return fmt.Errorf("HTTP_ADDR must not be empty")
	}
	if strings.TrimSpace(cfg.LLMAPIURL) == "" {
		return fmt.Errorf("LLM_API_URL must not be empty")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return fmt.Errorf("LLM_MODEL must not be empty")
	}
	if cfg.ResponseHeaderTimeout <= 0 {
		return fmt.Errorf("LLM_RESPONSE_HEADER_TIMEOUT must be positive")
	}
	if cfg.MaxRequestBody <= 0 {
		return fmt.Errorf("LLM_MAX_REQUEST_BODY must be positive")
	}
	if cfg.ShutdownTimeout <= 0 {
		return fmt.Errorf("SHUTDOWN_TIMEOUT must be positive")
	}
	if cfg.MaxRetries < 0 || cfg.MaxRetries > 10 {
		return fmt.Errorf("LLM_MAX_RETRIES must be between 0 and 10")
	}
	return nil
}

func envString(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func envDuration(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s is invalid: %w", name, err)
	}
	return duration, nil
}

func envInt64(name string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s is invalid: %w", name, err)
	}
	return number, nil
}

func envInt(name string, fallback int) (int, error) {
	value, err := envInt64(name, int64(fallback))
	return int(value), err
}
