package cache

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

type RedisOptions struct {
	Addr         string        // host:port e.g. "localhost:6379"
	Password     string        // Redis AUTH password
	DB           int           // Redis DB index (default 0)
	Timeout      time.Duration // Network timeout (default 3s)
	MaxIdleConns int           // Connection pool size (default 10)
	FailOpen     bool          // Fall back to memory cache if Redis is unavailable (default true)
}

func DefaultRedisOptions(addr string) RedisOptions {
	return RedisOptions{
		Addr:         addr,
		Timeout:      3 * time.Second,
		MaxIdleConns: 10,
		FailOpen:     true,
	}
}

type redisItemEnvelope struct {
	Value      string `json:"v"`  // base64 encoded payload
	FreshUntil string `json:"fu"` // RFC3339Nano
	StaleUntil string `json:"su"` // RFC3339Nano
}

type RedisStore struct {
	opts        RedisOptions
	pool        chan net.Conn
	mu          sync.RWMutex
	closed      bool
	memoryStore *MemoryStore // fail-open fallback
}

// NewRedisStore initializes a Redis-backed cache store with connection pooling and fail-open support.
func NewRedisStore(opts RedisOptions) (*RedisStore, error) {
	if opts.Addr == "" {
		return nil, errors.New("redis: address cannot be empty")
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 3 * time.Second
	}
	if opts.MaxIdleConns <= 0 {
		opts.MaxIdleConns = 10
	}

	rs := &RedisStore{
		opts:        opts,
		pool:        make(chan net.Conn, opts.MaxIdleConns),
		memoryStore: NewMemoryStore(DefaultMemoryStoreOptions()),
	}

	// Verify connection on startup
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()

	if err := rs.Ping(ctx); err != nil {
		if !opts.FailOpen {
			return nil, fmt.Errorf("redis: initial ping failed: %w", err)
		}
	}

	return rs, nil
}

func (r *RedisStore) getConn(ctx context.Context) (net.Conn, error) {
	r.mu.RLock()
	if r.closed {
		r.mu.RUnlock()
		return nil, ErrStoreClosed
	}
	r.mu.RUnlock()

	select {
	case conn, ok := <-r.pool:
		if !ok || conn == nil {
			return nil, ErrStoreClosed
		}
		return conn, nil
	default:
	}

	d := net.Dialer{Timeout: r.opts.Timeout}
	conn, err := d.DialContext(ctx, "tcp", r.opts.Addr)
	if err != nil {
		return nil, err
	}

	// If password provided, send AUTH
	if r.opts.Password != "" {
		if err := r.execSimpleAuth(ctx, conn, r.opts.Password); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}

	// If DB > 0, send SELECT
	if r.opts.DB > 0 {
		if err := r.execSimpleSelect(ctx, conn, r.opts.DB); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}

	return conn, nil
}

func (r *RedisStore) putConn(conn net.Conn) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.closed || conn == nil {
		if conn != nil {
			_ = conn.Close()
		}
		return
	}

	select {
	case r.pool <- conn:
	default:
		_ = conn.Close()
	}
}

func (r *RedisStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	entry, err := r.GetEntry(ctx, key)
	if err != nil {
		return nil, false, err
	}
	if entry.State == StateFresh {
		return entry.Value, true, nil
	}
	return nil, false, nil
}

