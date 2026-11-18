package cache

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound      = errors.New("cache: key not found")
	ErrExpired       = errors.New("cache: entry expired")
	ErrStoreClosed   = errors.New("cache: store is closed")
	ErrNilValue      = errors.New("cache: value is nil")
	ErrInvalidKey    = errors.New("cache: invalid or empty key")
	ErrValueTooLarge = errors.New("cache: value exceeds maximum allowed size")
)

// EntryState defines the freshness state of a cached entry.
type EntryState int

const (
	StateNotFound EntryState = iota
	StateFresh
	StateStale
	StateExpired
)

func (s EntryState) String() string {
	switch s {
	case StateFresh:
		return "FRESH"
	case StateStale:
		return "STALE"
	case StateExpired:
		return "EXPIRED"
	default:
		return "NOT_FOUND"
	}
}

// Entry encapsulates a cached value and its freshness boundaries.
type Entry struct {
	Value      []byte
	State      EntryState
	FreshUntil time.Time
	StaleUntil time.Time
}

// Store defines the pluggable, concurrency-safe infrastructure cache interface.
type Store interface {
	// Get retrieves the cached binary payload if it is FRESH. Returns (data, found, error).
	Get(ctx context.Context, key string) ([]byte, bool, error)

	// GetEntry retrieves the cached entry and its freshness state (Fresh, Stale, Expired, NotFound).
	GetEntry(ctx context.Context, key string) (*Entry, error)

	// Set stores a binary payload with a simple TTL (freshUntil = now + ttl, staleUntil = now + ttl).
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error

	// SetWithStale stores a payload with explicit fresh and stale-servable lifetimes.
	SetWithStale(ctx context.Context, key string, value []byte, freshTTL time.Duration, staleTTL time.Duration) error

	// Delete explicitly removes an entry from the cache.
	Delete(ctx context.Context, key string) error

	// Len returns the current number of active (non-evicted) entries.
	Len() int

	// Close shuts down the cache and releases associated resources.
	Close() error
}
