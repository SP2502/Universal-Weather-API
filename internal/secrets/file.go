package secrets

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type FileSecretStore struct {
	baseDir string
	mu      sync.RWMutex
}

// NewFileSecretStore creates a SecretStore backed by a local directory (e.g. Kubernetes/Docker secret volume mounts).
func NewFileSecretStore(baseDir string) (*FileSecretStore, error) {
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		baseDir = "/var/run/secrets/uap"
	}

	cleanDir := filepath.Clean(baseDir)
	return &FileSecretStore{
		baseDir: cleanDir,
	}, nil
}

func (s *FileSecretStore) secretPath(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", ErrEmptyKey
	}

	// Prevent directory traversal
	if strings.Contains(key, "..") || strings.Contains(key, "/") || strings.Contains(key, "\\") {
		return "", errors.New("invalid secret key name")
	}

	return filepath.Join(s.baseDir, key), nil
}

func (s *FileSecretStore) GetSecret(ctx context.Context, key string) (string, error) {
	filePath, err := s.secretPath(key)
	if err != nil {
		return "", err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrSecretNotFound
		}
		return "", fmt.Errorf("failed to read secret file: %w", err)
	}

	val := strings.TrimRight(string(data), "\r\n")
	if val == "" {
		return "", ErrSecretNotFound
	}

	return val, nil
}

func (s *FileSecretStore) SetSecret(ctx context.Context, key, value string) error {
	filePath, err := s.secretPath(key)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.baseDir, 0700); err != nil {
		return fmt.Errorf("failed to create secret directory: %w", err)
	}

	return os.WriteFile(filePath, []byte(value), 0600)
}

func (s *FileSecretStore) HasSecret(ctx context.Context, key string) bool {
	filePath, err := s.secretPath(key)
	if err != nil {
		return false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	info, err := os.Stat(filePath)
	return err == nil && !info.IsDir() && info.Size() > 0
}
