package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	gstorage "cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

// GCSStore is a content-addressed object store backed by Google Cloud Storage.
//
// Files are stored using the same two-level SHA-256 key layout as Local, S3,
// and Azure:
//
//	<first2hex>/<remaining62hex>
//
// This keeps keys identical across all backends, so no migration is needed
// when switching between them.
type GCSStore struct {
	client  *gstorage.Client
	bucket  string
	project string
}

// NewGCS creates a GCSStore. bucket is the GCS bucket name; project is the GCP
// project ID used for bucket creation (may be empty if the bucket already
// exists). credFile is the path to a service-account JSON key file; leave
// empty to use Application Default Credentials (ADC).
func NewGCS(bucket, project, credFile string) (*GCSStore, error) {
	if bucket == "" {
		return nil, fmt.Errorf("GCS bucket name must not be empty")
	}

	var opts []option.ClientOption
	if credFile != "" {
		opts = append(opts, option.WithCredentialsFile(credFile))
	}

	client, err := gstorage.NewClient(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("create GCS client: %w", err)
	}

	return newGCSWithClient(client, bucket, project)
}

// NewGCSWithClient creates a GCSStore from an existing *gstorage.Client.
// This is the preferred constructor for tests, which supply a pre-configured
// fake-gcs-server client.
func NewGCSWithClient(client *gstorage.Client, bucket, project string) (*GCSStore, error) {
	if client == nil {
		return nil, fmt.Errorf("GCS client must not be nil")
	}
	if bucket == "" {
		return nil, fmt.Errorf("GCS bucket name must not be empty")
	}
	return newGCSWithClient(client, bucket, project)
}

// newGCSWithClient is the shared internal constructor.
func newGCSWithClient(client *gstorage.Client, bucket, project string) (*GCSStore, error) {
	store := &GCSStore{
		client:  client,
		bucket:  bucket,
		project: project,
	}
	if err := store.ensureBucket(context.Background()); err != nil {
		return nil, err
	}
	return store, nil
}

// ensureBucket creates the bucket if it does not already exist.
func (s *GCSStore) ensureBucket(ctx context.Context) error {
	bkt := s.client.Bucket(s.bucket)

	_, err := bkt.Attrs(ctx)
	if err == nil {
		return nil // bucket exists
	}
	if !errors.Is(err, gstorage.ErrBucketNotExist) {
		return fmt.Errorf("check bucket %q: %w", s.bucket, err)
	}

	// Bucket does not exist — create it.
	attrs := &gstorage.BucketAttrs{}
	if err := bkt.Create(ctx, s.project, attrs); err != nil {
		return fmt.Errorf("create bucket %q: %w", s.bucket, err)
	}
	return nil
}

// Put streams r into a temporary file, computes its SHA-256 checksum, then
// uploads to GCS if the object does not already exist (content-addressed
// deduplication). The caller is not required to provide a seek-able reader.
func (s *GCSStore) Put(ctx context.Context, r io.Reader) (key string, size int64, checksum string, err error) {
	// Buffer to a temp file so we know the exact size and checksum before
	// uploading.
	tmp, err := os.CreateTemp("", "tinydm-gcs-*")
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
	objName := sum[:2] + "/" + sum[2:]

	// Dedup: skip upload if the object already exists.
	obj := s.client.Bucket(s.bucket).Object(objName)
	_, attrErr := obj.Attrs(ctx)
	if attrErr == nil {
		return objName, size, sum, nil // already present
	}
	if !errors.Is(attrErr, gstorage.ErrObjectNotExist) {
		return "", 0, "", fmt.Errorf("check object existence: %w", attrErr)
	}

	// Seek back to the start of the buffered temp file for the upload.
	if _, err = tmp.Seek(0, io.SeekStart); err != nil {
		return "", 0, "", fmt.Errorf("seek temp file: %w", err)
	}

	w := obj.NewWriter(ctx)
	if _, err = io.Copy(w, tmp); err != nil {
		w.Close() //nolint:errcheck
		return "", 0, "", fmt.Errorf("upload object: %w", err)
	}
	if err = w.Close(); err != nil {
		return "", 0, "", fmt.Errorf("finalise upload: %w", err)
	}

	return objName, size, sum, nil
}

// Get returns a reader for the object at key. The caller must close the reader.
func (s *GCSStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	rc, err := s.client.Bucket(s.bucket).Object(key).NewReader(ctx)
	if err != nil {
		if errors.Is(err, gstorage.ErrObjectNotExist) {
			return nil, fmt.Errorf("content not found: %s", key)
		}
		return nil, fmt.Errorf("get object: %w", err)
	}
	return rc, nil
}

// Delete removes the object at key. Returns nil if the object does not exist.
func (s *GCSStore) Delete(ctx context.Context, key string) error {
	err := s.client.Bucket(s.bucket).Object(key).Delete(ctx)
	if err != nil {
		if errors.Is(err, gstorage.ErrObjectNotExist) {
			return nil // already gone — idempotent
		}
		return fmt.Errorf("delete object: %w", err)
	}
	return nil
}
