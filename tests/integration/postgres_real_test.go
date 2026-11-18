package integration_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/auth"
	"github.com/universal-api/platform/internal/database"
)

// Set UWA_TEST_POSTGRES_DSN only for a disposable, task-owned PostgreSQL DB.
func TestRealPostgresMigrationsAndKeyLifecycle(t *testing.T) {
	dsn := os.Getenv("UWA_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("UWA_TEST_POSTGRES_DSN is not set")
	}
	store, err := database.OpenPostgres(database.DefaultPostgresOptions(dsn))
	if err != nil {
		t.Fatalf("open and migrate real PostgreSQL: %v", err)
	}
	defer func() { _ = store.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	id := fmt.Sprintf("real-pg-%d", time.Now().UnixNano())
	oldHash := database.HashKey(id + "-old")
	newHash := database.HashKey(id + "-new")
	key := &database.APIKey{ID: id, Name: "real postgres test", KeyPrefix: "test-", KeyHash: oldHash, Scopes: []string{"weather:read", "geocode:read"}, IsActive: true}
	if err := store.CreateAPIKey(ctx, key); err != nil {
		t.Fatalf("create key: %v", err)
	}
	if got, err := store.GetAPIKeyByHash(ctx, oldHash); err != nil || got.ID != id || len(got.Scopes) != 2 {
		t.Fatalf("lookup key: got=%+v err=%v", got, err)
	}
	if err := store.RotateAPIKey(ctx, id, newHash, 2*time.Second); err != nil {
		t.Fatalf("rotate key: %v", err)
	}
	for _, hash := range []string{oldHash, newHash} {
		if _, err := store.GetAPIKeyByHash(ctx, hash); err != nil {
			t.Fatalf("rotation overlap lookup: %v", err)
		}
	}
	rotated, err := store.GetAPIKeyByID(ctx, id)
	if err != nil || rotated.RotationDeadline == nil {
		t.Fatalf("rotation deadline missing: key=%+v err=%v", rotated, err)
	}
	if wait := time.Until(*rotated.RotationDeadline) + 10*time.Millisecond; wait > 0 {
		time.Sleep(wait)
	}
	if _, err := store.GetAPIKeyByHash(ctx, oldHash); !errors.Is(err, database.ErrKeyExpired) {
		t.Fatalf("old hash after rotation deadline: %v", err)
	}
	if _, err := store.GetAPIKeyByHash(ctx, newHash); err != nil {
		t.Fatalf("new hash after rotation deadline: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close first connection: %v", err)
	}
	store, err = database.OpenPostgres(database.DefaultPostgresOptions(dsn))
	if err != nil {
		t.Fatalf("reopen persisted database: %v", err)
	}
	if got, err := store.GetAPIKeyByHash(ctx, newHash); err != nil || got.ID != id {
		t.Fatalf("lookup after reopen: got=%+v err=%v", got, err)
	}
	if err := store.RevokeAPIKey(ctx, id); err != nil {
		t.Fatalf("revoke key: %v", err)
	}
	if _, err := store.GetAPIKeyByHash(ctx, newHash); !errors.Is(err, database.ErrKeyRevoked) {
		t.Fatalf("revoked key lookup: got %v", err)
	}
	expiredID := id + "-expired"
	expiredHash := database.HashKey(expiredID)
	expiry := time.Now().Add(-time.Minute)
	if err := store.CreateAPIKey(ctx, &database.APIKey{ID: expiredID, Name: "expired", KeyPrefix: "test-", KeyHash: expiredHash, IsActive: true, ExpiresAt: &expiry}); err != nil {
		t.Fatalf("create expired key: %v", err)
	}
	if _, err := store.GetAPIKeyByHash(ctx, expiredHash); !errors.Is(err, database.ErrKeyExpired) {
		t.Fatalf("expired key lookup: got %v", err)
	}
}

// The legacy DB must be seeded from migrations/000001_init_schema.up.sql.
func TestRealPostgresLegacyUpgradePreservesKey(t *testing.T) {
	dsn := os.Getenv("UWA_TEST_POSTGRES_LEGACY_DSN")
	if dsn == "" {
		t.Skip("UWA_TEST_POSTGRES_LEGACY_DSN is not set")
	}
	store, err := database.OpenPostgres(database.DefaultPostgresOptions(dsn))
	if err != nil {
		t.Fatalf("upgrade legacy PostgreSQL: %v", err)
	}
	defer func() { _ = store.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	got, err := store.GetAPIKeyByID(ctx, "legacy-seed")
	if err != nil || got.ID != "legacy-seed" || len(got.Scopes) != 1 || got.Scopes[0] != "weather:read" {
		t.Fatalf("legacy key after upgrade: got=%+v err=%v", got, err)
	}
	previousHash := got.KeyHash
	newHash := database.HashKey(fmt.Sprintf("legacy-%d", time.Now().UnixNano()))
	if err := store.RotateAPIKey(ctx, "legacy-seed", newHash, time.Minute); err != nil {
		t.Fatalf("rotate upgraded legacy key: %v", err)
	}
	for _, hash := range []string{previousHash, newHash} {
		if _, err := store.GetAPIKeyByHash(ctx, hash); err != nil {
			t.Fatalf("upgraded legacy hash %s unavailable: %v", hash, err)
		}
	}
}

// Point this at a fresh, disposable database to exercise migration startup contention.
func TestRealPostgresConcurrentMigrations(t *testing.T) {
	dsn := os.Getenv("UWA_TEST_POSTGRES_CONCURRENT_DSN")
	if dsn == "" {
		t.Skip("UWA_TEST_POSTGRES_CONCURRENT_DSN is not set")
	}
	const workers = 8
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			store, err := database.OpenPostgres(database.DefaultPostgresOptions(dsn))
			if err == nil {
				err = store.Close()
			}
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent migration: %v", err)
		}
	}
}

