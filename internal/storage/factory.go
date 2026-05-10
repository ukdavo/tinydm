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

	// Azure Blob Storage — used when Backend == "azure".
	AzureAccount   string // storage account name (required)
	AzureKey       string // storage account key (required)
	AzureContainer string // blob container name (required)
	AzureEndpoint  string // custom endpoint URL — set for Azurite; empty = real Azure

	// Google Cloud Storage — used when Backend == "gcs".
	GCSBucket          string // bucket name (required)
	GCSProject         string // GCP project ID — used for bucket creation
	GCSCredentialsFile string // path to service-account JSON; empty = Application Default Credentials
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
	case "azure":
		return NewAzure(cfg.AzureAccount, cfg.AzureKey, cfg.AzureContainer, cfg.AzureEndpoint)
	case "gcs":
		return NewGCS(cfg.GCSBucket, cfg.GCSProject, cfg.GCSCredentialsFile)
	default:
		return nil, fmt.Errorf("unknown storage backend %q: valid options are local, s3, azure, gcs", cfg.Backend)
	}
}
