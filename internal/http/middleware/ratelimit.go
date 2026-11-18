package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/universal-api/platform/internal/api"
	"github.com/universal-api/platform/internal/cache"
	"github.com/universal-api/platform/internal/observability/logging"
	"github.com/universal-api/platform/internal/observability/metrics"
)

// Each gateway request is throttled on the server-observed peer address, not
// on an unverified API key. Rotating bogus credentials cannot create new quota
// identities or store raw secrets in memory/Redis. Trusted-proxy interpretation
// must be implemented explicitly at the edge, never via arbitrary XFF headers.
type bucket struct {
	tokens     float64
	lastRefill time.Time
	lastSeen   time.Time
}

type RateLimiter struct {
	mu               sync.Mutex
	rate             float64
	burst            float64
	buckets          map[string]*bucket
	redisStore       *cache.RedisStore
	maxBuckets       int
	ops              uint64
	trustedProxyNets []*net.IPNet // immutable after configuration
}

func NewRateLimiter(rate float64, burst int) *RateLimiter {
	if rate <= 0 {
		rate = 100
	}
	if burst <= 0 {
		burst = 200
	}
	return &RateLimiter{rate: rate, burst: float64(burst), buckets: make(map[string]*bucket), maxBuckets: 10000}
}
func NewDistributedRateLimiter(rate float64, burst int, redisStore *cache.RedisStore) *RateLimiter {
	rl := NewRateLimiter(rate, burst)
	rl.redisStore = redisStore
	return rl
}

// ParseTrustedProxies accepts operator-reviewed proxy CIDRs. Trusting an
// arbitrary client-supplied XFF header is strictly forbidden. Broad /0
// networks are rejected because they make every client self-declared.
func ParseTrustedProxies(value string) ([]*net.IPNet, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	if len(parts) > 16 {
		return nil, fmt.Errorf("too many trusted proxy networks")
	}
	out := make([]*net.IPNet, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		_, netw, err := net.ParseCIDR(p)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy CIDR")
		}
		ones, bits := netw.Mask.Size()
		if (bits == 32 && ones < 8) || (bits == 128 && ones < 16) {
			return nil, fmt.Errorf("trusted proxy CIDR is dangerously broad")
		}
		out = append(out, netw)
	}
	return out, nil
}

// SetTrustedProxies must run before serving requests.
func (rl *RateLimiter) SetTrustedProxies(nets []*net.IPNet) { rl.trustedProxyNets = nets }

func (rl *RateLimiter) isTrusted(ip net.IP) bool {
	for _, netw := range rl.trustedProxyNets {
		if netw.Contains(ip) {
			return true
		}
	}
	return false
}

// clientRemote resolves the nearest untrusted hop from the right of a bounded
// X-Forwarded-For chain, only if the actual network peer is trusted.
// Malformed chains fall back to the transport peer, never attacker input.
func (rl *RateLimiter) clientRemote(r *http.Request) string {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	peerIP := net.ParseIP(peer)
	if peerIP == nil || !rl.isTrusted(peerIP) {
		return r.RemoteAddr
	}
	xff := r.Header.Values("X-Forwarded-For")
	if len(xff) != 1 || len(xff[0]) > 1024 || xff[0] == "" {
		return r.RemoteAddr
	}
	hops := strings.Split(xff[0], ",")
	if len(hops) > 16 {
		return r.RemoteAddr
	}
	for i := len(hops) - 1; i >= 0; i-- {
		raw := strings.TrimSpace(hops[i])
		ip := net.ParseIP(raw)
		if ip == nil {
			return r.RemoteAddr
		}
		if !rl.isTrusted(ip) {
			return ip.String()
		}
	}
	return r.RemoteAddr
}

func identity(remote string) string {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	host = strings.TrimSpace(host)
	parsed := net.ParseIP(host)
	if parsed == nil {
		host = "unknown"
	} else {
		host = parsed.String()
	}
	sum := sha256.Sum256([]byte(host))
	return hex.EncodeToString(sum[:16])
}

func (rl *RateLimiter) allow(ctx context.Context, key string) (bool, error) {
	if rl.redisStore != nil {
		ctx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		defer cancel()
		// Failure cannot fall back to a per-replica quota; that would make a
		// shared limit unenforceable when Redis fails.
		return rl.redisStore.AllowToken(ctx, "uap:ratelimit:v2:"+key, rl.rate, int(rl.burst))
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	rl.ops++
	if rl.ops%512 == 0 {
		for k, b := range rl.buckets {
			if now.Sub(b.lastSeen) > 5*time.Minute {
				delete(rl.buckets, k)
			}
		}
	}
	b, exists := rl.buckets[key]
	if !exists {
		if len(rl.buckets) >= rl.maxBuckets {
			// Drop old buckets, but never evict active buckets to admit new
			// arbitrary identities without a limit.
			for k, b := range rl.buckets {
				if now.Sub(b.lastSeen) > time.Minute {
					delete(rl.buckets, k)
				}
			}
			if len(rl.buckets) >= rl.maxBuckets {
				return false, nil
			}
		}
		rl.buckets[key] = &bucket{tokens: rl.burst - 1, lastRefill: now, lastSeen: now}
		return true, nil
	}
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * rl.rate
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.lastRefill = now
	b.lastSeen = now
	if b.tokens >= 1 {
		b.tokens--
		return true, nil
	}
	return false, nil
}

// ClientAdmission isolates only the exact public probes. It does not bypass
// authentication, scope checks or resource limits on any capability route.
func (rl *RateLimiter) ClientAdmission(next http.Handler) http.Handler {
	limited := rl.Middleware()(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && (r.URL.Path == "/health" || r.URL.Path == "/liveness" || r.URL.Path == "/readiness") {
			next.ServeHTTP(w, r)
			return
		}
		limited.ServeHTTP(w, r)
	})
}

func (rl *RateLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqID := logging.RequestIDFromContext(r.Context())
			allowed, err := rl.allow(r.Context(), identity(rl.clientRemote(r)))
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					w.Header().Set("Retry-After", "1")
				}
				api.WriteError(w, http.StatusServiceUnavailable, "RATE_LIMIT_BACKEND_UNAVAILABLE", "Shared rate limit state unavailable. Please retry.", reqID, true)
				return
			}
			if !allowed {
				metrics.DefaultMetrics.RateLimitEventsTotal.Inc()
				w.Header().Set("Retry-After", "1")
				api.WriteError(w, http.StatusTooManyRequests, "RATE_LIMIT_EXCEEDED", "Rate limit exceeded. Please retry after backoff.", reqID, true)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
