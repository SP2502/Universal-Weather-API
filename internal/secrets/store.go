package secrets

import (
	"context"
	"errors"
)

var (
	ErrSecretNotFound = errors.New("secret not found")
	ErrEmptyKey       = errors.New("secret key cannot be empty")
)

type SecretStore interface {
	GetSecret(ctx context.Context, key string) (string, error)
	SetSecret(ctx context.Context, key, value string) error
	HasSecret(ctx context.Context, key string) bool
}
