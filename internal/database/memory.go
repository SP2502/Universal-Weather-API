package database

import (
	"context"
	"sync"
	"time"
)

type MemoryStore struct {
	mu       sync.RWMutex
	keys     map[string]*APIKey // keyHash / secondaryKeyHash -> APIKey
	keysByID map[string]*APIKey // id -> APIKey
	usage    []*UsageRecord
	closed   bool
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		keys:     make(map[string]*APIKey),
		keysByID: make(map[string]*APIKey),
		usage:    make([]*UsageRecord, 0),
	}
}

func (m *MemoryStore) GetAPIKeyByHash(ctx context.Context, keyHash string) (*APIKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, ErrNotFound
	}

	k, exists := m.keys[keyHash]
	if !exists {
		return nil, ErrNotFound
	}
	if !k.IsActive {
		return nil, ErrKeyRevoked
	}
	if k.ExpiresAt != nil && time.Now().After(*k.ExpiresAt) {
		return nil, ErrKeyExpired
	}

	// If matched via secondary key, check whether rotation deadline has passed
	if k.SecondaryKeyHash == keyHash && k.RotationDeadline != nil && time.Now().After(*k.RotationDeadline) {
		return nil, ErrKeyExpired
	}

	// return a copy
	copied := *k
	return &copied, nil
}

func (m *MemoryStore) GetAPIKeyByID(ctx context.Context, id string) (*APIKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, ErrNotFound
	}

	k, exists := m.keysByID[id]
	if !exists {
		return nil, ErrNotFound
	}

	copied := *k
	return &copied, nil
}

func (m *MemoryStore) CreateAPIKey(ctx context.Context, key *APIKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Defensive copy
	copied := *key
	m.keys[copied.KeyHash] = &copied
	if copied.SecondaryKeyHash != "" {
		m.keys[copied.SecondaryKeyHash] = &copied
	}
	m.keysByID[copied.ID] = &copied
	return nil
}

func (m *MemoryStore) RevokeAPIKey(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	k, exists := m.keysByID[id]
	if !exists {
		return ErrNotFound
	}

	k.IsActive = false
	return nil
}

func (m *MemoryStore) RotateAPIKey(ctx context.Context, id string, newKeyHash string, gracePeriod time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	k, exists := m.keysByID[id]
	if !exists {
		return ErrNotFound
	}

	if gracePeriod > 0 {
		// Old primary becomes secondary during grace period
		oldHash := k.KeyHash
		k.SecondaryKeyHash = oldHash
		k.KeyHash = newKeyHash

		deadline := time.Now().Add(gracePeriod)
		k.RotationDeadline = &deadline

		m.keys[newKeyHash] = k
		m.keys[oldHash] = k
	} else {
		// Immediate rotation without grace period
		delete(m.keys, k.KeyHash)
		if k.SecondaryKeyHash != "" {
			delete(m.keys, k.SecondaryKeyHash)
			k.SecondaryKeyHash = ""
		}
		k.KeyHash = newKeyHash
		k.RotationDeadline = nil
		m.keys[newKeyHash] = k
	}

	return nil
}

func (m *MemoryStore) RecordUsage(ctx context.Context, record *UsageRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	m.usage = append(m.usage, record)
	return nil
}

func (m *MemoryStore) RecordUsageBatch(ctx context.Context, records []*UsageRecord) error {
	if len(records) == 0 {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	for _, r := range records {
		if r == nil {
			continue
		}
		if r.CreatedAt.IsZero() {
			r.CreatedAt = now
		}
		m.usage = append(m.usage, r)
	}
	return nil
}

func (m *MemoryStore) UpdateAPIKeyLastUsed(ctx context.Context, id string, lastUsed time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if k, exists := m.keysByID[id]; exists {
		k.LastUsedAt = &lastUsed
		return nil
	}
	return ErrNotFound
}

func (m *MemoryStore) GetUsageCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.usage)
}

func (m *MemoryStore) Ping(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return ErrNotFound
	}
	return nil
}

func (m *MemoryStore) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}
