package auth

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/universal-api/platform/internal/database"
)

func TestPhase08BLastUsedCoalescesBoundedlyAndDrains(t *testing.T) {
	db := database.NewMemoryStore()
	if err := db.CreateAPIKey(context.Background(), &database.APIKey{ID: "real-key", IsActive: true, KeyHash: database.HashKey("synthetic")}); err != nil {
		t.Fatal(err)
	}
	w := NewLastUsedWriter(db, time.Hour, 3)
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); w.Record("real-key") }()
	}
	wg.Wait()
	pending, dropped := w.Stats()
	if pending != 1 || dropped != 0 {
		t.Fatalf("coalesce pending=%d dropped=%d", pending, dropped)
	}
	for i := 0; i < 10; i++ {
		w.Record(fmt.Sprintf("different-key-%d", i))
	}
	pending, dropped = w.Stats()
	if pending != 3 || dropped != 8 {
		t.Fatalf("bounded pending=%d dropped=%d", pending, dropped)
	}
	if err := w.Close(); err == nil {
		t.Fatal("expected missing test IDs to report flush errors")
	}
	if err := w.Close(); err == nil {
		t.Fatal("close should return same saved error")
	}
	result, err := db.GetAPIKeyByID(context.Background(), "real-key")
	if err != nil || result.LastUsedAt == nil {
		t.Fatalf("expected drained timestamp, err=%v", err)
	}
	w.Record("after-close")
	if pending, _ := w.Stats(); pending != 0 {
		t.Fatalf("closed writer accepted new events: %d", pending)
	}
}

func TestPhase08BAuthenticationStillChecksRevocation(t *testing.T) {
	db := database.NewMemoryStore()
	key := "synthetic-secret-123"
	_ = db.CreateAPIKey(context.Background(), &database.APIKey{ID: "id", IsActive: true, KeyHash: database.HashKey(key)})
	auth := NewAuthenticator(db)
	if _, err := auth.AuthenticateKey(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeAPIKey(context.Background(), "id"); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.AuthenticateKey(context.Background(), key); err != database.ErrKeyRevoked {
		t.Fatalf("revocation bypassed: %v", err)
	}
}
