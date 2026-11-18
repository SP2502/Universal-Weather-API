package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type PostgresOptions struct {
	DriverName      string        // e.g. "postgres", "pgx", or custom SQL driver
	DSN             string        // PostgreSQL connection string
	MaxOpenConns    int           // default 25
	MaxIdleConns    int           // default 5
	ConnMaxLifetime time.Duration // default 5m
	AutoMigrate     bool          // whether to run schema migrations on init
}

func DefaultPostgresOptions(dsn string) PostgresOptions {
	return PostgresOptions{
		DriverName:      "postgres",
		DSN:             dsn,
		MaxOpenConns:    25,
		MaxIdleConns:    5,
		ConnMaxLifetime: 5 * time.Minute,
		AutoMigrate:     true,
	}
}

type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore creates a PostgresStore from an existing *sql.DB connection or creates one with options.
func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

// OpenPostgres initializes a Postgres connection and runs migrations if requested.
func OpenPostgres(opts PostgresOptions) (*PostgresStore, error) {
	if opts.DSN == "" {
		return nil, errors.New("postgres: DSN cannot be empty")
	}
	if opts.DriverName == "" {
		opts.DriverName = "postgres"
	}
	if opts.MaxOpenConns <= 0 {
		opts.MaxOpenConns = 25
	}
	if opts.MaxIdleConns <= 0 {
		opts.MaxIdleConns = 5
	}
	if opts.ConnMaxLifetime <= 0 {
		opts.ConnMaxLifetime = 5 * time.Minute
	}

	var db *sql.DB
	var err error
	if opts.DriverName == "postgres" {
		db = sql.OpenDB(boundedConnector{dsn: opts.DSN})
	} else {
		db, err = sql.Open(opts.DriverName, opts.DSN)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to open connection: %w", err)
	}

	db.SetMaxOpenConns(opts.MaxOpenConns)
	db.SetMaxIdleConns(opts.MaxIdleConns)
	db.SetConnMaxLifetime(opts.ConnMaxLifetime)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("postgres: ping failed: %w", err)
	}

	store := &PostgresStore{db: db}

	if opts.AutoMigrate {
		migrationCtx, migrationCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer migrationCancel()
		if err := store.Migrate(migrationCtx); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("postgres: schema migration failed: %w", err)
		}
	}

	return store, nil
}

// Migrate executes the embedded, versioned SQL migrations in one transaction.
// This also upgrades installations originally initialized with the legacy
// docker-entrypoint script: the base DDL is idempotent and 000002 adds columns.
func (p *PostgresStore) Migrate(ctx context.Context) error {
	return applyMigrations(ctx, p.db)
}

func (p *PostgresStore) GetAPIKeyByHash(ctx context.Context, keyHash string) (*APIKey, error) {
	query := `SELECT id, name, key_prefix, key_hash, COALESCE(secondary_key_hash, ''), rotation_deadline,
	                 scopes, is_active, created_at, last_used_at, expires_at
	          FROM api_keys
	          WHERE key_hash = $1 OR secondary_key_hash = $1`

	var k APIKey
	var secondaryHash string
	var scopesStr string

	row := p.db.QueryRowContext(ctx, query, keyHash)
	err := row.Scan(
		&k.ID,
		&k.Name,
		&k.KeyPrefix,
		&k.KeyHash,
		&secondaryHash,
		&k.RotationDeadline,
		&scopesStr,
		&k.IsActive,
		&k.CreatedAt,
		&k.LastUsedAt,
		&k.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get api key by hash failed: %w", err)
	}

	k.SecondaryKeyHash = secondaryHash
	k.Scopes = parseScopesArray(scopesStr)

	if !k.IsActive {
		return nil, ErrKeyRevoked
	}
	if k.ExpiresAt != nil && time.Now().After(*k.ExpiresAt) {
		return nil, ErrKeyExpired
	}
	if k.SecondaryKeyHash == keyHash && k.RotationDeadline != nil && time.Now().After(*k.RotationDeadline) {
		return nil, ErrKeyExpired
	}

	return &k, nil
}

func (p *PostgresStore) GetAPIKeyByID(ctx context.Context, id string) (*APIKey, error) {
	query := `SELECT id, name, key_prefix, key_hash, COALESCE(secondary_key_hash, ''), rotation_deadline,
	                 scopes, is_active, created_at, last_used_at, expires_at
	          FROM api_keys
	          WHERE id = $1`

	var k APIKey
	var secondaryHash string
	var scopesStr string

	row := p.db.QueryRowContext(ctx, query, id)
	err := row.Scan(
		&k.ID,
		&k.Name,
		&k.KeyPrefix,
		&k.KeyHash,
		&secondaryHash,
		&k.RotationDeadline,
		&scopesStr,
		&k.IsActive,
		&k.CreatedAt,
		&k.LastUsedAt,
		&k.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get api key by id failed: %w", err)
	}

	k.SecondaryKeyHash = secondaryHash
	k.Scopes = parseScopesArray(scopesStr)

	return &k, nil
}

