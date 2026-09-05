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

func TestMigrations(t *testing.T) {
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
	assertConnectorAuditActor(t, database)
	testIdempotency(t, ctx, databaseURL, database)
	testRemoteAccessGovernanceConstraints(t, database)
	assertRemoteAccessPolicyRemoved(t, database)
	assertRemoteAccessGovernanceHasNoPhysicalDelete(t, database)
	assertExplicitDataAuthorizationTable(t, database)
	assertRoleBindingSubjectRoleUnique(t, database)
	assertBastionRootLifecycle(t, database)

	if err := arguspostgres.RunMigrations(ctx, databaseURL, directory, arguspostgres.MigrationDown); err != nil {
		t.Fatalf("down: %v", err)
	}
	if err := arguspostgres.RunMigrations(ctx, databaseURL, directory, arguspostgres.MigrationUp); err != nil {
		t.Fatalf("up after down: %v", err)
	}
}

func assertBastionRootLifecycle(t *testing.T, database *sql.DB) {
	t.Helper()
	tx, err := database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	var enterpriseID string
	if err = tx.QueryRow(`
		INSERT INTO enterprises (id, name, code, timezone)
		VALUES (gen_random_uuid(), 'Bastion lifecycle test', 'bastion-lifecycle-test', 'UTC')
		RETURNING id::text
	`).Scan(&enterpriseID); err != nil {
		t.Fatal(err)
	}
	createScope := func() string {
		t.Helper()
		var scopeID string
		if scanErr := tx.QueryRow(`
			INSERT INTO bastion_scopes (id, enterprise_id, name, environment, labels_hash, onboarding_mode)
			VALUES (gen_random_uuid(), $1::uuid, 'Reusable bastion', 'production', decode(repeat('00', 32), 'hex'), 'command')
			RETURNING id::text
		`, enterpriseID).Scan(&scopeID); scanErr != nil {
			t.Fatal(scanErr)
		}
		return scopeID
	}
	createRoot := func(scopeID, name string) string {
		t.Helper()
		var hostID string
		if scanErr := tx.QueryRow(`
			INSERT INTO hosts (id, enterprise_id, name, address, port, platform, connection_mode, bastion_scope_id, environment, labels_hash)
			VALUES (gen_random_uuid(), $1::uuid, $2, 'connector://pending', 1, 'linux', 'connector_local', $3::uuid, 'production', decode(repeat('00', 32), 'hex'))
			RETURNING id::text
		`, enterpriseID, name, scopeID).Scan(&hostID); scanErr != nil {
			t.Fatal(scanErr)
		}
		return hostID
	}

	firstScopeID := createScope()
	firstHostID := createRoot(firstScopeID, "Reusable bastion")
	if _, err = tx.Exec(`UPDATE bastion_scopes SET connector_host_id=$1::uuid WHERE id=$2::uuid`, firstHostID, firstScopeID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`UPDATE hosts SET status='deleted', deleted_at=now() WHERE id=$1::uuid`, firstHostID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`UPDATE bastion_scopes SET status='deleted', deleted_at=now() WHERE id=$1::uuid`, firstScopeID); err != nil {
		t.Fatal(err)
	}

	secondScopeID := createScope()
	createRoot(secondScopeID, "Reusable bastion")
	if _, err = tx.Exec(`
		INSERT INTO hosts (id, enterprise_id, name, address, port, platform, connection_mode, bastion_scope_id, environment, labels_hash)
		VALUES (gen_random_uuid(), $1::uuid, 'Second live root', 'connector://duplicate', 1, 'linux', 'connector_local', $2::uuid, 'production', decode(repeat('00', 32), 'hex'))
	`, enterpriseID, secondScopeID); err == nil {
		t.Fatal("a scope accepted a second live connector_local root host")
	}
}

func assertRoleBindingSubjectRoleUnique(t *testing.T, database *sql.DB) {
	t.Helper()
	var unique bool
	if err := database.QueryRow(`
		SELECT index.indisunique
		FROM pg_index index
		JOIN pg_class relation ON relation.oid = index.indexrelid
		WHERE relation.relname = 'role_bindings_subject_role_unique'
	`).Scan(&unique); err != nil {
		t.Fatalf("role binding subject-role index missing: %v", err)
	}
	if !unique {
		t.Fatal("role binding subject-role index must be unique")
	}
}

