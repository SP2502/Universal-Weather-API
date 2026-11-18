package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment       string        `json:"environment"`
	Port              int           `json:"port"`
	BindHost          string        `json:"bind_host"`
	ReadHeaderTimeout time.Duration `json:"read_header_timeout"`
	MaxHeaderBytes    int           `json:"max_header_bytes"`
	LogFile           string        `json:"log_file"`
	ReadTimeout       time.Duration `json:"read_timeout"`
	WriteTimeout      time.Duration `json:"write_timeout"`
	IdleTimeout       time.Duration `json:"idle_timeout"`
	ShutdownTimeout   time.Duration `json:"shutdown_timeout"`
	LogLevel          string        `json:"log_level"`
	Version           string        `json:"version"`

	// Database & Persistence
	DBDriver    string `json:"db_driver"`    // "memory" or "postgres"
	DatabaseURL string `json:"database_url"` // PostgreSQL connection string

	// Distributed Cache
	CacheDriver   string `json:"cache_driver"`   // "memory" or "redis"
	RedisURL      string `json:"redis_url"`      // e.g. "localhost:6379"
	RedisPassword string `json:"redis_password"` // Redis auth password
	RedisDB       int    `json:"redis_db"`       // Redis DB index

	// Secret Management
	SecretDriver string `json:"secret_driver"` // "chained", "file", "env", "memory"
	SecretsDir   string `json:"secrets_dir"`   // Directory for file secrets
}

