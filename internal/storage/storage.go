// Package storage defines the content-store abstraction and a local
// filesystem implementation. Future backends (S3, NFS, etc.) implement
// the Store interface without touching any other layer.
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Store is the content-store interface. All document bytes flow through it.
type Store interface {
	// Put writes r to the store and returns the storage key.
	Put(ctx context.Context, r io.Reader) (key string, size int64, checksum string, err error)
	// Get returns a reader for the content at key.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes the content at key. Implementations should be
	// idempotent — deleting a non-existent key is not an error.
	Delete(ctx context.Context, key string) error
	// Ping checks that the backend is reachable and the storage location is
	// accessible. Used by the /health endpoint.
	Ping(ctx context.Context) error
}

// Local is a content-addressed local-filesystem store.
// Files are stored under basePath/<first2>/<remaining> where
// <first2>/<remaining> is the hex-encoded SHA-256 of the content.
type Local struct {
	basePath string
}

// NewLocal creates (or opens) a local content store at basePath.
func NewLocal(basePath string) (*Local, error) {
	if err := os.MkdirAll(basePath, 0o750); err != nil {
		return nil, fmt.Errorf("create storage directory: %w", err)
	}
	return &Local{basePath: basePath}, nil
}

// Put streams r to a temp file, computes its SHA-256, then moves it into the
// content-addressed location. Identical content is automatically deduplicated.
func (l *Local) Put(_ context.Context, r io.Reader) (key string, size int64, checksum string, err error) {
	tmp, err := os.CreateTemp(l.basePath, "upload-*")
	if err != nil {
		return "", 0, "", fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	// Clean up the temp file on any error path.
	defer func() {
		if err != nil {
			os.Remove(tmpName)
		}
	}()

	h := sha256.New()
	w := io.MultiWriter(tmp, h)

	if size, err = io.Copy(w, r); err != nil {
		tmp.Close()
		return "", 0, "", fmt.Errorf("write content: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return "", 0, "", fmt.Errorf("close temp file: %w", err)
	}

	hex := hex.EncodeToString(h.Sum(nil))
	dir := filepath.Join(l.basePath, hex[:2])
	if err = os.MkdirAll(dir, 0o750); err != nil {
		return "", 0, "", fmt.Errorf("create content dir: %w", err)
	}

	dest := filepath.Join(dir, hex[2:])
	if _, err = os.Stat(dest); os.IsNotExist(err) {
		if err = os.Rename(tmpName, dest); err != nil {
			return "", 0, "", fmt.Errorf("move content file: %w", err)
		}
	} else {
		// Content already exists — discard the duplicate.
		os.Remove(tmpName)
	}

	return hex[:2] + "/" + hex[2:], size, hex, nil
}

// Get opens the file at key for reading.
func (l *Local) Get(_ context.Context, key string) (io.ReadCloser, error) {
	path := filepath.Join(l.basePath, filepath.FromSlash(key))
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("content not found: %s", key)
		}
		return nil, fmt.Errorf("open content: %w", err)
	}
	return f, nil
}

// Delete removes the file at key. Returns nil if the file does not exist.
func (l *Local) Delete(_ context.Context, key string) error {
	path := filepath.Join(l.basePath, filepath.FromSlash(key))
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete content: %w", err)
	}
	return nil
}

// Ping checks that the local storage directory is accessible.
func (l *Local) Ping(_ context.Context) error {
	if _, err := os.Stat(l.basePath); err != nil {
		return fmt.Errorf("local storage unavailable: %w", err)
	}
	return nil
}
