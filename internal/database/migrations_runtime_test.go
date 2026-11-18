package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
)

// SQL-driver simulation tests the migration transaction and ordering without
// requiring a live database. It is not a PostgreSQL syntax/integration test.
type migrationRecorder struct {
	statements   []string
	failContains string
	committed    bool
	rolledBack   bool
}
type recordingConnector struct{ r *migrationRecorder }

func (c recordingConnector) Connect(context.Context) (driver.Conn, error) {
	return recordingConn(c), nil
}
func (c recordingConnector) Driver() driver.Driver { return recordingDriver(c) }

type recordingDriver struct{ r *migrationRecorder }

func (d recordingDriver) Open(string) (driver.Conn, error) { return recordingConn(d), nil }

type recordingConn struct{ r *migrationRecorder }

func (c recordingConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not implemented")
}
func (c recordingConn) Close() error              { return nil }
func (c recordingConn) Begin() (driver.Tx, error) { return recordingTx(c), nil }
func (c recordingConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return recordingTx(c), nil
}
func (c recordingConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	c.r.statements = append(c.r.statements, q)
	if c.r.failContains != "" && strings.Contains(q, c.r.failContains) {
		return nil, errors.New("simulated SQL failure")
	}
	return driver.RowsAffected(1), nil
}
func (c recordingConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if !strings.Contains(q, "SELECT EXISTS") {
		return nil, errors.New("unexpected query")
	}
	return &existRows{notRead: true}, nil
}

type existRows struct{ notRead bool }

func (*existRows) Columns() []string { return []string{"exists"} }
func (*existRows) Close() error      { return nil }
func (r *existRows) Next(dest []driver.Value) error {
	if !r.notRead {
		return io.EOF
	}
	r.notRead = false
	dest[0] = false
	return nil
}

type recordingTx struct{ r *migrationRecorder }

func (t recordingTx) Commit() error   { t.r.committed = true; return nil }
func (t recordingTx) Rollback() error { t.r.rolledBack = true; return nil }
func TestVersionedMigrationTransaction(t *testing.T) {
	for _, fail := range []string{"", "ADD COLUMN IF NOT EXISTS secondary_key_hash"} {
		t.Run("failOn="+fail, func(t *testing.T) {
			r := &migrationRecorder{failContains: fail}
			db := sql.OpenDB(recordingConnector{r})
			defer db.Close()
			err := applyMigrations(context.Background(), db)
			if fail != "" {
				if err == nil || r.committed || !r.rolledBack {
					t.Fatalf("expected rollback: err=%v committed=%v rollback=%v", err, r.committed, r.rolledBack)
				}
				return
			}
			if err != nil || !r.committed {
				t.Fatalf("migration failed: %v committed=%v", err, r.committed)
			}
			applied := 0
			for _, s := range r.statements {
				if strings.HasPrefix(s, "INSERT INTO schema_migrations") {
					applied++
				}
			}
			if applied != 2 {
				t.Fatalf("expected two version records, got %d", applied)
			}
		})
	}
}
