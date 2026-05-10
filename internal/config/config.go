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
	Host   string // TINYDM_HOST (default: 0.0.0.0)
	Port   int    // TINYDM_PORT (default: 8080)
	NodeID string // TINYDM_NODE_ID — stable node identifier for clustering (default: hostname)

	// Database
	// DBDriver selects the backend: "sqlite" (default) or "postgres".
	DBDriver string // TINYDM_DB_DRIVER
	// DBPath is the SQLite database file path (only used when DBDriver == "sqlite").
	DBPath string // TINYDM_DB_PATH (default: tinydm.db)
	// DBDSN is a PostgreSQL connection string (only used when DBDriver == "postgres").
	// Example: "host=localhost user=tinydm dbname=tinydm sslmode=disable"
	DBDSN string // TINYDM_DB_DSN

	// File storage
	// StorageBackend selects the storage driver: "local" (default), "s3", "azure", or "gcs".
	StorageBackend string // TINYDM_STORAGE_BACKEND (default: "local")
	// StoragePath is the root directory used by the local backend.
	StoragePath string // TINYDM_STORAGE_PATH (default: data/content)

	// S3-compatible storage (AWS S3, MinIO, Backblaze B2, etc.)
	// Required when StorageBackend == "s3".
	S3Bucket   string // TINYDM_S3_BUCKET
	S3Endpoint string // TINYDM_S3_ENDPOINT — optional; set to e.g. "http://localhost:9000" for MinIO
	S3Region   string // TINYDM_S3_REGION (default: "us-east-1")
	S3KeyID    string // TINYDM_S3_KEY_ID
	S3Secret   string // TINYDM_S3_SECRET

	// Azure Blob Storage — required when StorageBackend == "azure".
	AzureAccount   string // TINYDM_AZURE_ACCOUNT — storage account name
	AzureKey       string // TINYDM_AZURE_KEY — storage account key
	AzureContainer string // TINYDM_AZURE_CONTAINER — blob container name
	AzureEndpoint  string // TINYDM_AZURE_ENDPOINT — optional; set to e.g. "http://localhost:10000" for Azurite

	// Google Cloud Storage — required when StorageBackend == "gcs".
	GCSBucket          string // TINYDM_GCS_BUCKET
	GCSProject         string // TINYDM_GCS_PROJECT — GCP project ID (used for bucket creation)
	GCSCredentialsFile string // TINYDM_GCS_CREDENTIALS_FILE — path to service account JSON; empty = ADC

	// Authentication
	JWTSecret        string // TINYDM_JWT_SECRET — must be set; no default
	JWTExpiryMinutes int    // TINYDM_JWT_EXPIRY_MINUTES (default: 60)
	SecureCookies    bool   // TINYDM_SECURE_COOKIES — set true when serving over HTTPS (default: false)

	// Bootstrap — used only on the very first run when the DB has no users.
	// Set all three to seed an initial admin; they are ignored thereafter.
	BootstrapTenantID   string // TINYDM_BOOTSTRAP_TENANT_ID   (default: "default")
	BootstrapTenantName string // TINYDM_BOOTSTRAP_TENANT_NAME (default: "Default")
	BootstrapAdminUser  string // TINYDM_BOOTSTRAP_ADMIN_USER  (default: "admin")
	BootstrapAdminEmail string // TINYDM_BOOTSTRAP_ADMIN_EMAIL (default: "")
	BootstrapAdminPass  string // TINYDM_BOOTSTRAP_ADMIN_PASS  — required for bootstrap
}

