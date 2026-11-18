package provider

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type Status string

const (
	StatusActive     Status = "ACTIVE"
	StatusDegraded   Status = "DEGRADED"
	StatusDisabled   Status = "DISABLED"
	StatusDeprecated Status = "DEPRECATED"
	StatusRemoved    Status = "REMOVED"
)

type Attribution struct {
	Required bool   `json:"required"`
	Text     string `json:"text"`
	URL      string `json:"url,omitempty"`
}

type Manifest struct {
	ID                  string        `json:"id"`
	Name                string        `json:"name"`
	Version             string        `json:"version"`
	Capabilities        []string      `json:"capabilities"`
	BaseURL             string        `json:"base_url"`
	Timeout             time.Duration `json:"timeout"`
	FallbackPriority    int           `json:"fallback_priority"` // Lower number = higher priority
	Attribution         Attribution   `json:"attribution"`
	Status              Status        `json:"status"`
	CredentialsRequired []string      `json:"credentials_required,omitempty"`
	DocumentationURL    string        `json:"documentation_url,omitempty"`
	TermsURL            string        `json:"terms_url,omitempty"`
}

func (m Manifest) Validate() error {
	if strings.TrimSpace(m.ID) == "" {
		return errors.New("manifest: provider id is required")
	}
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("manifest: provider name is required")
	}
	if len(m.Capabilities) == 0 {
		return errors.New("manifest: at least one capability is required")
	}
	if m.Timeout <= 0 {
		return errors.New("manifest: timeout must be greater than 0")
	}
	if m.FallbackPriority <= 0 {
		return errors.New("manifest: fallback priority must be >= 1")
	}
	if m.BaseURL != "" {
		if _, err := url.ParseRequestURI(m.BaseURL); err != nil {
			return fmt.Errorf("manifest: invalid base url: %w", err)
		}
	}
	if m.Attribution.Required && strings.TrimSpace(m.Attribution.Text) == "" {
		return errors.New("manifest: attribution text is required when attribution is required")
	}

	switch m.Status {
	case StatusActive, StatusDegraded, StatusDisabled, StatusDeprecated, StatusRemoved, "":
		// valid
	default:
		return fmt.Errorf("manifest: unknown status %s", m.Status)
	}

	return nil
}
