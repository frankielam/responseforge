package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	ProtocolVersion     = 1
	DefaultMaxBodyBytes = 2 * 1024 * 1024
	DefaultLLMTimeout   = 90 * time.Second
	DefaultLLMSample    = 12000
	WorkerMaxBodyBytes  = 1 * 1024 * 1024
)

// Config is process configuration. Secrets come from the environment only.
type Config struct {
	Addr            string
	Storage         string
	DataDir         string
	DatabaseURL     string
	IngestToken     string
	MaxBodyBytes    int64
	LLMBaseURL      string
	LLMAPIKey       string
	LLMModel        string
	LLMTimeout      time.Duration
	LLMMaxSample    int
	PostgresTimeout time.Duration
	Debug           bool
}

func (c Config) LLMEnabled() bool {
	return strings.TrimSpace(c.LLMBaseURL) != ""
}

func (c Config) Database() bool {
	return c.Storage == "postgres"
}

// Load reads environment variables. An unset STORAGE value means CSV.
func Load() (Config, error) {
	storage := strings.ToLower(strings.TrimSpace(os.Getenv("STORAGE")))
	if storage == "" {
		storage = "csv"
	}
	if storage != "csv" && storage != "postgres" {
		return Config{}, fmt.Errorf("STORAGE must be csv or postgres, got %q", storage)
	}
	maxBody := int64(DefaultMaxBodyBytes)
	if raw := strings.TrimSpace(os.Getenv("MAX_BODY_BYTES")); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("MAX_BODY_BYTES must be a positive integer")
		}
		maxBody = n
	}
	timeout := DefaultLLMTimeout
	if raw := strings.TrimSpace(os.Getenv("LLM_TIMEOUT_SECONDS")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("LLM_TIMEOUT_SECONDS must be a positive integer")
		}
		timeout = time.Duration(n) * time.Second
	}
	sample := DefaultLLMSample
	if raw := strings.TrimSpace(os.Getenv("LLM_MAX_SAMPLE_CHARS")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("LLM_MAX_SAMPLE_CHARS must be a positive integer")
		}
		sample = n
	}
	pgTimeout := 30 * time.Second
	if raw := strings.TrimSpace(os.Getenv("POSTGRES_CONNECT_TIMEOUT_SECONDS")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("POSTGRES_CONNECT_TIMEOUT_SECONDS must be a positive integer")
		}
		pgTimeout = time.Duration(n) * time.Second
	}
	dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if dataDir == "" {
		dataDir = "./private/data"
	}
	addr := strings.TrimSpace(os.Getenv("ADDR"))
	if addr == "" {
		addr = ":8080"
	}
	cfg := Config{
		Addr:            addr,
		Storage:         storage,
		DataDir:         dataDir,
		DatabaseURL:     strings.TrimSpace(os.Getenv("DATABASE_URL")),
		IngestToken:     os.Getenv("INGEST_TOKEN"),
		MaxBodyBytes:    maxBody,
		LLMBaseURL:      strings.TrimRight(strings.TrimSpace(os.Getenv("LLM_BASE_URL")), "/"),
		LLMAPIKey:       os.Getenv("LLM_API_KEY"),
		LLMModel:        strings.TrimSpace(os.Getenv("LLM_MODEL")),
		LLMTimeout:      timeout,
		LLMMaxSample:    sample,
		PostgresTimeout: pgTimeout,
		Debug:           debugEnabled(),
	}
	if cfg.Database() && cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required when STORAGE=postgres")
	}
	if cfg.LLMEnabled() && cfg.LLMModel == "" {
		return Config{}, fmt.Errorf("LLM_MODEL is required when LLM_BASE_URL is set")
	}
	return cfg, nil
}

func debugEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DEBUG"))) {
	case "1", "true", "yes", "on", "debug":
		return true
	default:
		return false
	}
}
