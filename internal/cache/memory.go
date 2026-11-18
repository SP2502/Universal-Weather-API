package cache

import (
	"container/list"
	"context"
	"sync"
	"time"
)

const defaultNumShards = 16

type cacheItem struct {
	key        string
	value      []byte
	freshUntil time.Time
	staleUntil time.Time
}

type cacheShard struct {
	mu         sync.Mutex
	items      map[string]*list.Element
	lru        *list.List
	maxEntries int
	evictions  int64
}

func newCacheShard(maxEntries int) *cacheShard {
	return &cacheShard{
		items:      make(map[string]*list.Element),
		lru:        list.New(),
		maxEntries: maxEntries,
	}
}

// MemoryStore is an in-process, thread-safe, sharded LRU memory cache.
type MemoryStore struct {
	shards       []*cacheShard
	numShards    int
	maxValueSize int
	closed       bool
	mu           sync.RWMutex
}

// MemoryStoreOptions configuration options for creating a MemoryStore.
type MemoryStoreOptions struct {
	MaxEntries        int // Total maximum entries across all shards
	NumShards         int // Number of shards (defaults to 16)
	MaxValueSizeBytes int // Maximum allowed value size in bytes (defaults to 1MB)
}

// DefaultMemoryStoreOptions returns standard bounded options (10,000 entries, 16 shards, 1MB max value).
func DefaultMemoryStoreOptions() MemoryStoreOptions {
	return MemoryStoreOptions{
		MaxEntries:        10000,
		NumShards:         defaultNumShards,
		MaxValueSizeBytes: 1024 * 1024, // 1MB
	}
}

// NewMemoryStore creates a bounded in-memory sharded LRU cache.
func NewMemoryStore(opts MemoryStoreOptions) *MemoryStore {
	if opts.MaxEntries <= 0 {
		opts.MaxEntries = 10000
	}
	if opts.NumShards <= 0 {
		opts.NumShards = defaultNumShards
	}
	if opts.MaxValueSizeBytes <= 0 {
		opts.MaxValueSizeBytes = 1024 * 1024
	}

	// Never allocate more shard capacity than MaxEntries. The previous
	// floor-division also discarded remainder capacity for uneven sizes.
	if opts.NumShards > opts.MaxEntries {
		opts.NumShards = opts.MaxEntries
	}
	base := opts.MaxEntries / opts.NumShards
	remainder := opts.MaxEntries % opts.NumShards
	shards := make([]*cacheShard, opts.NumShards)
	for i := 0; i < opts.NumShards; i++ {
		capacity := base
		if i < remainder {
			capacity++
		}
		shards[i] = newCacheShard(capacity)
	}

	return &MemoryStore{
		shards:       shards,
		numShards:    opts.NumShards,
		maxValueSize: opts.MaxValueSizeBytes,
	}
}

func (m *MemoryStore) getShard(key string) *cacheShard {
	// Inline FNV-1a avoids allocating a hash object and converting the key
	// into a temporary []byte for each cache lookup.
	var h uint32 = 2166136261
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	idx := h % uint32(m.numShards)
	return m.shards[idx]
}

// Get retrieves a defensive clone of the cached value if it is in FRESH state.
func (m *MemoryStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	entry, err := m.GetEntry(ctx, key)
	if err != nil {
		return nil, false, err
	}
	if entry.State == StateFresh {
		return entry.Value, true, nil
	}
	return nil, false, nil
}