// Load reads configuration from environment variables, falling back to defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Host:               getEnv("TINYDM_HOST", "0.0.0.0"),
		Port:               getEnvInt("TINYDM_PORT", 8080),
		NodeID:             getNodeID(),
		DBDriver:           getEnv("TINYDM_DB_DRIVER", "sqlite"),
		DBPath:             getEnv("TINYDM_DB_PATH", "tinydm.db"),
		DBDSN:              getEnv("TINYDM_DB_DSN", ""),
		StorageBackend:     getEnv("TINYDM_STORAGE_BACKEND", "local"),
		StoragePath:        getEnv("TINYDM_STORAGE_PATH", "data/content"),
		S3Bucket:           getEnv("TINYDM_S3_BUCKET", ""),
		S3Endpoint:         getEnv("TINYDM_S3_ENDPOINT", ""),
		S3Region:           getEnv("TINYDM_S3_REGION", "us-east-1"),
		S3KeyID:            getEnv("TINYDM_S3_KEY_ID", ""),
		S3Secret:           getEnv("TINYDM_S3_SECRET", ""),
		AzureAccount:       getEnv("TINYDM_AZURE_ACCOUNT", ""),
		AzureKey:           getEnv("TINYDM_AZURE_KEY", ""),
		AzureContainer:     getEnv("TINYDM_AZURE_CONTAINER", ""),
		AzureEndpoint:      getEnv("TINYDM_AZURE_ENDPOINT", ""),
		GCSBucket:          getEnv("TINYDM_GCS_BUCKET", ""),
		GCSProject:         getEnv("TINYDM_GCS_PROJECT", ""),
		GCSCredentialsFile: getEnv("TINYDM_GCS_CREDENTIALS_FILE", ""),
		JWTSecret:        getEnv("TINYDM_JWT_SECRET", ""),
		JWTExpiryMinutes: getEnvInt("TINYDM_JWT_EXPIRY_MINUTES", 60),
		SecureCookies:    getEnvBool("TINYDM_SECURE_COOKIES", false),

		BootstrapTenantID:   getEnv("TINYDM_BOOTSTRAP_TENANT_ID", "default"),
		BootstrapTenantName: getEnv("TINYDM_BOOTSTRAP_TENANT_NAME", "Default"),
		BootstrapAdminUser:  getEnv("TINYDM_BOOTSTRAP_ADMIN_USER", "admin"),
		BootstrapAdminEmail: getEnv("TINYDM_BOOTSTRAP_ADMIN_EMAIL", ""),
		BootstrapAdminPass:  getEnv("TINYDM_BOOTSTRAP_ADMIN_PASS", ""),
	}

	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("TINYDM_JWT_SECRET must be set")
	}
	if cfg.DBDriver != "sqlite" && cfg.DBDriver != "postgres" {
		return nil, fmt.Errorf("TINYDM_DB_DRIVER must be sqlite or postgres, got %q", cfg.DBDriver)
	}
	if cfg.DBDriver == "postgres" && cfg.DBDSN == "" {
		return nil, fmt.Errorf("TINYDM_DB_DSN must be set when TINYDM_DB_DRIVER=postgres")
	}

	validBackends := map[string]bool{"local": true, "s3": true, "azure": true, "gcs": true}
	if !validBackends[cfg.StorageBackend] {
		return nil, fmt.Errorf("TINYDM_STORAGE_BACKEND must be one of local, s3, azure, gcs; got %q", cfg.StorageBackend)
	}
	if cfg.StorageBackend == "s3" {
		if cfg.S3Bucket == "" {
			return nil, fmt.Errorf("TINYDM_S3_BUCKET must be set when TINYDM_STORAGE_BACKEND=s3")
		}
		if cfg.S3KeyID == "" || cfg.S3Secret == "" {
			return nil, fmt.Errorf("TINYDM_S3_KEY_ID and TINYDM_S3_SECRET must be set when TINYDM_STORAGE_BACKEND=s3")
		}
	}
	if cfg.StorageBackend == "azure" {
		if cfg.AzureAccount == "" {
			return nil, fmt.Errorf("TINYDM_AZURE_ACCOUNT must be set when TINYDM_STORAGE_BACKEND=azure")
		}
		if cfg.AzureKey == "" {
			return nil, fmt.Errorf("TINYDM_AZURE_KEY must be set when TINYDM_STORAGE_BACKEND=azure")
		}
		if cfg.AzureContainer == "" {
			return nil, fmt.Errorf("TINYDM_AZURE_CONTAINER must be set when TINYDM_STORAGE_BACKEND=azure")
		}
	}
	if cfg.StorageBackend == "gcs" {
		if cfg.GCSBucket == "" {
			return nil, fmt.Errorf("TINYDM_GCS_BUCKET must be set when TINYDM_STORAGE_BACKEND=gcs")
		}
	}

	return cfg, nil
}

// Addr returns the full host:port listen address.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// DSN returns the connection string for the configured database driver.
// For SQLite this is the file path; for PostgreSQL it is the libpq DSN.
func (c *Config) DSN() string {
	if c.DBDriver == "postgres" {
		return c.DBDSN
	}
	return c.DBPath
}

// getNodeID returns the value of TINYDM_NODE_ID, falling back to the system
// hostname. This value is used to identify nodes in a multi-node cluster.
func getNodeID() string {
	if v := os.Getenv("TINYDM_NODE_ID"); v != "" {
		return v
	}
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "unknown"
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

func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	switch v {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	}
	return fallback
}