func (r *RedisStore) GetEntry(ctx context.Context, key string) (*Entry, error) {
	if key == "" {
		return nil, ErrInvalidKey
	}

	conn, err := r.getConn(ctx)
	if err != nil {
		if r.opts.FailOpen {
			return r.memoryStore.GetEntry(ctx, key)
		}
		return nil, fmt.Errorf("redis get connection failed: %w", err)
	}

	raw, found, err := r.execGet(ctx, conn, key)
	if err != nil {
		_ = conn.Close()
		if r.opts.FailOpen {
			return r.memoryStore.GetEntry(ctx, key)
		}
		return nil, err
	}
	r.putConn(conn)

	if !found {
		return &Entry{State: StateNotFound}, nil
	}

	var env redisItemEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("redis cache envelope malformed: %w", err)
	}

	val, err := base64.StdEncoding.DecodeString(env.Value)
	if err != nil {
		return nil, fmt.Errorf("redis cache envelope has invalid payload: %w", err)
	}

	var freshUntil, staleUntil time.Time
	if env.FreshUntil != "" {
		freshUntil, err = time.Parse(time.RFC3339Nano, env.FreshUntil)
		if err != nil {
			return nil, fmt.Errorf("redis cache invalid fresh timestamp: %w", err)
		}
	}
	if env.StaleUntil != "" {
		staleUntil, err = time.Parse(time.RFC3339Nano, env.StaleUntil)
		if err != nil {
			return nil, fmt.Errorf("redis cache invalid stale timestamp: %w", err)
		}
	}

	now := time.Now()
	if !staleUntil.IsZero() && !now.Before(staleUntil) {
		return &Entry{State: StateExpired}, nil
	}

	state := StateFresh
	if !freshUntil.IsZero() && !now.Before(freshUntil) {
		state = StateStale
	}

	return &Entry{
		Value:      val,
		State:      state,
		FreshUntil: freshUntil,
		StaleUntil: staleUntil,
	}, nil
}

func (r *RedisStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return r.SetWithStale(ctx, key, value, ttl, ttl)
}

func (r *RedisStore) SetWithStale(ctx context.Context, key string, value []byte, freshTTL, staleTTL time.Duration) error {
	if key == "" {
		return ErrInvalidKey
	}
	if value == nil {
		return ErrNilValue
	}
	if len(value) > 1024*1024 {
		return ErrValueTooLarge
	}

	if staleTTL < freshTTL {
		staleTTL = freshTTL
	}
	now := time.Now()
	var freshUntil, staleUntil time.Time
	if freshTTL > 0 {
		freshUntil = now.Add(freshTTL)
	}
	if staleTTL > 0 {
		staleUntil = now.Add(staleTTL)
	}

	env := redisItemEnvelope{
		Value: base64.StdEncoding.EncodeToString(value),
	}
	if !freshUntil.IsZero() {
		env.FreshUntil = freshUntil.Format(time.RFC3339Nano)
	}
	if !staleUntil.IsZero() {
		env.StaleUntil = staleUntil.Format(time.RFC3339Nano)
	}

	encoded, err := json.Marshal(env)
	if err != nil {
		return err
	}

	// Calculate Redis key TTL: use staleTTL or freshTTL
	redisTTL := staleTTL
	if redisTTL <= 0 {
		redisTTL = freshTTL
	}

	conn, err := r.getConn(ctx)
	if err != nil {
		if r.opts.FailOpen {
			return r.memoryStore.SetWithStale(ctx, key, value, freshTTL, staleTTL)
		}
		return err
	}

	if err := r.execSet(ctx, conn, key, encoded, redisTTL); err != nil {
		_ = conn.Close()
		if r.opts.FailOpen {
			return r.memoryStore.SetWithStale(ctx, key, value, freshTTL, staleTTL)
		}
		return err
	}
	r.putConn(conn)

	if r.opts.FailOpen {
		_ = r.memoryStore.SetWithStale(ctx, key, value, freshTTL, staleTTL)
	}

	return nil
}

func (r *RedisStore) Delete(ctx context.Context, key string) error {
	if key == "" {
		return ErrInvalidKey
	}

	conn, err := r.getConn(ctx)
	if err != nil {
		if r.opts.FailOpen {
			return r.memoryStore.Delete(ctx, key)
		}
		return err
	}

	if err := r.execDel(ctx, conn, key); err != nil {
		_ = conn.Close()
		if r.opts.FailOpen {
			return r.memoryStore.Delete(ctx, key)
		}
		return err
	}
	r.putConn(conn)

	if r.opts.FailOpen {
		_ = r.memoryStore.Delete(ctx, key)
	}

	return nil
}

func (r *RedisStore) Len() int {
	ctx, cancel := context.WithTimeout(context.Background(), r.opts.Timeout)
	defer cancel()
	conn, err := r.getConn(ctx)
	if err != nil {
		if r.opts.FailOpen {
			return r.memoryStore.Len()
		}
		return 0
	}

	size, err := r.execDBSize(ctx, conn)
	if err != nil {
		_ = conn.Close()
		if r.opts.FailOpen {
			return r.memoryStore.Len()
		}
		return 0
	}
	r.putConn(conn)

	return size
}

