package secrets

import (
	"context"
	"strings"
	"sync"
)

type MemorySecretStore struct {
	mu      sync.RWMutex
	secrets map[string]string
}

func NewMemorySecretStore() *MemorySecretStore {
	return &MemorySecretStore{
		secrets: make(map[string]string),
	}
}

func (s *MemorySecretStore) GetSecret(ctx context.Context, key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key = strings.TrimSpace(key)
	if key == "" {
		return "", ErrEmptyKey
	}

	val, exists := s.secrets[key]
	if !exists || val == "" {
		return "", ErrSecretNotFound
	}

	return val, nil
}

func (s *MemorySecretStore) SetSecret(ctx context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key = strings.TrimSpace(key)
	if key == "" {
		return ErrEmptyKey
	}

	s.secrets[key] = value
	return nil
}

func (s *MemorySecretStore) HasSecret(ctx context.Context, key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}

	val, exists := s.secrets[key]
	return exists && val != ""
}
