package main

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	ctx := context.Background()
	pool, err := newPool(ctx)
	if err != nil {
		t.Fatalf("newPool: %v", err)
	}
	truncateAll(ctx, t, pool)
	t.Cleanup(func() {
		truncateAll(ctx, t, pool)
		pool.Close()
	})
	return pool
}

func truncateAll(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, "TRUNCATE orgs, users, memberships, sessions, projects, tasks CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func TestNewIDReturnsUnique32CharHexStrings(t *testing.T) {
	a, b := newID(), newID()
	if len(a) != 32 || len(b) != 32 {
		t.Fatalf("expected 32-char IDs, got %d and %d chars", len(a), len(b))
	}
	if a == b {
		t.Fatalf("expected unique IDs, got two matching: %q", a)
	}
}

func TestSchemaBootstrapsAndAcceptsAnOrgRow(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()

	id := newID()
	if _, err := pool.Exec(ctx, "INSERT INTO orgs (id, name) VALUES ($1, $2)", id, "Acme"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	var name string
	if err := pool.QueryRow(ctx, "SELECT name FROM orgs WHERE id = $1", id).Scan(&name); err != nil {
		t.Fatalf("select: %v", err)
	}
	if name != "Acme" {
		t.Fatalf("expected name %q, got %q", "Acme", name)
	}
}
