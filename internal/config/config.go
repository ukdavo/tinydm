package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all runtime configuration for TinyDM.
// Values are loaded from environment variables with sensible defaults.
type Config struct {
	// Server
	Host string // TINYDM_HOST (default: 0.0.0.0)
	Port int    // TINYDM_PORT (default: 8080)

	// Database
	DBPath string // TINYDM_DB_PATH (default: tinydm.db)

	// File storage
	StoragePath string // TINYDM_STORAGE_PATH (default: data/content)

	// Authentication
	JWTSecret          string // TINYDM_JWT_SECRET  — must be changed in production
	JWTExpiryMinutes   int    // TINYDM_JWT_EXPIRY_MINUTES (default: 60)
}

// Load reads configuration from environment variables, falling back to defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Host:               getEnv("TINYDM_HOST", "0.0.0.0"),
		Port:               getEnvInt("TINYDM_PORT", 8080),
		DBPath:             getEnv("TINYDM_DB_PATH", "tinydm.db"),
		StoragePath:        getEnv("TINYDM_STORAGE_PATH", "data/content"),
		JWTSecret:          getEnv("TINYDM_JWT_SECRET", ""),
		JWTExpiryMinutes:   getEnvInt("TINYDM_JWT_EXPIRY_MINUTES", 60),
	}

	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("TINYDM_JWT_SECRET must be set")
	}

	return cfg, nil
}

// Addr returns the full host:port listen address.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
