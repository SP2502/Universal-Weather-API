package database

// PostgreSQL's database/sql driver must be linked into the final binary.
// This import is intentionally isolated from the storage implementation.
import _ "github.com/lib/pq"
