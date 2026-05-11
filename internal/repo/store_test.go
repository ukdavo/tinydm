package repo_test

import (
	"context"
	"path/filepath"
	"testing"

	"tinydm/internal/db"
	"tinydm/internal/repo"
)

func newTestStore(t *testing.T) *repo.Store {
	t.Helper()
	sqlDB, err := db.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	return repo.NewStore(sqlDB)
}

func seedTenant(t *testing.T, s *repo.Store, name string) *repo.Tenant {
	t.Helper()
	tenant, err := s.CreateTenant(context.Background(), name, "")
	if err != nil {
		t.Fatalf("CreateTenant %q: %v", name, err)
	}
	return tenant
}

func seedProject(t *testing.T, s *repo.Store, tenantID, name string) *repo.Project {
	t.Helper()
	p, err := s.CreateProject(context.Background(), tenantID, name, "")
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
	s := newTestStore(t)
	tenant := seedTenant(t, s, "acme")
	p1 := seedProject(t, s, tenant.ID, "alpha")
	p2 := seedProject(t, s, tenant.ID, "beta")
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

func TestCountDocumentsInProject_ReturnsCorrectCount(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	tenant := seedTenant(t, s, "acme")
	p1 := seedProject(t, s, tenant.ID, "alpha")
	p2 := seedProject(t, s, tenant.ID, "beta")
	b1 := seedBucket(t, s, p1.ID, "bkt1")
	b2 := seedBucket(t, s, p1.ID, "bkt2")
	b3 := seedBucket(t, s, p2.ID, "bkt3")

	n, err := s.CountDocumentsInProject(ctx, p1.ID)
	if err != nil {
		t.Fatalf("CountDocumentsInProject: %v", err)
	}
	if n != 0 {
		t.Errorf("empty project: got %d, want 0", n)
	}
	_ = b1
	_ = b2
	_ = b3
}

func TestSumDocumentSizeInBucket_ReturnsZeroForEmptyBucket(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	tenant := seedTenant(t, s, "acme")
	p := seedProject(t, s, tenant.ID, "alpha")
	b := seedBucket(t, s, p.ID, "bkt")

	total, err := s.SumDocumentSizeInBucket(ctx, b.ID)
	if err != nil {
		t.Fatalf("SumDocumentSizeInBucket: %v", err)
	}
	if total != 0 {
		t.Errorf("got %d, want 0", total)
	}
}
