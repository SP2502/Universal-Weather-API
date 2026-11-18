package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

var (
	ErrNotFound   = errors.New("record not found")
	ErrKeyRevoked = errors.New("api key is revoked")
	ErrKeyExpired = errors.New("api key has expired")
)

type APIKey struct {
	ID               string
	Name             string
	KeyPrefix        string
	KeyHash          string
	SecondaryKeyHash string     // Active during rotation grace period
	RotationDeadline *time.Time // Deadline after which secondary key is no longer valid or replaces primary
	Scopes           []string
	IsActive         bool
	CreatedAt        time.Time
	LastUsedAt       *time.Time
	ExpiresAt        *time.Time
}

func (k *APIKey) HasScope(scope string) bool {
	for _, s := range k.Scopes {
		if s == "*" || s == scope {
			return true
		}
	}
	return false
}

type UsageRecord struct {
	KeyID      string
	Capability string
	Provider   string
	StatusCode int
	DurationMs int
	RequestID  string
	CreatedAt  time.Time
}

type Store interface {
	GetAPIKeyByHash(ctx context.Context, keyHash string) (*APIKey, error)
	GetAPIKeyByID(ctx context.Context, id string) (*APIKey, error)
	CreateAPIKey(ctx context.Context, key *APIKey) error
	RevokeAPIKey(ctx context.Context, id string) error
	RotateAPIKey(ctx context.Context, id string, newKeyHash string, gracePeriod time.Duration) error
	RecordUsage(ctx context.Context, record *UsageRecord) error
	RecordUsageBatch(ctx context.Context, records []*UsageRecord) error
	UpdateAPIKeyLastUsed(ctx context.Context, id string, lastUsed time.Time) error
	Ping(ctx context.Context) error
	Close() error
}

func HashKey(plaintextKey string) string {
	sum := sha256.Sum256([]byte(plaintextKey))
	return hex.EncodeToString(sum[:])
}
