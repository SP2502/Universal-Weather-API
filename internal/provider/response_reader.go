package provider

import (
	"fmt"
	"io"
)

const MaxUpstreamJSONBytes = 1 << 20

// ReadLimitedJSON rejects oversized responses rather than accepting a
// truncated prefix that happens to be syntactically valid JSON.
func ReadLimitedJSON(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, MaxUpstreamJSONBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read provider body", ErrMalformedResponse)
	}
	if len(data) > MaxUpstreamJSONBytes {
		return nil, fmt.Errorf("%w: provider JSON exceeds 1 MiB", ErrMalformedResponse)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: empty provider response", ErrMalformedResponse)
	}
	return data, nil
}
