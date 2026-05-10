package storage

import "fmt"

// BackendConfig holds all backend-specific configuration for the storage
// factory. Only the fields relevant to the selected Backend need to be set.
type BackendConfig struct {
	// Backend selects the storage driver.
	// Valid values: "local" (default), "s3", "azure", "gcs".
	Backend string

	// Local backend — used when Backend == "local".
	Path string // root directory for stored files

	// S3-compatible backend — used when Backend == "s3".
	// Works with AWS S3, MinIO, Backblaze B2, and any S3-compatible service.
	S3Bucket   string // bucket name (required)
	S3Region   string // AWS region (default: "us-east-1")
	S3Endpoint string // custom endpoint URL — set for MinIO and S3-compatible services
	S3KeyID    string // access key ID (required)
	S3Secret   string // secret access key (required)
}

// New creates a Store from the supplied BackendConfig.
// The Backend field selects the driver; remaining fields configure it.
// Callers that load configuration from environment variables should build
// a BackendConfig from *config.Config and pass it here.
func New(cfg BackendConfig) (Store, error) {
	switch cfg.Backend {
	case "local", "":
		return NewLocal(cfg.Path)
	case "s3":
		return NewS3(cfg.S3Bucket, cfg.S3Region, cfg.S3Endpoint, cfg.S3KeyID, cfg.S3Secret)
	case "azure", "gcs":
		return nil, fmt.Errorf("storage backend %q is planned but not yet implemented", cfg.Backend)
	default:
		return nil, fmt.Errorf("unknown storage backend %q: valid options are local, s3, azure, gcs", cfg.Backend)
	}
}