// UWA_TEST_CLI_KEY is the secret emitted by uapctl into an ignored task artifact.
func TestRealPostgresCLIKeyAuthenticates(t *testing.T) {
	dsn, rawKey := os.Getenv("UWA_TEST_POSTGRES_DSN"), os.Getenv("UWA_TEST_CLI_KEY")
	if dsn == "" || rawKey == "" {
		t.Skip("real PostgreSQL DSN and disposable CLI key are required")
	}
	store, err := database.OpenPostgres(database.DefaultPostgresOptions(dsn))
	if err != nil {
		t.Fatalf("open real PostgreSQL: %v", err)
	}
	defer func() { _ = store.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	caller, err := auth.NewAuthenticator(store).AuthenticateKey(ctx, rawKey)
	if err != nil || caller == nil || caller.Name != "UWA disposable acceptance" || len(caller.Scopes) != 2 {
		t.Fatalf("CLI key authentication failed: caller=%+v err=%v", caller, err)
	}
	if _, err := store.GetAPIKeyByHash(ctx, rawKey); !errors.Is(err, database.ErrNotFound) {
		t.Fatalf("plaintext key was accepted as stored hash: %v", err)
	}
}

// This opt-in test pauses only a named disposable UWA PostgreSQL container.
func TestRealPostgresOutageAndPoolRecovery(t *testing.T) {
	dsn, container := os.Getenv("UWA_TEST_POSTGRES_DSN"), os.Getenv("UWA_TEST_POSTGRES_CONTAINER")
	if dsn == "" || container == "" {
		t.Skip("real PostgreSQL DSN and task-owned container name are required")
	}
	if !regexp.MustCompile(`^uwa-audit-pg-[0-9]{8}$`).MatchString(container) {
		t.Fatal("refusing to pause a container outside the disposable UWA test namespace")
	}
	store, err := database.OpenPostgres(database.DefaultPostgresOptions(dsn))
	if err != nil {
		t.Fatalf("open real PostgreSQL: %v", err)
	}
	defer func() { _ = store.Close() }()
	containerCommand := func(action string) error {
		cmdCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		out, err := exec.CommandContext(cmdCtx, "docker", action, container).CombinedOutput()
		if err != nil {
			return fmt.Errorf("docker %s failed: %w (%s)", action, err, out)
		}
		return nil
	}
	if err := containerCommand("pause"); err != nil {
		t.Fatal(err)
	}
	paused := true
	defer func() {
		if paused {
			_ = containerCommand("unpause")
		}
	}()
	outageCtx, outageCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	pingResult := make(chan error, 1)
	go func() { pingResult <- store.Ping(outageCtx) }()
	bounded := true
	select {
	case err = <-pingResult:
	case <-time.After(3 * time.Second):
		bounded = false
	}
	outageCancel()
	if err := containerCommand("unpause"); err != nil {
		t.Fatal(err)
	}
	paused = false
	if !bounded {
		select {
		case <-pingResult:
		case <-time.After(5 * time.Second):
		}
		t.Fatal("PostgreSQL health probe ignored caller deadline")
	}
	if err == nil {
		t.Fatal("paused PostgreSQL unexpectedly answered ping")
	}
	recoveryCtx, recoveryCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer recoveryCancel()
	if err := store.Ping(recoveryCtx); err != nil {
		t.Fatalf("existing PostgreSQL pool failed to recover: %v", err)
	}
	if err := containerCommand("pause"); err != nil {
		t.Fatal(err)
	}
	paused = true
	lookupCtx, lookupCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	lookupResult := make(chan error, 1)
	go func() { _, lookupErr := store.GetAPIKeyByHash(lookupCtx, "missing-hash"); lookupResult <- lookupErr }()
	bounded = true
	select {
	case err = <-lookupResult:
	case <-time.After(3 * time.Second):
		bounded = false
	}
	lookupCancel()
	if err := containerCommand("unpause"); err != nil {
		t.Fatal(err)
	}
	paused = false
	if !bounded {
		select {
		case <-lookupResult:
		case <-time.After(5 * time.Second):
		}
		t.Fatal("PostgreSQL key lookup ignored caller deadline")
	}
	if err == nil {
		t.Fatal("paused PostgreSQL unexpectedly answered key lookup")
	}
	lookupRecoveryCtx, lookupRecoveryCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer lookupRecoveryCancel()
	if _, err := store.GetAPIKeyByHash(lookupRecoveryCtx, "missing-hash"); !errors.Is(err, database.ErrNotFound) {
		t.Fatalf("existing PostgreSQL pool failed to recover for key lookup: %v", err)
	}
}
