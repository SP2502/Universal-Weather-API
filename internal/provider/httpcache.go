package provider

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ForecastFreshTTL returns the most restrictive explicit origin freshness
// lifetime. nil means the upstream supplied no usable directive, so a separately
// approved conservative gateway policy may be used. Zero means do not cache.
// Shared gateway caches must never store private or no-store responses.
func ForecastFreshTTL(h http.Header, now time.Time) *time.Duration {
	if strings.TrimSpace(h.Get("Vary")) == "*" {
		return durationPtr(0)
	}
	cc := strings.ToLower(h.Get("Cache-Control"))
	var maxAge *time.Duration
	for _, part := range strings.Split(cc, ",") {
		directive := strings.TrimSpace(part)
		if directive == "no-store" || directive == "no-cache" || directive == "private" || strings.HasPrefix(directive, "no-cache=") || strings.HasPrefix(directive, "private=") {
			return durationPtr(0)
		}
		if strings.HasPrefix(directive, "s-maxage=") || strings.HasPrefix(directive, "max-age=") {
			chunks := strings.SplitN(directive, "=", 2)
			if len(chunks) != 2 {
				continue
			}
			n, err := strconv.ParseInt(strings.Trim(chunks[1], `"`), 10, 64)
			if err != nil || n < 0 {
				continue
			}
			if n > 86400*365 {
				n = 86400 * 365
			}
			candidate := time.Duration(n) * time.Second
			if maxAge == nil || candidate < *maxAge {
				maxAge = &candidate
			}
		}
	}
	if maxAge != nil {
		if rawAge := h.Get("Age"); rawAge != "" {
			if age, e := strconv.ParseInt(strings.TrimSpace(rawAge), 10, 64); e == nil && age >= 0 {
				if age > 86400*365 {
					age = 86400 * 365
				}
				*maxAge -= time.Duration(age) * time.Second
			}
		}
		if *maxAge < 0 {
			*maxAge = 0
		}
		return maxAge
	}
	if exp, e := http.ParseTime(h.Get("Expires")); e == nil {
		remain := exp.Sub(now)
		if remain < 0 {
			remain = 0
		}
		return &remain
	}
	return nil
}
func durationPtr(d time.Duration) *time.Duration { return &d }
