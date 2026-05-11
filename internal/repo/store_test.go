package repo_test

import (
	"context"
	"path/filepath"
	"testing"

	"tinydm/internal/db"
	"tinydm/internal/repo"
)

func newTestStore(t *testing.T) (*repo.Store, *db.DB) {
	t.Helper()
	sqlDB, err := db.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	return repo.NewStore(sqlDB), sqlDB
}

func seedProject(t *testing.T, s *repo.Store, name string) *repo.Project {
	t.Helper()
	p, err := s.CreateProject(context.Background(), name, "")
	if err != nil {
		t.Fatalf("CreateProject %q: %v", name, err)
	}
	return p
}

func seedBucket(t *testing.T, s *repo.Store, projectID, name string) *repo.Bucket {
	t.Helper()
	b, err := s.CreateBucket(context.Background(), projectID, name, "")
	if err != nil {
		t.Fatalf("CreateBucket %q: %v", name, err)
	}
	return b
}

func TestCountBucketsInProject_ReturnsCorrectCount(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	p1 := seedProject(t, s, "alpha")
	p2 := seedProject(t, s, "beta")
	seedBucket(t, s, p1.ID, "b1")
	seedBucket(t, s, p1.ID, "b2")
	seedBucket(t, s, p2.ID, "b3")

	n, err := s.CountBucketsInProject(ctx, p1.ID)
	if err != nil {
		t.Fatalf("CountBucketsInProject: %v", err)
	}
	if n != 2 {
		t.Errorf("got %d, want 2", n)
	}
}

func TestCountDocumentsInProject_ReturnsZeroForEmptyProject(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	p1 := seedProject(t, s, "alpha")

	n, err := s.CountDocumentsInProject(ctx, p1.ID)
	if err != nil {
		t.Fatalf("CountDocumentsInProject: %v", err)
	}
	if n != 0 {
		t.Errorf("empty project: got %d, want 0", n)
	}
}

func TestCountDocumentsInProject_CountsDocumentsAcrossBuckets(t *testing.T) {
	ctx := context.Background()
	s, sqlDB := newTestStore(t)
	p1 := seedProject(t, s, "alpha")
	p2 := seedProject(t, s, "beta")
	b1 := seedBucket(t, s, p1.ID, "bkt1")
	b2 := seedBucket(t, s, p1.ID, "bkt2")
	b3 := seedBucket(t, s, p2.ID, "bkt3")

	// Insert 2 documents in b1 and 1 in b2 (total 3 in p1).
	for _, row := range []struct {
		id, bucketID, name string
	}{
		{"doc-1", b1.ID, "file1.txt"},
		{"doc-2", b1.ID, "file2.txt"},
		{"doc-3", b2.ID, "file3.txt"},
	} {
		_, err := sqlDB.ExecContext(ctx,
			`INSERT INTO documents (id, bucket_id, name, content_type, size, storage_key, version, created_by)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			row.id, row.bucketID, row.name, "text/plain", 100, "key/"+row.id, 1, "test")
		if err != nil {
			t.Fatalf("insert document %s: %v", row.id, err)
		}
	}

	// Insert 1 document in b3 (in p2) — must NOT be counted for p1.
	_, err := sqlDB.ExecContext(ctx,
		`INSERT INTO documents (id, bucket_id, name, content_type, size, storage_key, version, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"doc-4", b3.ID, "file4.txt", "text/plain", 100, "key/doc-4", 1, "test")
	if err != nil {
		t.Fatalf("insert document doc-4: %v", err)
	}

	n, err := s.CountDocumentsInProject(ctx, p1.ID)
	if err != nil {
		t.Fatalf("CountDocumentsInProject: %v", err)
	}
	if n != 3 {
		t.Errorf("got %d, want 3", n)
	}
}

func TestSumDocumentSizeInBucket_ReturnsZeroForEmptyBucket(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	p := seedProject(t, s, "alpha")
	b := seedBucket(t, s, p.ID, "bkt")

	total, err := s.SumDocumentSizeInBucket(ctx, b.ID)
	if err != nil {
		t.Fatalf("SumDocumentSizeInBucket: %v", err)
	}
	if total != 0 {
		t.Errorf("got %d, want 0", total)
	}
}

func TestSumDocumentSizeInBucket_ReturnsTotalSize(t *testing.T) {
	ctx := context.Background()
	s, sqlDB := newTestStore(t)
	p := seedProject(t, s, "alpha")
	b := seedBucket(t, s, p.ID, "bkt")

	// Insert two documents with known sizes.
	for _, row := range []struct {
		id   string
		name string
		size int
	}{
		{"doc-1", "file1.txt", 1000},
		{"doc-2", "file2.txt", 500},
	} {
		_, err := sqlDB.ExecContext(ctx,
			`INSERT INTO documents (id, bucket_id, name, content_type, size, storage_key, version, created_by)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			row.id, b.ID, row.name, "text/plain", row.size, "key/"+row.id, 1, "test")
		if err != nil {
			t.Fatalf("insert document %s: %v", row.id, err)
		}
	}

	total, err := s.SumDocumentSizeInBucket(ctx, b.ID)
	if err != nil {
		t.Fatalf("SumDocumentSizeInBucket: %v", err)
	}
	if total != 1500 {
		t.Errorf("got %d, want 1500", total)
	}
}