func assertRemoteAccessGovernanceHasNoPhysicalDelete(t *testing.T, database *sql.DB) {
	t.Helper()
	for _, table := range []string{
		"remote_access_grants",
		"remote_access_rules",
		"remote_access_approval_workflows",
		"remote_access_session_profiles",
	} {
		var triggerCount int
		if err := database.QueryRow(`SELECT count(*) FROM pg_trigger trigger JOIN pg_class relation ON relation.oid=trigger.tgrelid WHERE relation.relname=$1 AND NOT trigger.tgisinternal AND (trigger.tgtype & 8) = 8`, table).Scan(&triggerCount); err != nil {
			t.Fatal(err)
		}
		if triggerCount == 0 {
			t.Fatalf("%s must reject physical deletion", table)
		}
	}
}

func assertExplicitDataAuthorizationTable(t *testing.T, database *sql.DB) {
	t.Helper()
	var tableName sql.NullString
	if err := database.QueryRow("SELECT to_regclass('public.data_authorization_grants')::text").Scan(&tableName); err != nil {
		t.Fatal(err)
	}
	if !tableName.Valid {
		t.Fatal("explicit data authorization table is missing")
	}
	var columnCount int
	if err := database.QueryRow("SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='data_authorization_grants' AND column_name IN ('subject_type','subject_id','resource_type','resource_id','version')").Scan(&columnCount); err != nil {
		t.Fatal(err)
	}
	if columnCount != 5 {
		t.Fatalf("explicit authorization columns missing: %d", columnCount)
	}
}

