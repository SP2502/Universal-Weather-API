package auth

import (
	"context"
	"crypto/subtle"
	"strings"
	"time"

	"github.com/universal-api/platform/internal/database"
)

type contextKey string

const (
	CallerKey contextKey = "authenticated_caller"
)

type Caller struct {
	KeyID     string
	Name      string
	KeyPrefix string
	Scopes    []string
}

type Authenticator struct {
	store database.Store
}

func NewAuthenticator(store database.Store) *Authenticator {
	return &Authenticator{store: store}
}

func (a *Authenticator) AuthenticateKey(ctx context.Context, rawKey string) (*Caller, error) {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" {
		return nil, database.ErrNotFound
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	keyHash := database.HashKey(rawKey)
	apiKey, err := a.store.GetAPIKeyByHash(ctx, keyHash)
	if err != nil {
		return nil, err
	}

	// Constant-time hash verification against matched key (primary or secondary)
	matchPrimary := subtle.ConstantTimeCompare([]byte(keyHash), []byte(apiKey.KeyHash)) == 1
	matchSecondary := apiKey.SecondaryKeyHash != "" && subtle.ConstantTimeCompare([]byte(keyHash), []byte(apiKey.SecondaryKeyHash)) == 1

	if !matchPrimary && !matchSecondary {
		return nil, database.ErrNotFound
	}

	caller := &Caller{
		KeyID:     apiKey.ID,
		Name:      apiKey.Name,
		KeyPrefix: apiKey.KeyPrefix,
		Scopes:    apiKey.Scopes,
	}

	return caller, nil
}

func WithCaller(ctx context.Context, caller *Caller) context.Context {
	return context.WithValue(ctx, CallerKey, caller)
}

func CallerFromContext(ctx context.Context) *Caller {
	if val, ok := ctx.Value(CallerKey).(*Caller); ok {
		return val
	}
	return nil
}
