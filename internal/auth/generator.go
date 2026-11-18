package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/universal-api/platform/internal/database"
)

type KeyPair struct {
	PlaintextKey string `json:"plaintext_key"`
	KeyPrefix    string `json:"key_prefix"`
	KeyHash      string `json:"key_hash"`
}

// GenerateKey generates a cryptographically secure API key with 256 bits of entropy.
// prefix should typically be "uap_live_" or "uap_test_".
func GenerateKey(prefix string) (*KeyPair, error) {
	if prefix == "" {
		prefix = "uap_live_"
	}
	if !strings.HasSuffix(prefix, "_") {
		prefix += "_"
	}

	// 32 bytes of random entropy (256 bits)
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("failed to generate secure random bytes: %w", err)
	}

	// URL-safe base64 encoding without padding
	randomPart := base64.RawURLEncoding.EncodeToString(buf)
	plaintext := prefix + randomPart

	// Extract public prefix for operational identification (e.g. "uap_live_abcd")
	prefixLen := len(prefix) + 8
	if len(plaintext) < prefixLen {
		return nil, errors.New("generated key unexpectedly short")
	}
	keyPrefix := plaintext[:prefixLen]

	keyHash := database.HashKey(plaintext)

	return &KeyPair{
		PlaintextKey: plaintext,
		KeyPrefix:    keyPrefix,
		KeyHash:      keyHash,
	}, nil
}