func assertRemoteAccessPolicyRemoved(t *testing.T, database *sql.DB) {
	t.Helper()
	var tableName sql.NullString
	if err := database.QueryRow(`SELECT to_regclass('public.remote_access_policies')::text`).Scan(&tableName); err != nil {
		t.Fatal(err)
	}
	if tableName.Valid {
		t.Fatalf("legacy remote_access_policies table still exists as %q", tableName.String)
	}
	for table, column := range map[string]string{
		"remote_access_requirement_snapshots": "policy_id",
		"remote_access_leases":                "policy_snapshot_hash",
	} {
		var count int
		if err := database.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name=$1 AND column_name=$2`, table, column).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("legacy column %s.%s still exists", table, column)
		}
	}
	var permissionCount int
	if err := database.QueryRow(`SELECT count(*) FROM permissions WHERE id IN ('remote_access.policy.read','remote_access.policy.manage')`).Scan(&permissionCount); err != nil {
		t.Fatal(err)
	}
	if permissionCount != 0 {
		t.Fatalf("legacy remote access policy permissions still exist: %d", permissionCount)
	}
}

func testRemoteAccessGovernanceConstraints(t *testing.T, database *sql.DB) {
	t.Helper()
	tx, err := database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	enterpriseA := "11111111-1111-4111-8111-111111111111"
	enterpriseB := "22222222-2222-4222-8222-222222222222"
	workflowA := "33333333-3333-4333-8333-333333333333"
	workflowB := "44444444-4444-4444-8444-444444444444"
	profileA := "55555555-5555-4555-8555-555555555555"
	profileB := "66666666-6666-4666-8666-666666666666"
	ruleID := "77777777-7777-4777-8777-777777777777"
	createdBy := "88888888-8888-4888-8888-888888888888"

	for _, value := range []struct {
		id, name, code string
	}{
		{enterpriseA, "Governance Constraint A", "governance-constraint-a"},
		{enterpriseB, "Governance Constraint B", "governance-constraint-b"},
	} {
		if _, err := tx.Exec(`INSERT INTO enterprises (id, name, code, timezone) VALUES ($1::uuid, $2, $3, 'UTC')`, value.id, value.name, value.code); err != nil {
			t.Fatal(err)
		}
	}
	insertWorkflow := `INSERT INTO remote_access_approval_workflows
		(id, enterprise_id, name, description, approver_role_ids, minimum_approvals, separation_of_duties,
		 approval_timeout_seconds, timeout_effect, escalation_role_ids, status, created_by)
		VALUES ($1::uuid, $2::uuid, $3, '', ARRAY[gen_random_uuid()], 1, true, 3600, 'expire', '{}', 'draft', $4::uuid)`
	if _, err := tx.Exec(insertWorkflow, workflowA, enterpriseA, "Workflow A", createdBy); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(insertWorkflow, workflowB, enterpriseB, "Workflow B", createdBy); err != nil {
		t.Fatal(err)
	}
	insertProfile := `INSERT INTO remote_access_session_profiles
		(id, enterprise_id, name, description, max_session_seconds, idle_timeout_seconds, recording_mode,
		 command_audit_mode, clipboard_mode, file_upload_mode, file_download_mode, port_forward_mode,
		 session_share_mode, retention_days, status, created_by)
		VALUES ($1::uuid, $2::uuid, $3, '', 3600, 600, 'required', 'required', 'disabled', 'disabled', 'disabled', 'disabled', 'disabled', 90, 'draft', $4::uuid)`
	if _, err := tx.Exec(insertProfile, profileA, enterpriseA, "Profile A", createdBy); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(insertProfile, profileB, enterpriseB, "Profile B", createdBy); err != nil {
		t.Fatal(err)
	}

	insertRule := `INSERT INTO remote_access_rules
		(id, enterprise_id, name, description, priority, protocols, actions, source_cidrs, time_windows,
		 effects, approval_workflow_id, session_profile_id, status, created_by)
		VALUES ($1::uuid, $2::uuid, $3, '', 100, ARRAY['ssh'], ARRAY['terminal'], '{}', '[]', $4, $5::uuid, $6::uuid, 'draft', $7::uuid)`
	if _, err := tx.Exec(insertRule, ruleID, enterpriseA, "Rule A", "{require_approval}", workflowA, profileA, createdBy); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(insertRule, "99999999-9999-4999-8999-999999999999", enterpriseA, "Rule Cross Enterprise", "{require_approval}", workflowB, profileB, createdBy); err == nil {
		t.Fatal("cross-enterprise workflow/profile references must fail")
	}
	if _, err := tx.Exec(insertRule, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", enterpriseA, "Rule Deny Combination", "{deny,notify}", workflowA, profileA, createdBy); err == nil {
		t.Fatal("deny must not be combined with other effects")
	}
	if _, err := tx.Exec(`INSERT INTO remote_access_approval_workflows
			(id, enterprise_id, name, description, approver_role_ids, minimum_approvals, separation_of_duties,
			 approval_timeout_seconds, escalation_after_seconds, timeout_effect, escalation_role_ids, status, created_by)
			VALUES ('bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb'::uuid, $1::uuid, 'workflow a', '', ARRAY[gen_random_uuid()], 1, true, 3600, 900, 'expire', '{}', 'draft', $2::uuid)`, enterpriseA, createdBy); err == nil {
		t.Fatal("governance names must be unique case-insensitively per enterprise")
	}
	if _, err := tx.Exec(`INSERT INTO remote_access_approval_workflows
			(id, enterprise_id, name, description, approver_role_ids, minimum_approvals, separation_of_duties,
			 approval_timeout_seconds, escalation_after_seconds, timeout_effect, escalation_role_ids, status, created_by)
			VALUES ('dddddddd-dddd-4ddd-8ddd-dddddddddddd'::uuid, $1::uuid, 'Invalid escalation', '', ARRAY[gen_random_uuid()], 1, true, 3600, 3600, 'expire', '{}', 'draft', $2::uuid)`, enterpriseA, createdBy); err == nil {
		t.Fatal("escalation threshold must precede approval timeout")
	}
	if _, err := tx.Exec(`INSERT INTO remote_access_session_profiles
		(id, enterprise_id, name, description, max_session_seconds, idle_timeout_seconds, recording_mode,
		 command_audit_mode, clipboard_mode, file_upload_mode, file_download_mode, port_forward_mode,
		 session_share_mode, retention_days, status, created_by)
		VALUES ('cccccccc-cccc-4ccc-8ccc-cccccccccccc'::uuid, $1::uuid, 'Invalid profile', '', 600, 601, 'required', 'required', 'disabled', 'disabled', 'disabled', 'disabled', 'disabled', 90, 'draft', $2::uuid)`, enterpriseA, createdBy); err == nil {
		t.Fatal("idle timeout above max session duration must fail")
	}
}

func assertConnectorAuditActor(t *testing.T, database *sql.DB) {
	t.Helper()
	tx, err := database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for index, actorType := range []string{"connector", "direct_executor"} {
		eventHash := bytes.Repeat([]byte{byte(index + 1)}, 32)
		_, err = tx.Exec(`INSERT INTO audit_events (
			id, domain, enterprise_id, actor_type, actor_id, action, resource_type,
			resource_id, result, details, previous_hash, event_hash
		) VALUES (
			gen_random_uuid(), 'enterprise', gen_random_uuid(), $1, gen_random_uuid()::text,
			'connector.enroll', 'connector', gen_random_uuid()::text, 'success', '{}', $2, $3
		)`, actorType, make([]byte, 32), eventHash)
		if err != nil {
			t.Fatalf("%s audit actor must be accepted after M3 migration: %v", actorType, err)
		}
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
