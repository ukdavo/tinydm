package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3Store is a content-addressed object store backed by any S3-compatible
// service (AWS S3, MinIO, Backblaze B2, etc.).
//
// Files are stored using the same two-level SHA-256 key layout as Local:
//
//	<first2hex>/<remaining62hex>
//
// so keys are identical across backends and no migration is needed when
// switching between them.
type S3Store struct {
	client *s3.Client
	bucket string
	region string
}

// NewS3 creates an S3Store. Credentials are taken from keyID and secret;
// if endpoint is non-empty it overrides the AWS regional endpoint, enabling
// MinIO and other S3-compatible services.
//
// NewS3 creates the bucket on first use if it does not already exist.
// This is idempotent — on AWS the call succeeds silently when the bucket is
// already owned by the same account. Set up the bucket manually in production
// IAM-restricted environments where s3:CreateBucket is not permitted.
func NewS3(bucket, region, endpoint, keyID, secret string) (*S3Store, error) {
	if bucket == "" {
		return nil, fmt.Errorf("S3 bucket name must not be empty")
	}
	if region == "" {
		region = "us-east-1"
	}

	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(keyID, secret, ""),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	var clientOpts []func(*s3.Options)
	if endpoint != "" {
		clientOpts = append(clientOpts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true // required for MinIO and path-style S3 endpoints
		})
	}

	store := &S3Store{
		client: s3.NewFromConfig(cfg, clientOpts...),
		bucket: bucket,
		region: region,
	}

	if err := store.ensureBucket(context.Background()); err != nil {
		return nil, err
	}

	return store, nil
}

// ensureBucket creates the bucket if it does not already exist.
func (s *S3Store) ensureBucket(ctx context.Context) error {
	input := &s3.CreateBucketInput{
		Bucket: aws.String(s.bucket),
	}
	// AWS requires a LocationConstraint for all regions except us-east-1.
	if s.region != "" && s.region != "us-east-1" {
		input.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(s.region),
		}
	}

	_, err := s.client.CreateBucket(ctx, input)
	if err != nil {
		var bae *types.BucketAlreadyExists
		var bao *types.BucketAlreadyOwnedByYou
		if errors.As(err, &bae) || errors.As(err, &bao) {
			return nil // bucket already exists — fine
		}
		return fmt.Errorf("ensure bucket %q: %w", s.bucket, err)
	}
	return nil
}

// Put streams r into a temporary file, computes its SHA-256 checksum, then
// uploads to S3 if the object does not already exist (content-addressed
// deduplication). The caller is not required to provide a seek-able reader.
func (s *S3Store) Put(ctx context.Context, r io.Reader) (key string, size int64, checksum string, err error) {
	// Buffer to a temp file so we know the exact size and checksum before
	// sending to S3, which requires Content-Length on PutObject.
	tmp, err := os.CreateTemp("", "tinydm-s3-*")
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
	s3Key := sum[:2] + "/" + sum[2:]

	// Dedup: skip upload if the object already exists.
	_, headErr := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s3Key),
	})
	if headErr == nil {
		return s3Key, size, sum, nil // already present
	}
	var notFound *types.NotFound
	if !errors.As(headErr, &notFound) {
		return "", 0, "", fmt.Errorf("check object existence: %w", headErr)
	}

	// Seek back to the start of the buffered temp file for the upload.
	if _, err = tmp.Seek(0, io.SeekStart); err != nil {
		return "", 0, "", fmt.Errorf("seek temp file: %w", err)
	}

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(s3Key),
		Body:          tmp,
		ContentLength: aws.Int64(size),
	})
	if err != nil {
		return "", 0, "", fmt.Errorf("upload object: %w", err)
	}

	return s3Key, size, sum, nil
}

// Get returns a reader for the object at key. The caller must close the reader.
func (s *S3Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			return nil, fmt.Errorf("content not found: %s", key)
		}
		return nil, fmt.Errorf("get object: %w", err)
	}
	return out.Body, nil
}

// Delete removes the object at key. Returns nil if the object does not exist.
func (s *S3Store) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			return nil // already gone — idempotent
		}
		return fmt.Errorf("delete object: %w", err)
	}
	return nil
}

// Ping checks that the S3 bucket is reachable by sending a HeadBucket request.
func (s *S3Store) Ping(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	})
	if err != nil {
		return fmt.Errorf("s3 ping: %w", err)
	}
	return nil
}
