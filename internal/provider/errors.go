package provider

import "errors"

var (
	ErrProviderNotFound    = errors.New("provider not found for requested capability")
	ErrProviderDisabled    = errors.New("provider is disabled")
	ErrProviderUnavailable = errors.New("provider upstream is unavailable")
	ErrMalformedResponse   = errors.New("provider returned an invalid or malformed response")
	ErrRateLimited         = errors.New("provider upstream rate limit exceeded")
	ErrAmbiguousLocation   = errors.New("ambiguous location; user disambiguation required")
)
