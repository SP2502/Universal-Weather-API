package database

import (
	"io/fs"
	"strings"
	"testing"
)

func TestEmbeddedMigrationSequence(t *testing.T) {
	names, err := fs.Glob(migrationFiles, "schema/*.sql")
	if err != nil || len(names) != 2 {
		t.Fatalf("expected versioned base and upgrade migrations: %v %v", names, err)
	}
	for _, name := range names {
		b, err := migrationFiles.ReadFile(name)
		if err != nil || len(b) == 0 {
			t.Fatalf("missing migration %s: %v", name, err)
		}
	}
	upgrade, _ := migrationFiles.ReadFile(names[1])
	for _, needle := range []string{"ADD COLUMN IF NOT EXISTS secondary_key_hash", "ADD COLUMN IF NOT EXISTS rotation_deadline", "ALTER COLUMN key_prefix TYPE VARCHAR(64)"} {
		if !strings.Contains(string(upgrade), needle) {
			t.Fatalf("missing upgrade requirement %s", needle)
		}
	}
}
