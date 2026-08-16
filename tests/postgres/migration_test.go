package postgres_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	arguspostgres "github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

func TestM2Migrations(t *testing.T) {
	databaseURL := os.Getenv("ARGUS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ARGUS_TEST_DATABASE_URL is not configured")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "migrations", "postgresql")
	ctx := context.Background()
	if err := arguspostgres.RunMigrations(ctx, databaseURL, directory, arguspostgres.MigrationUp); err != nil {
		t.Fatalf("empty database up: %v", err)
	}
	if err := arguspostgres.RunMigrations(ctx, databaseURL, directory, arguspostgres.MigrationUp); err != nil {
		t.Fatalf("repeat up: %v", err)
	}

	var wait sync.WaitGroup
	errors := make(chan error, 2)
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errors <- arguspostgres.RunMigrations(ctx, databaseURL, directory, arguspostgres.MigrationUp)
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent up: %v", err)
		}
	}

	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	assertConstraint(t, database, `INSERT INTO enterprise_users (id, enterprise_id, department_id, username, display_name) VALUES (gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), 'invalid-user', 'Invalid')`)
	assertConstraint(t, database, `INSERT INTO api_keys (id, enterprise_id, service_account_id, name, prefix, secret_hash, authorization_version) VALUES (gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), 'invalid', 'prefix1', decode(repeat('00', 32), 'hex'), 1)`)
	testIdempotency(t, ctx, databaseURL, database)

	if err := arguspostgres.RunMigrations(ctx, databaseURL, directory, arguspostgres.MigrationDown); err != nil {
		t.Fatalf("down: %v", err)
	}
	if err := arguspostgres.RunMigrations(ctx, databaseURL, directory, arguspostgres.MigrationUp); err != nil {
		t.Fatalf("up after down: %v", err)
	}
}

func testIdempotency(t *testing.T, ctx context.Context, databaseURL string, database *sql.DB) {
	t.Helper()
	store, err := arguspostgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := arguspostgres.Idempotency{Key: bytes.Repeat([]byte{0x7a}, 32)}
	request := map[string]any{"name": "one-time-credential"}
	callbackCalls := 0
	execute := func(input map[string]any) (string, error) {
		return arguspostgres.ExecuteIdempotent(ctx, store, service, "enterprise", "subject-1", "credential.create", "idem-1", input, 201, func(*db.Queries) (string, error) {
			callbackCalls++
			return "one-time-secret", nil
		})
	}
	first, err := execute(request)
	if err != nil || first != "one-time-secret" {
		t.Fatalf("first idempotent execution: value=%q err=%v", first, err)
	}
	replay, err := execute(request)
	if err != nil || replay != first || callbackCalls != 1 {
		t.Fatalf("idempotent replay: value=%q calls=%d err=%v", replay, callbackCalls, err)
	}
	if _, err := execute(map[string]any{"name": "different-request"}); !errors.Is(err, arguspostgres.ErrIdempotencyConflict) {
		t.Fatalf("request mismatch: got %v", err)
	}
	var ciphertext []byte
	if err := database.QueryRow(`SELECT response_ciphertext FROM idempotency_records WHERE audience='enterprise' AND subject_id='subject-1' AND operation='credential.create' AND idempotency_key='idem-1'`).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, []byte(first)) {
		t.Fatal("idempotency ciphertext contains the one-time response in plaintext")
	}
	if _, err := database.Exec(`UPDATE idempotency_records SET expires_at = now() - interval '1 second' WHERE audience='enterprise' AND subject_id='subject-1' AND operation='credential.create' AND idempotency_key='idem-1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(request); !errors.Is(err, arguspostgres.ErrIdempotencyExpired) {
		t.Fatalf("expired replay: got %v", err)
	}
}

func assertConstraint(t *testing.T, database *sql.DB, statement string) {
	t.Helper()
	if _, err := database.Exec(statement); err == nil {
		t.Fatalf("expected constraint failure for %s", statement)
	}
}
