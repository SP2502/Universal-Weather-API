package secrets

import (
	"context"
	"os"
	"strings"
)

type EnvSecretStore struct {
	prefix string
}

func NewEnvSecretStore(prefix string) *EnvSecretStore {
	return &EnvSecretStore{
		prefix: prefix,
	}
}

func (s *EnvSecretStore) GetSecret(ctx context.Context, key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", ErrEmptyKey
	}

	envKey := s.prefix + key
	val, exists := os.LookupEnv(envKey)
	if !exists || val == "" {
		return "", ErrSecretNotFound
	}

	return val, nil
}

func (s *EnvSecretStore) SetSecret(ctx context.Context, key, value string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return ErrEmptyKey
	}

	envKey := s.prefix + key
	return os.Setenv(envKey, value)
}

func (s *EnvSecretStore) HasSecret(ctx context.Context, key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}

	envKey := s.prefix + key
	val, exists := os.LookupEnv(envKey)
	return exists && val != ""
}
