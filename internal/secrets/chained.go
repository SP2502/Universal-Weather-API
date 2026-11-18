package secrets

import (
	"context"
	"strings"
	"sync"
)

type ChainedSecretStore struct {
	stores []SecretStore
	mu     sync.RWMutex
}

// NewChainedSecretStore creates a composite SecretStore that searches backends in priority order.
func NewChainedSecretStore(stores ...SecretStore) *ChainedSecretStore {
	var validStores []SecretStore
	for _, s := range stores {
		if s != nil {
			validStores = append(validStores, s)
		}
	}
	return &ChainedSecretStore{
		stores: validStores,
	}
}

func (c *ChainedSecretStore) GetSecret(ctx context.Context, key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", ErrEmptyKey
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, s := range c.stores {
		val, err := s.GetSecret(ctx, key)
		if err == nil && val != "" {
			return val, nil
		}
	}

	return "", ErrSecretNotFound
}

func (c *ChainedSecretStore) SetSecret(ctx context.Context, key, value string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return ErrEmptyKey
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.stores) == 0 {
		return ErrSecretNotFound
	}

	// Write to the first available store in the chain
	return c.stores[0].SetSecret(ctx, key, value)
}

func (c *ChainedSecretStore) HasSecret(ctx context.Context, key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, s := range c.stores {
		if s.HasSecret(ctx, key) {
			return true
		}
	}

	return false
}
