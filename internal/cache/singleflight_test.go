package cache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSingleFlight_ConcurrentCoalescing(t *testing.T) {
	group := NewSingleFlightGroup()
	execCount := &atomic.Int64{}

	slowFunc := func(ctx context.Context) ([]byte, error) {
		execCount.Add(1)
		time.Sleep(50 * time.Millisecond)
		return []byte("coalesced-result"), nil
	}

	concurrency := 30
	var wg sync.WaitGroup
	results := make([][]byte, concurrency)
	errorsList := make([]error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx := context.Background()
			res, _, err := group.Do(ctx, "weather:london", 5*time.Second, slowFunc)
			results[idx] = res
			errorsList[idx] = err
		}(i)
	}

	wg.Wait()

	if execCount.Load() != 1 {
		t.Fatalf("expected exactly 1 execution across %d concurrent callers, got %d", concurrency, execCount.Load())
	}

	for i := 0; i < concurrency; i++ {
		if errorsList[i] != nil {
			t.Errorf("caller %d got error: %v", i, errorsList[i])
		}
		if string(results[i]) != "coalesced-result" {
			t.Errorf("caller %d got unexpected result: %s", i, string(results[i]))
		}
	}
}

func TestSingleFlight_LeaderCancellation_FollowersSucceed(t *testing.T) {
	group := NewSingleFlightGroup()

	workStarted := make(chan struct{})
	releaseWork := make(chan struct{})
	slowFunc := func(ctx context.Context) ([]byte, error) {
		close(workStarted)
		<-releaseWork
		return []byte("slow-success"), nil
	}

	leaderCtx, leaderCancel := context.WithCancel(context.Background())
	followerCtx := context.Background()

	var leaderRes []byte
	var leaderErr error
	var followerRes []byte
	var followerErr error

	var wg sync.WaitGroup
	wg.Add(2)
	leaderDone := make(chan struct{})

	// Leader
	go func() {
		defer wg.Done()
		defer close(leaderDone)
		leaderRes, _, leaderErr = group.Do(leaderCtx, "key-cancel", 5*time.Second, slowFunc)
	}()

	// Wait for the flight to start before attaching a follower.
	select {
	case <-workStarted:
	case <-time.After(time.Second):
		t.Fatal("leader did not start shared work")
	}

	// Follower
	go func() {
		defer wg.Done()
		followerRes, _, followerErr = group.Do(followerCtx, "key-cancel", 5*time.Second, slowFunc)
	}()

	// Wait until the follower is attached; sleeps can let the flight finish
	// before cancellation on a loaded machine.
	deadline := time.Now().Add(time.Second)
	for {
		group.mu.Lock()
		flight := group.calls["key-cancel"]
		joined := flight != nil && flight.waiters == 2
		group.mu.Unlock()
		if joined {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("follower did not join shared work")
		}
		time.Sleep(time.Millisecond)
	}

	// Cancel only the leader while the shared operation remains blocked.
	leaderCancel()
	select {
	case <-leaderDone:
	case <-time.After(time.Second):
		close(releaseWork)
		wg.Wait()
		t.Fatal("canceled leader did not return promptly")
	}
	close(releaseWork)

	wg.Wait()

	// Leader should have been canceled
	if !errors.Is(leaderErr, context.Canceled) {
		t.Errorf("expected leader to receive context.Canceled, got %v (res: %s)", leaderErr, string(leaderRes))
	}

	// Follower should have succeeded despite leader cancellation!
	if followerErr != nil {
		t.Errorf("expected follower to succeed, got error: %v", followerErr)
	}
	if string(followerRes) != "slow-success" {
		t.Errorf("expected follower to get 'slow-success', got %s", string(followerRes))
	}
}

func TestSingleFlight_ErrorPropagationAndNoPoisoning(t *testing.T) {
	group := NewSingleFlightGroup()
	execCount := &atomic.Int64{}

	errFailure := errors.New("upstream provider failed")
	failingFunc := func(ctx context.Context) ([]byte, error) {
		execCount.Add(1)
		return nil, errFailure
	}

	// 1. Initial failing execution
	res1, _, err1 := group.Do(context.Background(), "fail-key", 2*time.Second, failingFunc)
	if res1 != nil || !errors.Is(err1, errFailure) {
		t.Fatalf("expected failure, got (%v, %v)", res1, err1)
	}

	// 2. Subsequent call can execute and succeed (no permanent poisoning)
	successFunc := func(ctx context.Context) ([]byte, error) {
		execCount.Add(1)
		return []byte("recovered"), nil
	}

	res2, _, err2 := group.Do(context.Background(), "fail-key", 2*time.Second, successFunc)
	if err2 != nil || string(res2) != "recovered" {
		t.Fatalf("expected recovered success on subsequent call, got (%s, %v)", string(res2), err2)
	}

	if execCount.Load() != 2 {
		t.Errorf("expected 2 executions, got %d", execCount.Load())
	}
}

func TestSingleFlight_VariedConcurrencyLevels(t *testing.T) {
	levels := []int{2, 10, 50, 100}

	for _, concurrency := range levels {
		t.Run(fmt.Sprintf("Concurrency_%d", concurrency), func(t *testing.T) {
			group := NewSingleFlightGroup()
			execCount := &atomic.Int64{}

			fn := func(ctx context.Context) ([]byte, error) {
				execCount.Add(1)
				time.Sleep(30 * time.Millisecond)
				return []byte("payload"), nil
			}

			var wg sync.WaitGroup
			for i := 0; i < concurrency; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					res, _, err := group.Do(context.Background(), "same-key", 2*time.Second, fn)
					if err != nil || string(res) != "payload" {
						t.Errorf("unexpected result: %s, err: %v", string(res), err)
					}
				}()
			}
			wg.Wait()

			if execCount.Load() != 1 {
				t.Errorf("level %d: expected 1 execution, got %d", concurrency, execCount.Load())
			}
		})
	}
}

func TestSingleFlight_IndependentKeysDoNotBlock(t *testing.T) {
	group := NewSingleFlightGroup()
	var startedA, startedB atomic.Bool
	var wg sync.WaitGroup
	wg.Add(2)

	// Key A runs slow (50ms)
	go func() {
		defer wg.Done()
		_, _, _ = group.Do(context.Background(), "key-A", 2*time.Second, func(ctx context.Context) ([]byte, error) {
			startedA.Store(true)
			time.Sleep(50 * time.Millisecond)
			return []byte("res-A"), nil
		})
	}()

	// Key B runs fast (5ms)
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond) // Give A time to start
		_, _, _ = group.Do(context.Background(), "key-B", 2*time.Second, func(ctx context.Context) ([]byte, error) {
			startedB.Store(true)
			return []byte("res-B"), nil
		})
	}()

	wg.Wait()

	if !startedA.Load() || !startedB.Load() {
		t.Fatalf("both independent keys should execute, got A=%v, B=%v", startedA.Load(), startedB.Load())
	}
}

func TestPhase03_SingleFlightCancelsOrphanedWork(t *testing.T) {
	group := NewSingleFlightGroup()
	started := make(chan struct{})
	stopped := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, _, err := group.Do(ctx, "orphan", 2*time.Second, func(ctx context.Context) ([]byte, error) {
			close(started)
			<-ctx.Done()
			close(stopped)
			return nil, ctx.Err()
		})
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("flight never started")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("caller did not return")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("orphaned flight was not cancelled")
	}
	if group.InFlightCount() != 0 {
		t.Fatal("orphaned flight retained in map")
	}
}
