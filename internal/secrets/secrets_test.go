package secrets

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEnvSecretStore(t *testing.T) {
	ctx := context.Background()
	store := NewEnvSecretStore("TEST_UAP_")

	os.Setenv("TEST_UAP_MY_SECRET", "super-secret-value")
	defer os.Unsetenv("TEST_UAP_MY_SECRET")

	val, err := store.GetSecret(ctx, "MY_SECRET")
	if err != nil {
		t.Fatalf("expected to get secret, got error: %v", err)
	}
	if val != "super-secret-value" {
		t.Fatalf("expected 'super-secret-value', got '%s'", val)
	}

	if !store.HasSecret(ctx, "MY_SECRET") {
		t.Fatalf("expected HasSecret to return true")
	}

	_, err = store.GetSecret(ctx, "NON_EXISTENT")
	if err != ErrSecretNotFound {
		t.Fatalf("expected ErrSecretNotFound, got %v", err)
	}

	if store.HasSecret(ctx, "NON_EXISTENT") {
		t.Fatalf("expected HasSecret to return false")
	}

	_ = store.SetSecret(ctx, "DYNAMIC_KEY", "dynamic-value")
	defer os.Unsetenv("TEST_UAP_DYNAMIC_KEY")

	dynVal, err := store.GetSecret(ctx, "DYNAMIC_KEY")
	if err != nil || dynVal != "dynamic-value" {
		t.Fatalf("expected 'dynamic-value', got '%s', err: %v", dynVal, err)
	}
}

func TestMemorySecretStore(t *testing.T) {
	ctx := context.Background()
	store := NewMemorySecretStore()

	_ = store.SetSecret(ctx, "API_KEY_1", "val-123")

	val, err := store.GetSecret(ctx, "API_KEY_1")
	if err != nil || val != "val-123" {
		t.Fatalf("expected 'val-123', got '%s', err: %v", val, err)
	}

	if !store.HasSecret(ctx, "API_KEY_1") {
		t.Fatalf("expected HasSecret to return true")
	}

	_, err = store.GetSecret(ctx, "API_KEY_MISSING")
	if err != ErrSecretNotFound {
		t.Fatalf("expected ErrSecretNotFound, got %v", err)
	}

	if store.HasSecret(ctx, "API_KEY_MISSING") {
		t.Fatalf("expected HasSecret to return false")
	}
}

func TestFileSecretStore(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	fileStore, err := NewFileSecretStore(tempDir)
	if err != nil {
		t.Fatalf("failed to create FileSecretStore: %v", err)
	}

	// Create test secret file
	secretPath := filepath.Join(tempDir, "OPENWEATHER_API_KEY")
	if err := os.WriteFile(secretPath, []byte("test-ow-key\n"), 0600); err != nil {
		t.Fatalf("failed to write secret file: %v", err)
	}

	val, err := fileStore.GetSecret(ctx, "OPENWEATHER_API_KEY")
	if err != nil {
		t.Fatalf("expected secret, got err: %v", err)
	}
	if val != "test-ow-key" {
		t.Errorf("expected 'test-ow-key', got '%s'", val)
	}

	if !fileStore.HasSecret(ctx, "OPENWEATHER_API_KEY") {
		t.Errorf("expected HasSecret to return true")
	}

	// Traversal attempt
	if _, err := fileStore.GetSecret(ctx, "../escape"); err == nil {
		t.Errorf("expected directory traversal error")
	}

	// Dynamic set
	if err := fileStore.SetSecret(ctx, "DYNAMIC_KEY", "dyn-file-val"); err != nil {
		t.Fatalf("failed to set secret: %v", err)
	}
	val2, _ := fileStore.GetSecret(ctx, "DYNAMIC_KEY")
	if val2 != "dyn-file-val" {
		t.Errorf("expected 'dyn-file-val', got '%s'", val2)
	}
}

func TestChainedSecretStore(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	fileStore, _ := NewFileSecretStore(tempDir)
	envStore := NewEnvSecretStore("CHAIN_TEST_")
	memStore := NewMemorySecretStore()

	_ = fileStore.SetSecret(ctx, "FILE_KEY", "from-file")
	os.Setenv("CHAIN_TEST_ENV_KEY", "from-env")
	defer os.Unsetenv("CHAIN_TEST_ENV_KEY")
	_ = memStore.SetSecret(ctx, "MEM_KEY", "from-memory")

	// Chained store
	chain := NewChainedSecretStore(fileStore, envStore, memStore)

	// File check
	v1, err := chain.GetSecret(ctx, "FILE_KEY")
	if err != nil || v1 != "from-file" {
		t.Errorf("expected 'from-file', got '%s', err: %v", v1, err)
	}

	// Env check
	v2, err := chain.GetSecret(ctx, "ENV_KEY")
	if err != nil || v2 != "from-env" {
		t.Errorf("expected 'from-env', got '%s', err: %v", v2, err)
	}

	// Memory check
	v3, err := chain.GetSecret(ctx, "MEM_KEY")
	if err != nil || v3 != "from-memory" {
		t.Errorf("expected 'from-memory', got '%s', err: %v", v3, err)
	}

	// Missing
	if _, err := chain.GetSecret(ctx, "UNKNOWN_KEY"); err != ErrSecretNotFound {
		t.Errorf("expected ErrSecretNotFound, got %v", err)
	}
}