// GetEntry retrieves the cached entry and evaluates its freshness state (Fresh, Stale, Expired, NotFound).
func (m *MemoryStore) GetEntry(ctx context.Context, key string) (*Entry, error) {
	if key == "" {
		return nil, ErrInvalidKey
	}

	m.mu.RLock()
	defer m.mu.RUnlock() // Prevent Close from racing past this operation.
	if m.closed {
		return nil, ErrStoreClosed
	}

	shard := m.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	elem, exists := shard.items[key]
	if !exists {
		return &Entry{State: StateNotFound}, nil
	}

	item := elem.Value.(*cacheItem)
	now := time.Now()

	// Check if at or past final stale deadline (now >= staleUntil)
	if !item.staleUntil.IsZero() && !now.Before(item.staleUntil) {
		shard.lru.Remove(elem)
		delete(shard.items, key)
		shard.evictions++
		return &Entry{State: StateExpired}, nil
	}

	shard.lru.MoveToFront(elem)

	state := StateFresh
	// Check if at or past fresh deadline (now >= freshUntil)
	if !item.freshUntil.IsZero() && !now.Before(item.freshUntil) {
		state = StateStale
	}

	copied := make([]byte, len(item.value))
	copy(copied, item.value)

	return &Entry{
		Value:      copied,
		State:      state,
		FreshUntil: item.freshUntil,
		StaleUntil: item.staleUntil,
	}, nil
}

// Set stores a value with a single TTL (fresh until TTL, expired thereafter).
func (m *MemoryStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return m.SetWithStale(ctx, key, value, ttl, ttl)
}

// SetWithStale stores a value with distinct fresh and stale-servable lifetimes.
func (m *MemoryStore) SetWithStale(ctx context.Context, key string, value []byte, freshTTL time.Duration, staleTTL time.Duration) error {
	if key == "" {
		return ErrInvalidKey
	}
	if value == nil {
		return ErrNilValue
	}
	if m.maxValueSize > 0 && len(value) > m.maxValueSize {
		return ErrValueTooLarge
	}

	m.mu.RLock()
	defer m.mu.RUnlock() // Close waits for this operation to finish.
	if m.closed {
		return ErrStoreClosed
	}

	if staleTTL < freshTTL {
		staleTTL = freshTTL
	}

	now := time.Now()
	var freshUntil, staleUntil time.Time
	if freshTTL > 0 {
		freshUntil = now.Add(freshTTL)
	}
	if staleTTL > 0 {
		staleUntil = now.Add(staleTTL)
	}

	shard := m.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	valCopy := make([]byte, len(value))
	copy(valCopy, value)

	if elem, exists := shard.items[key]; exists {
		shard.lru.MoveToFront(elem)
		item := elem.Value.(*cacheItem)
		item.value = valCopy
		item.freshUntil = freshUntil
		item.staleUntil = staleUntil
		return nil
	}

	if shard.lru.Len() >= shard.maxEntries {
		oldest := shard.lru.Back()
		if oldest != nil {
			shard.lru.Remove(oldest)
			oldItem := oldest.Value.(*cacheItem)
			delete(shard.items, oldItem.key)
			shard.evictions++
		}
	}

	item := &cacheItem{
		key:        key,
		value:      valCopy,
		freshUntil: freshUntil,
		staleUntil: staleUntil,
	}
	elem := shard.lru.PushFront(item)
	shard.items[key] = elem

	return nil
}

// Delete removes an entry from the cache.
func (m *MemoryStore) Delete(ctx context.Context, key string) error {
	if key == "" {
		return ErrInvalidKey
	}

	m.mu.RLock()
	defer m.mu.RUnlock() // Close waits for this operation to finish.
	if m.closed {
		return ErrStoreClosed
	}

	shard := m.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	if elem, exists := shard.items[key]; exists {
		shard.lru.Remove(elem)
		delete(shard.items, key)
	}

	return nil
}

// Len returns the total number of items stored across all shards.
func (m *MemoryStore) Len() int {
	total := 0
	for _, shard := range m.shards {
		shard.mu.Lock()
		total += len(shard.items)
		shard.mu.Unlock()
	}
	return total
}

// TotalEvictions returns the total number of evictions recorded across all shards.
func (m *MemoryStore) TotalEvictions() int64 {
	var total int64
	for _, shard := range m.shards {
		shard.mu.Lock()
		total += shard.evictions
		shard.mu.Unlock()
	}
	return total
}

// Close marks the store as closed and clears all items.
func (m *MemoryStore) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}
	m.closed = true

	for _, shard := range m.shards {
		shard.mu.Lock()
		shard.items = make(map[string]*list.Element)
		shard.lru.Init()
		shard.mu.Unlock()
	}

	return nil
}