func (r *RedisStore) AllowsFallback() bool { return r.opts.FailOpen }

func (r *RedisStore) Ping(ctx context.Context) error {
	conn, err := r.getConn(ctx)
	if err != nil {
		return err
	}

	if err := r.execPing(ctx, conn); err != nil {
		_ = conn.Close()
		return err
	}
	r.putConn(conn)
	return nil
}

func (r *RedisStore) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}
	r.closed = true

	close(r.pool)
	for conn := range r.pool {
		_ = conn.Close()
	}

	if r.memoryStore != nil {
		_ = r.memoryStore.Close()
	}

	return nil
}

// --- Lightweight RESP (Redis Serialization Protocol) Helpers ---

func (r *RedisStore) execPing(ctx context.Context, conn net.Conn) error {
	defer r.boundIO(ctx, conn)()
	if _, err := conn.Write([]byte("*1\r\n$4\r\nPING\r\n")); err != nil {
		return err
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	if strings.HasPrefix(line, "+PONG") {
		return nil
	}
	return fmt.Errorf("unexpected ping response: %s", line)
}

func (r *RedisStore) execSimpleAuth(ctx context.Context, conn net.Conn, pass string) error {
	defer r.boundIO(ctx, conn)()
	cmd := fmt.Sprintf("*2\r\n$4\r\nAUTH\r\n$%d\r\n%s\r\n", len(pass), pass)
	if _, err := conn.Write([]byte(cmd)); err != nil {
		return err
	}
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	if strings.HasPrefix(line, "+OK") {
		return nil
	}
	return fmt.Errorf("redis auth failed: %s", line)
}

func (r *RedisStore) execSimpleSelect(ctx context.Context, conn net.Conn, db int) error {
	defer r.boundIO(ctx, conn)()
	dbStr := strconv.Itoa(db)
	cmd := fmt.Sprintf("*2\r\n$6\r\nSELECT\r\n$%d\r\n%s\r\n", len(dbStr), dbStr)
	if _, err := conn.Write([]byte(cmd)); err != nil {
		return err
	}
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	if strings.HasPrefix(line, "+OK") {
		return nil
	}
	return fmt.Errorf("redis select db failed: %s", line)
}

func (r *RedisStore) execGet(ctx context.Context, conn net.Conn, key string) ([]byte, bool, error) {
	defer r.boundIO(ctx, conn)()
	cmd := fmt.Sprintf("*2\r\n$3\r\nGET\r\n$%d\r\n%s\r\n", len(key), key)
	if _, err := conn.Write([]byte(cmd)); err != nil {
		return nil, false, err
	}

	reader := bufio.NewReader(conn)
	prefix, err := reader.ReadByte()
	if err != nil {
		return nil, false, err
	}

	switch prefix {
	case '$':
		lenStr, err := reader.ReadString('\n')
		if err != nil {
			return nil, false, err
		}
		length, err := strconv.Atoi(strings.TrimSpace(lenStr))
		if err != nil {
			return nil, false, err
		}
		if length == -1 {
			return nil, false, nil // Not found
		}

		if length < 0 || length > 2*1024*1024 {
			return nil, false, fmt.Errorf("redis get reply exceeds protocol size limit")
		}
		buf := make([]byte, length)
		if _, err := io.ReadFull(reader, buf); err != nil {
			return nil, false, err
		}
		// Discard trailing CRLF
		_, _ = reader.ReadString('\n')
		return buf, true, nil

	case '-':
		errMsg, _ := reader.ReadString('\n')
		return nil, false, fmt.Errorf("redis error: %s", strings.TrimSpace(errMsg))
	default:
		return nil, false, fmt.Errorf("redis unexpected response prefix: %c", prefix)
	}
}

func (r *RedisStore) execSet(ctx context.Context, conn net.Conn, key string, val []byte, ttl time.Duration) error {
	defer r.boundIO(ctx, conn)()

	var buf bytes.Buffer
	if ttl > 0 {
		milliseconds := int64(math.Ceil(float64(ttl) / float64(time.Millisecond)))
		if milliseconds < 1 {
			milliseconds = 1
		}
		msStr := strconv.FormatInt(milliseconds, 10)
		buf.WriteString(fmt.Sprintf("*5\r\n$3\r\nSET\r\n$%d\r\n%s\r\n$%d\r\n", len(key), key, len(val)))
		buf.Write(val)
		buf.WriteString(fmt.Sprintf("\r\n$2\r\nPX\r\n$%d\r\n%s\r\n", len(msStr), msStr))
	} else {
		buf.WriteString(fmt.Sprintf("*3\r\n$3\r\nSET\r\n$%d\r\n%s\r\n$%d\r\n", len(key), key, len(val)))
		buf.Write(val)
		buf.WriteString("\r\n")
	}

	if _, err := conn.Write(buf.Bytes()); err != nil {
		return err
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	if strings.HasPrefix(line, "+OK") {
		return nil
	}
	return fmt.Errorf("redis set failed: %s", strings.TrimSpace(line))
}

func (r *RedisStore) execDel(ctx context.Context, conn net.Conn, key string) error {
	defer r.boundIO(ctx, conn)()
	cmd := fmt.Sprintf("*2\r\n$3\r\nDEL\r\n$%d\r\n%s\r\n", len(key), key)
	if _, err := conn.Write([]byte(cmd)); err != nil {
		return err
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	if strings.HasPrefix(line, ":") {
		return nil
	}
	return fmt.Errorf("redis del failed: %s", strings.TrimSpace(line))
}

func (r *RedisStore) execDBSize(ctx context.Context, conn net.Conn) (int, error) {
	defer r.boundIO(ctx, conn)()
	if _, err := conn.Write([]byte("*1\r\n$6\r\nDBSIZE\r\n")); err != nil {
		return 0, err
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return 0, err
	}
	if strings.HasPrefix(line, ":") {
		size, err := strconv.Atoi(strings.TrimSpace(line[1:]))
		if err != nil {
			return 0, err
		}
		return size, nil
	}
	return 0, fmt.Errorf("redis dbsize failed: %s", strings.TrimSpace(line))
}

// boundIO applies the earlier of the caller deadline and the Redis operation
// timeout, including on reused sockets. Cancellation interrupts active I/O.
// stop/wait ensures a cancelled callback cannot affect a later pooled borrower.
func (r *RedisStore) boundIO(ctx context.Context, conn net.Conn) func() {
	deadline := time.Now().Add(r.opts.Timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if ctx.Err() != nil {
		deadline = time.Now()
	}
	_ = conn.SetDeadline(deadline)
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { defer close(done); _ = conn.SetDeadline(time.Now()) })
	return func() {
		if !stop() {
			<-done
		}
		_ = conn.SetDeadline(time.Time{})
	}
}

// AllowToken performs one server-time, atomic Redis token-bucket decision.
// Unlike the optional weather cache, quota admission NEVER falls back to
// uncoordinated local counters when Redis is configured.
func (r *RedisStore) AllowToken(ctx context.Context, key string, rate float64, burst int) (bool, error) {
	if len(key) == 0 || len(key) > 256 || rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) || burst <= 0 {
		return false, fmt.Errorf("invalid distributed token-bucket parameters")
	}
	conn, err := r.getConn(ctx)
	if err != nil {
		return false, err
	}
	allowed, err := r.execTokenBucket(ctx, conn, key, rate, burst)
	if err != nil {
		_ = conn.Close()
		return false, err
	}
	r.putConn(conn)
	return allowed, nil
}

const tokenBucketScript = `
local ts=redis.call('TIME')
local now=tonumber(ts[1])+tonumber(ts[2])/1000000
local rate=tonumber(ARGV[1])
local burst=tonumber(ARGV[2])
local values=redis.call('HMGET',KEYS[1],'tokens','updated')
local tokens=tonumber(values[1]) or burst
local updated=tonumber(values[2]) or now
local elapsed=math.max(0,now-updated)
tokens=math.min(burst,tokens+elapsed*rate)
local allowed=0
if tokens>=1 then tokens=tokens-1; allowed=1 end
redis.call('HSET',KEYS[1],'tokens',tostring(tokens),'updated',tostring(now))
redis.call('PEXPIRE',KEYS[1],math.max(1000,math.ceil(2000*burst/rate)))
return allowed
`

func (r *RedisStore) execTokenBucket(ctx context.Context, conn net.Conn, key string, rate float64, burst int) (bool, error) {
	defer r.boundIO(ctx, conn)()
	args := []string{"EVAL", tokenBucketScript, "1", key, strconv.FormatFloat(rate, 'g', -1, 64), strconv.Itoa(burst)}
	var b bytes.Buffer
	fmt.Fprintf(&b, "*%d\r\n", len(args))
	for _, arg := range args {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(arg), arg)
	}
	if _, err := conn.Write(b.Bytes()); err != nil {
		return false, err
	}
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	if strings.HasPrefix(line, ":1\r\n") {
		return true, nil
	}
	if strings.HasPrefix(line, ":0\r\n") {
		return false, nil
	}
	return false, fmt.Errorf("redis rate limiter failed: unexpected response type")
}

// ReserveBudget atomically admits one or more chargeable provider units in
// Redis server-time UTC minute/day windows. Admission is conservative: it is
// never refunded after an ambiguous outbound timeout.
// ReserveBudget preserves the Phase 04 API for providers without a per-second ceiling.
func (r *RedisStore) ReserveBudget(ctx context.Context, key string, perMinute, perDay, cost int64) (bool, error) {
	return r.ReserveBudgetWithSecond(ctx, key, 0, perMinute, perDay, cost)
}

// ReserveBudgetWithSecond applies second/minute/day windows in one atomic
// Redis EVAL. Uses Redis TIME, so replicas cannot disagree about UTC slots.
func (r *RedisStore) ReserveBudgetWithSecond(ctx context.Context, key string, perSecond, perMinute, perDay, cost int64) (bool, error) {
	if len(key) == 0 || len(key) > 256 || (perSecond <= 0 && perMinute <= 0 && perDay <= 0) || perSecond < 0 || perMinute < 0 || perDay < 0 || cost <= 0 {
		return false, errors.New("invalid Redis provider quota")
	}
	conn, err := r.getConn(ctx)
	if err != nil {
		return false, err
	}
	ok, err := r.execQuota(ctx, conn, key, perSecond, perMinute, perDay, cost)
	if err != nil {
		_ = conn.Close()
		return false, err
	}
	r.putConn(conn)
	return ok, nil
}

const quotaScript = `
local tm=redis.call('TIME')
local seconds=tonumber(tm[1])
local perSecond=tonumber(ARGV[1])
local minute=tonumber(ARGV[2])
local daily=tonumber(ARGV[3])
local cost=tonumber(ARGV[4])
local secondKey=KEYS[1]..':s:'..seconds
local minuteKey=KEYS[1]..':m:'..math.floor(seconds/60)
local dayKey=KEYS[1]..':d:'..math.floor(seconds/86400)
if perSecond>0 and (tonumber(redis.call('GET',secondKey)) or 0)+cost>perSecond then return 0 end
if minute>0 and (tonumber(redis.call('GET',minuteKey)) or 0)+cost>minute then return 0 end
if daily>0 and (tonumber(redis.call('GET',dayKey)) or 0)+cost>daily then return 0 end
if perSecond>0 then redis.call('INCRBY',secondKey,cost); redis.call('EXPIRE',secondKey,3) end
if minute>0 then redis.call('INCRBY',minuteKey,cost); redis.call('EXPIRE',minuteKey,120) end
if daily>0 then redis.call('INCRBY',dayKey,cost); redis.call('EXPIRE',dayKey,172800) end
return 1
`

func (r *RedisStore) execQuota(ctx context.Context, conn net.Conn, key string, perSecond, perMinute, perDay, cost int64) (bool, error) {
	defer r.boundIO(ctx, conn)()
	args := []string{"EVAL", quotaScript, "1", key, strconv.FormatInt(perSecond, 10), strconv.FormatInt(perMinute, 10), strconv.FormatInt(perDay, 10), strconv.FormatInt(cost, 10)}
	var b bytes.Buffer
	fmt.Fprintf(&b, "*%d\r\n", len(args))
	for _, arg := range args {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(arg), arg)
	}
	if _, err := conn.Write(b.Bytes()); err != nil {
		return false, err
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return false, err
	}
	if line == ":1\r\n" {
		return true, nil
	}
	if line == ":0\r\n" {
		return false, nil
	}
	return false, errors.New("unexpected Redis quota response")
}