func Load() (*Config, error) {
	if err := validateEnvTypes(); err != nil {
		return nil, err
	}
	cfg := &Config{
		Environment:       getEnv("APP_ENV", "development"),
		Port:              getEnvInt("APP_PORT", 8080),
		BindHost:          getEnv("APP_BIND_HOST", "127.0.0.1"),
		ReadHeaderTimeout: getEnvDuration("APP_READ_HEADER_TIMEOUT", 5*time.Second),
		MaxHeaderBytes:    getEnvInt("APP_MAX_HEADER_BYTES", 1<<20),
		LogFile:           getEnv("APP_LOG_FILE", ""),
		ReadTimeout:       getEnvDuration("APP_READ_TIMEOUT", 10*time.Second),
		WriteTimeout:      getEnvDuration("APP_WRITE_TIMEOUT", 15*time.Second),
		IdleTimeout:       getEnvDuration("APP_IDLE_TIMEOUT", 60*time.Second),
		ShutdownTimeout:   getEnvDuration("APP_SHUTDOWN_TIMEOUT", 10*time.Second),
		LogLevel:          getEnv("APP_LOG_LEVEL", "INFO"),
		Version:           getEnv("APP_VERSION", "0.1.0"),

		DBDriver:    getEnv("APP_DB_DRIVER", "memory"),
		DatabaseURL: getEnv("APP_DATABASE_URL", ""),

		CacheDriver:   getEnv("APP_CACHE_DRIVER", "memory"),
		RedisURL:      getEnv("APP_REDIS_URL", "localhost:6379"),
		RedisPassword: getEnv("APP_REDIS_PASSWORD", ""),
		RedisDB:       getEnvInt("APP_REDIS_DB", 0),

		SecretDriver: getEnv("APP_SECRET_DRIVER", "chained"),
		SecretsDir:   getEnv("APP_SECRETS_DIR", "/var/run/secrets/uap"),
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	// Preserve compatibility with manually constructed Config values in
	// existing callers while the environment loader supplies explicit defaults.
	if c.BindHost == "" {
		c.BindHost = "127.0.0.1"
	}
	if c.ReadHeaderTimeout == 0 {
		c.ReadHeaderTimeout = 5 * time.Second
	}
	if c.MaxHeaderBytes == 0 {
		c.MaxHeaderBytes = 1 << 20
	}
	if c.Port < 1 || c.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	if strings.TrimSpace(c.BindHost) == "" || strings.ContainsAny(c.BindHost, " /\\") || (strings.Contains(c.BindHost, ":") && net.ParseIP(c.BindHost) == nil) {
		return errors.New("APP_BIND_HOST must be a hostname or IP address without a port")
	}
	if c.ReadHeaderTimeout <= 0 {
		return errors.New("read_header_timeout must be positive")
	}
	if c.MaxHeaderBytes < 4096 || c.MaxHeaderBytes > 8<<20 {
		return errors.New("max_header_bytes must be between 4096 and 8388608")
	}
	if c.ReadTimeout <= 0 {
		return errors.New("read_timeout must be positive")
	}
	if c.WriteTimeout <= 0 {
		return errors.New("write_timeout must be positive")
	}
	if c.RedisDB < 0 {
		return errors.New("APP_REDIS_DB cannot be negative")
	}
	if c.IdleTimeout < 0 {
		return errors.New("idle_timeout cannot be negative")
	}
	if c.ShutdownTimeout <= 0 {
		return errors.New("shutdown_timeout must be positive")
	}
	switch c.LogLevel {
	case "DEBUG", "INFO", "WARN", "ERROR":
	default:
		return fmt.Errorf("invalid log_level: %s", c.LogLevel)
	}

	// Explicit environment names prevent a misspelled production mode from
	// silently enabling the development persistence and bootstrap behavior.
	switch c.Environment {
	case "", "development", "test", "staging", "production":
	default:
		return fmt.Errorf("unsupported APP_ENV value %q", c.Environment)
	}
	if c.Environment == "production" || c.Environment == "staging" {
		if c.DBDriver != "postgres" {
			return errors.New("production/staging requires APP_DB_DRIVER=postgres")
		}
		if c.DatabaseURL == "" {
			return errors.New("production/staging requires APP_DATABASE_URL")
		}
	}

	switch c.DBDriver {
	case "memory", "postgres":
	default:
		return fmt.Errorf("invalid db_driver: %s (must be 'memory' or 'postgres')", c.DBDriver)
	}

	switch c.CacheDriver {
	case "memory", "redis":
	default:
		return fmt.Errorf("invalid cache_driver: %s (must be 'memory' or 'redis')", c.CacheDriver)
	}

	switch c.SecretDriver {
	case "chained", "file", "env", "memory":
	default:
		return fmt.Errorf("invalid secret_driver: %s (must be 'chained', 'file', 'env', or 'memory')", c.SecretDriver)
	}

	return nil
}

func (c *Config) ListenAddress() string { return net.JoinHostPort(c.BindHost, strconv.Itoa(c.Port)) }

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	valStr := os.Getenv(key)
	if valStr == "" {
		return defaultVal
	}
	val, err := strconv.Atoi(valStr)
	if err != nil {
		return defaultVal
	}
	return val
}

func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	valStr := os.Getenv(key)
	if valStr == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(valStr)
	if err != nil {
		return defaultVal
	}
	return d
}

// An invalid security-relevant value must never fall back to a default while
// claiming that configuration validation succeeded.
func validateEnvTypes() error {
	for _, k := range []string{"APP_PORT", "APP_MAX_HEADER_BYTES", "APP_REDIS_DB"} {
		v, ok := os.LookupEnv(k)
		if !ok || v == "" {
			continue
		}
		if _, err := strconv.Atoi(v); err != nil {
			return fmt.Errorf("%s must be an integer", k)
		}
	}
	for _, k := range []string{"APP_READ_HEADER_TIMEOUT", "APP_READ_TIMEOUT", "APP_WRITE_TIMEOUT", "APP_IDLE_TIMEOUT", "APP_SHUTDOWN_TIMEOUT"} {
		v, ok := os.LookupEnv(k)
		if !ok || v == "" {
			continue
		}
		if _, err := time.ParseDuration(v); err != nil {
			return fmt.Errorf("%s must be a valid duration", k)
		}
	}
	return nil
}
