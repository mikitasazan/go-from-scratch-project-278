package store_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"code/internal/store"
)

// TestQueriesAgainstPostgres runs the generated queries against a real
// database. It is skipped when TEST_DATABASE_URL is unset, so `go test ./...`
// stays runnable without one.
func TestQueriesAgainstPostgres(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("cannot open the database: %v", err)
	}

	defer pool.Close()

	if _, err := pool.Exec(ctx, "TRUNCATE links RESTART IDENTITY"); err != nil {
		t.Fatalf("cannot clean the table: %v", err)
	}

	queries := store.New(pool)

	created, err := queries.CreateLink(ctx, store.CreateLinkParams{
		OriginalUrl: "https://example.com/long-url",
		ShortName:   "exmpl",
	})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	if created.CreatedAt.Time.IsZero() {
		t.Fatal("created_at was not filled by the database")
	}

	got, err := queries.GetLink(ctx, created.ID)
	if err != nil || got.ShortName != "exmpl" {
		t.Fatalf("GetLink: %v, %+v", err, got)
	}

	byName, err := queries.GetLinkByShortName(ctx, "exmpl")
	if err != nil || byName.ID != created.ID {
		t.Fatalf("GetLinkByShortName: %v, %+v", err, byName)
	}

	updated, err := queries.UpdateLink(ctx, store.UpdateLinkParams{
		ID:          created.ID,
		OriginalUrl: "https://example.com/changed",
		ShortName:   "chngd",
	})
	if err != nil || updated.ShortName != "chngd" {
		t.Fatalf("UpdateLink: %v, %+v", err, updated)
	}

	links, err := queries.ListLinks(ctx)
	if err != nil || len(links) != 1 {
		t.Fatalf("ListLinks: %v, %d rows", err, len(links))
	}

	affected, err := queries.DeleteLink(ctx, created.ID)
	if err != nil || affected != 1 {
		t.Fatalf("DeleteLink: %v, %d rows", err, affected)
	}

	if _, err := queries.GetLink(ctx, created.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("GetLink after delete = %v, want pgx.ErrNoRows", err)
	}
}
