package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
)

// AzureStore is a content-addressed object store backed by Azure Blob Storage.
//
// Files are stored using the same two-level SHA-256 key layout as Local and S3:
//
//	<first2hex>/<remaining62hex>
//
// This keeps keys identical across all backends, so no migration is needed
// when switching between them.
type AzureStore struct {
	client        *azblob.Client
	containerName string
}

// NewAzure creates an AzureStore. account and key are the storage account name
// and account key; containerName is the blob container to use. endpoint is
// optional: leave empty for real Azure (https://<account>.blob.core.windows.net)
// or set to e.g. "http://localhost:10000" for Azurite.
//
// NewAzure ensures the container exists on first call. This is idempotent —
// if the container already exists the call succeeds silently.
func NewAzure(account, key, containerName, endpoint string) (*AzureStore, error) {
	if account == "" {
		return nil, fmt.Errorf("Azure storage account name must not be empty")
	}
	if key == "" {
		return nil, fmt.Errorf("Azure storage account key must not be empty")
	}
	if containerName == "" {
		return nil, fmt.Errorf("Azure blob container name must not be empty")
	}

	cred, err := azblob.NewSharedKeyCredential(account, key)
	if err != nil {
		return nil, fmt.Errorf("create Azure credential: %w", err)
	}

	// Build the service URL. Azurite uses a path-style URL:
	//   http://<endpoint>/<account>
	// Real Azure uses:
	//   https://<account>.blob.core.windows.net
	var serviceURL string
	if endpoint != "" {
		serviceURL = fmt.Sprintf("%s/%s", endpoint, account)
	} else {
		serviceURL = fmt.Sprintf("https://%s.blob.core.windows.net", account)
	}

	client, err := azblob.NewClientWithSharedKeyCredential(serviceURL, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("create Azure client: %w", err)
	}

	store := &AzureStore{
		client:        client,
		containerName: containerName,
	}

	if err := store.ensureContainer(context.Background()); err != nil {
		return nil, err
	}

	return store, nil
}

// ensureContainer creates the container if it does not already exist.
func (s *AzureStore) ensureContainer(ctx context.Context) error {
	_, err := s.client.CreateContainer(ctx, s.containerName, nil)
	if err != nil {
		var respErr *azcore.ResponseError
		if errors.As(err, &respErr) && bloberror.HasCode(err, bloberror.ContainerAlreadyExists) {
			return nil // container already exists — fine
		}
		return fmt.Errorf("ensure container %q: %w", s.containerName, err)
	}
	return nil
}

// Put streams r into a temporary file, computes its SHA-256 checksum, then
// uploads to Azure Blob if the blob does not already exist (content-addressed
// deduplication). The caller is not required to provide a seek-able reader.
func (s *AzureStore) Put(ctx context.Context, r io.Reader) (key string, size int64, checksum string, err error) {
	// Buffer to a temp file so we know the exact size and checksum before
	// uploading. Azure UploadStream handles chunking internally.
	tmp, err := os.CreateTemp("", "tinydm-azure-*")
	if err != nil {
		return "", 0, "", fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpName)
	}()

	h := sha256.New()
	if size, err = io.Copy(io.MultiWriter(tmp, h), r); err != nil {
		return "", 0, "", fmt.Errorf("buffer content: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		return "", 0, "", fmt.Errorf("sync temp file: %w", err)
	}

	sum := hex.EncodeToString(h.Sum(nil))
	blobName := sum[:2] + "/" + sum[2:]

	// Dedup: skip upload if the blob already exists.
	blobClient := s.client.ServiceClient().NewContainerClient(s.containerName).NewBlobClient(blobName)
	_, headErr := blobClient.GetProperties(ctx, nil)
	if headErr == nil {
		return blobName, size, sum, nil // already present
	}
	if !bloberror.HasCode(headErr, bloberror.BlobNotFound) {
		return "", 0, "", fmt.Errorf("check blob existence: %w", headErr)
	}

	// Seek back to the start of the buffered temp file for the upload.
	if _, err = tmp.Seek(0, io.SeekStart); err != nil {
		return "", 0, "", fmt.Errorf("seek temp file: %w", err)
	}

	_, err = s.client.UploadStream(ctx, s.containerName, blobName, tmp, nil)
	if err != nil {
		return "", 0, "", fmt.Errorf("upload blob: %w", err)
	}

	return blobName, size, sum, nil
}

// Get returns a reader for the blob at key. The caller must close the reader.
func (s *AzureStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	resp, err := s.client.DownloadStream(ctx, s.containerName, key, nil)
	if err != nil {
		if bloberror.HasCode(err, bloberror.BlobNotFound) {
			return nil, fmt.Errorf("content not found: %s", key)
		}
		return nil, fmt.Errorf("download blob: %w", err)
	}
	return resp.Body, nil
}

// Delete removes the blob at key. Returns nil if the blob does not exist.
func (s *AzureStore) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteBlob(ctx, s.containerName, key, nil)
	if err != nil {
		if bloberror.HasCode(err, bloberror.BlobNotFound) {
			return nil // already gone — idempotent
		}
		return fmt.Errorf("delete blob: %w", err)
	}
	return nil
}

// Ping checks that the Azure Blob container is reachable by fetching its
// properties.
func (s *AzureStore) Ping(ctx context.Context) error {
	_, err := s.client.ServiceClient().NewContainerClient(s.containerName).GetProperties(ctx, nil)
	if err != nil {
		return fmt.Errorf("azure ping: %w", err)
	}
	return nil
}