func (p *PostgresStore) CreateAPIKey(ctx context.Context, key *APIKey) error {
	query := `INSERT INTO api_keys (id, name, key_prefix, key_hash, secondary_key_hash, rotation_deadline, scopes, is_active, created_at, expires_at)
	          VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	createdAt := key.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	scopesFormatted := formatScopesArray(key.Scopes)

	var secondaryHash sql.NullString
	if key.SecondaryKeyHash != "" {
		secondaryHash = sql.NullString{String: key.SecondaryKeyHash, Valid: true}
	}

	_, err := p.db.ExecContext(ctx, query,
		key.ID,
		key.Name,
		key.KeyPrefix,
		key.KeyHash,
		secondaryHash,
		key.RotationDeadline,
		scopesFormatted,
		key.IsActive,
		createdAt,
		key.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("postgres: create api key failed: %w", err)
	}

	return nil
}

func (p *PostgresStore) RevokeAPIKey(ctx context.Context, id string) error {
	query := `UPDATE api_keys SET is_active = FALSE WHERE id = $1`
	res, err := p.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("postgres: revoke api key failed: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}

	return nil
}

func (p *PostgresStore) RotateAPIKey(ctx context.Context, id string, newKeyHash string, gracePeriod time.Duration) error {
	k, err := p.GetAPIKeyByID(ctx, id)
	if err != nil {
		return err
	}

	if gracePeriod > 0 {
		deadline := time.Now().Add(gracePeriod)
		query := `UPDATE api_keys
		          SET key_hash = $1, secondary_key_hash = $2, rotation_deadline = $3
		          WHERE id = $4`
		_, err = p.db.ExecContext(ctx, query, newKeyHash, k.KeyHash, deadline, id)
	} else {
		query := `UPDATE api_keys
		          SET key_hash = $1, secondary_key_hash = NULL, rotation_deadline = NULL
		          WHERE id = $2`
		_, err = p.db.ExecContext(ctx, query, newKeyHash, id)
	}

	if err != nil {
		return fmt.Errorf("postgres: rotate api key failed: %w", err)
	}

	return nil
}

func (p *PostgresStore) RecordUsage(ctx context.Context, record *UsageRecord) error {
	query := `INSERT INTO usage_records (key_id, capability, provider, status_code, duration_ms, request_id, created_at)
	          VALUES ($1, $2, $3, $4, $5, $6, $7)`

	createdAt := record.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	_, err := p.db.ExecContext(ctx, query,
		record.KeyID,
		record.Capability,
		record.Provider,
		record.StatusCode,
		record.DurationMs,
		record.RequestID,
		createdAt,
	)
	if err != nil {
		return fmt.Errorf("postgres: record usage failed: %w", err)
	}

	return nil
}

func (p *PostgresStore) RecordUsageBatch(ctx context.Context, records []*UsageRecord) error {
	if len(records) == 0 {
		return nil
	}

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("postgres: begin tx for usage batch failed: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO usage_records (key_id, capability, provider, status_code, duration_ms, request_id, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`)
	if err != nil {
		return fmt.Errorf("postgres: prepare usage stmt failed: %w", err)
	}
	defer stmt.Close()

	now := time.Now().UTC()
	for _, r := range records {
		if r == nil {
			continue
		}
		createdAt := r.CreatedAt
		if createdAt.IsZero() {
			createdAt = now
		}
		if _, err := stmt.ExecContext(ctx, r.KeyID, r.Capability, r.Provider, r.StatusCode, r.DurationMs, r.RequestID, createdAt); err != nil {
			return fmt.Errorf("postgres: batch insert record failed: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("postgres: commit usage batch failed: %w", err)
	}

	return nil
}

func (p *PostgresStore) UpdateAPIKeyLastUsed(ctx context.Context, id string, lastUsed time.Time) error {
	query := `UPDATE api_keys SET last_used_at = $1 WHERE id = $2`
	_, err := p.db.ExecContext(ctx, query, lastUsed, id)
	if err != nil {
		return fmt.Errorf("postgres: update last used failed: %w", err)
	}
	return nil
}

func (p *PostgresStore) Ping(ctx context.Context) error {
	if p.db == nil {
		return errors.New("postgres: db connection is nil")
	}
	return p.db.PingContext(ctx)
}

func (p *PostgresStore) Close() error {
	if p.db == nil {
		return nil
	}
	return p.db.Close()
}

func formatScopesArray(scopes []string) string {
	if len(scopes) == 0 {
		return "{}"
	}
	return "{" + strings.Join(scopes, ",") + "}"
}

func parseScopesArray(raw string) []string {
	trimmed := strings.Trim(raw, "{}")
	if trimmed == "" {
		return []string{}
	}
	parts := strings.Split(trimmed, ",")
	var result []string
	for _, p := range parts {
		clean := strings.TrimSpace(p)
		if clean != "" {
			result = append(result, clean)
		}
	}
	return result
}
