package cache

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// A local scripted RESP peer verifies the request wire contract. It is NOT a
// replacement for real Redis/Lua multi-instance acceptance.
func phase04Peer(t *testing.T, handle func([]string) string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				rd := bufio.NewReader(c)
				for {
					header, e := rd.ReadString('\n')
					if e != nil {
						return
					}
					if !strings.HasPrefix(header, "*") {
						return
					}
					count, e := strconv.Atoi(strings.TrimSpace(header[1:]))
					if e != nil {
						return
					}
					args := make([]string, 0, count)
					for i := 0; i < count; i++ {
						lengthLine, e := rd.ReadString('\n')
						if e != nil {
							return
						}
						if !strings.HasPrefix(lengthLine, "$") {
							return
						}
						size, e := strconv.Atoi(strings.TrimSpace(lengthLine[1:]))
						if e != nil || size < 0 || size > 1<<20 {
							return
						}
						b := make([]byte, size+2)
						if _, e = io.ReadFull(rd, b); e != nil {
							return
						}
						args = append(args, string(b[:size]))
					}
					reply := handle(args)
					if reply == "STALL" {
						time.Sleep(500 * time.Millisecond)
						return
					}
					if _, e = fmt.Fprint(c, reply); e != nil {
						return
					}
				}
			}(conn)
		}
	}()
	return ln.Addr().String()
}

func TestPhase04_SeparateSWRAndSIEBoundaries(t *testing.T) {
	p := NewPolicyEngine().EvaluatePolicy("weather", "a")
	t0 := time.Now()
	entry := &Entry{State: StateStale, FreshUntil: t0.Add(-7 * time.Minute), StaleUntil: t0.Add(23 * time.Minute)}
	if p.CanServeSWR(entry, t0) {
		t.Fatal("SWR incorrectly extended by SIE")
	}
	if !p.CanServeSIE(entry, t0) {
		t.Fatal("SIE must remain eligible until its own expiry")
	}
	entry.FreshUntil = t0.Add(-31 * time.Minute)
	if p.CanServeSIE(entry, t0) {
		t.Fatal("SIE exceeded documented deadline")
	}
}

func TestPhase04_HealthyRedisMissIsAuthoritative(t *testing.T) {
	addr := phase04Peer(t, func(args []string) string {
		switch args[0] {
		case "PING":
			return "+PONG\r\n"
		case "SET":
			return "+OK\r\n"
		case "GET":
			return "$-1\r\n"
		}
		return ":0\r\n"
	})
	opts := DefaultRedisOptions(addr)
	store, err := NewRedisStore(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.Set(ctx, "removed", []byte("secret-old-value"), time.Minute); err != nil {
		t.Fatal(err)
	}
	entry, err := store.GetEntry(ctx, "removed")
	if err != nil {
		t.Fatal(err)
	}
	if entry.State != StateNotFound {
		t.Fatalf("healthy Redis miss resurrected local mirror: %v", entry.State)
	}
}

func TestPhase04_RedisCallerDeadlineAndAUTHDeadline(t *testing.T) {
	t.Run("GET", func(t *testing.T) {
		addr := phase04Peer(t, func(args []string) string {
			if args[0] == "GET" {
				return "STALL"
			}
			return "+PONG\r\n"
		})
		opts := DefaultRedisOptions(addr)
		opts.FailOpen = false
		opts.Timeout = time.Second
		store, err := NewRedisStore(opts)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Millisecond)
		defer cancel()
		began := time.Now()
		_, err = store.GetEntry(ctx, "key")
		if err == nil || time.Since(began) > 350*time.Millisecond {
			t.Fatalf("GET deadline was ignored: %v (%v)", err, time.Since(began))
		}
	})
	t.Run("AUTH", func(t *testing.T) {
		addr := phase04Peer(t, func(args []string) string { return "STALL" })
		opts := DefaultRedisOptions(addr)
		opts.Password = "synthetic"
		opts.FailOpen = false
		opts.Timeout = 60 * time.Millisecond
		began := time.Now()
		_, err := NewRedisStore(opts)
		if err == nil || time.Since(began) > 350*time.Millisecond {
			t.Fatalf("AUTH deadline was ignored: %v (%v)", err, time.Since(began))
		}
	})
}

func TestPhase04_QuotaLuaWireAndSharedUse(t *testing.T) {
	var mu sync.Mutex
	observed := []string{}
	addr := phase04Peer(t, func(args []string) string {
		if args[0] == "PING" {
			return "+PONG\r\n"
		}
		if args[0] == "EVAL" {
			mu.Lock()
			defer mu.Unlock()
			observed = append(observed, args[1])
			if args[3] != "uap:providerquota:v1:synthetic" {
				t.Errorf("bad quota key: %q", args[3])
			}
			if len(observed) <= 2 {
				return ":1\r\n"
			}
			return ":0\r\n"
		}
		return "-ERR unexpected\r\n"
	})
	opts := DefaultRedisOptions(addr)
	opts.FailOpen = false
	a, err := NewRedisStore(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := NewRedisStore(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	for i, store := range []*RedisStore{a, b, a} {
		accepted, err := store.ReserveBudget(context.Background(), "uap:providerquota:v1:synthetic", 2, 10, 1)
		if err != nil {
			t.Fatal(err)
		}
		if accepted != (i < 2) {
			t.Fatalf("admission %d=%v", i, accepted)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(observed) != 3 || !strings.Contains(observed[0], "redis.call('INCRBY'") {
		t.Fatal("atomic quota script missing")
	}
}

func TestPhase04_RedisOversizedReplyRejected(t *testing.T) {
	addr := phase04Peer(t, func(args []string) string {
		if args[0] == "PING" {
			return "+PONG\r\n"
		}
		return "$99999999\r\n"
	})
	opts := DefaultRedisOptions(addr)
	opts.FailOpen = false
	r, err := NewRedisStore(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.GetEntry(context.Background(), "big"); err == nil {
		t.Fatal("oversized RESP allocation not rejected")
	}
}
