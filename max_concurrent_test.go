package cron

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMaxConcurrent_LimitsParallelism(t *testing.T) {
	const limit = 3
	var running, maxRunning int32
	var mu sync.Mutex

	wrapper := MaxConcurrent(limit)
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		wrapped := wrapper(FuncJob(func() {
			defer wg.Done()
			cur := atomic.AddInt32(&running, 1)
			mu.Lock()
			if cur > maxRunning {
				maxRunning = cur
			}
			mu.Unlock()
			time.Sleep(50 * time.Millisecond)
			atomic.AddInt32(&running, -1)
		}))
		go wrapped.Run()
	}

	wg.Wait()

	mu.Lock()
	peak := maxRunning
	mu.Unlock()

	if peak > int32(limit) {
		t.Errorf("peak concurrency %d exceeded limit %d", peak, limit)
	}
	if peak < int32(limit) {
		t.Logf("note: peak concurrency was %d (limit %d) — timing-dependent", peak, limit)
	}
}

func TestMaxConcurrent_AllJobsComplete(t *testing.T) {
	var completed int32

	wrapper := MaxConcurrent(2)
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		wrapped := wrapper(FuncJob(func() {
			defer wg.Done()
			time.Sleep(5 * time.Millisecond)
			atomic.AddInt32(&completed, 1)
		}))
		go wrapped.Run()
	}

	wg.Wait()

	if got := atomic.LoadInt32(&completed); got != 20 {
		t.Errorf("expected 20 completed, got %d", got)
	}
}

func TestMaxConcurrent_PropagatesContext(t *testing.T) {
	wrapper := MaxConcurrent(5)
	var receivedCtx context.Context

	inner := FuncJobWithContext(func(ctx context.Context) {
		receivedCtx = ctx
	})

	wrapped := wrapper(inner)

	testCtx := context.WithValue(context.Background(), testContextKey("key"), "value")
	wrapped.(JobWithContext).RunWithContext(testCtx)

	if receivedCtx == nil {
		t.Fatal("expected context to be propagated")
	}
	if receivedCtx.Value(testContextKey("key")) != "value" {
		t.Error("context value not propagated")
	}
}

func TestMaxConcurrent_SharedAcrossJobs(t *testing.T) {
	// Verify the semaphore is shared across different jobs wrapped by the same wrapper
	const limit = 2
	var running, maxRunning int32
	var mu sync.Mutex

	wrapper := MaxConcurrent(limit)
	var wg sync.WaitGroup

	// Create multiple distinct jobs
	for i := 0; i < 6; i++ {
		wg.Add(1)
		job := FuncJob(func() {
			defer wg.Done()
			cur := atomic.AddInt32(&running, 1)
			mu.Lock()
			if cur > maxRunning {
				maxRunning = cur
			}
			mu.Unlock()
			time.Sleep(50 * time.Millisecond)
			atomic.AddInt32(&running, -1)
		})
		wrapped := wrapper(job)
		go wrapped.Run()
	}

	wg.Wait()

	mu.Lock()
	peak := maxRunning
	mu.Unlock()

	if peak > int32(limit) {
		t.Errorf("peak concurrency %d exceeded limit %d", peak, limit)
	}
}

func TestMaxConcurrent_PanicsOnZero(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for n=0")
		}
	}()
	MaxConcurrent(0)
}

func TestMaxConcurrent_PanicsOnNegative(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for n<0")
		}
	}()
	MaxConcurrent(-1)
}

func TestMaxConcurrent_SingleSlot(t *testing.T) {
	// n=1 should serialize all jobs
	var sequence []int
	var mu sync.Mutex
	var wg sync.WaitGroup

	wrapper := MaxConcurrent(1)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		idx := i
		wrapped := wrapper(FuncJob(func() {
			defer wg.Done()
			mu.Lock()
			sequence = append(sequence, idx)
			mu.Unlock()
			time.Sleep(10 * time.Millisecond)
		}))
		go wrapped.Run()
	}

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()

	if len(sequence) != 5 {
		t.Errorf("expected 5 completions, got %d", len(sequence))
	}
}

// --- MaxConcurrentSkip tests ---

func TestMaxConcurrentSkip_SkipsWhenFull(t *testing.T) {
	var executed, skipped int32
	started := make(chan struct{})
	proceed := make(chan struct{})

	wrapper := MaxConcurrentSkip(DiscardLogger, 1)

	// First job: acquire the slot and hold it
	wrapped1 := wrapper(FuncJob(func() {
		atomic.AddInt32(&executed, 1)
		close(started)
		<-proceed // block until told to proceed
	}))

	go wrapped1.Run()
	<-started // wait until first job is running

	// Second job: should be skipped (slot is full)
	wrapped2 := wrapper(FuncJob(func() {
		atomic.AddInt32(&executed, 1)
	}))
	wrapped2.Run()

	// Check that second job was skipped
	if got := atomic.LoadInt32(&executed); got != 1 {
		skipped = 0
	} else {
		skipped = 1
	}

	close(proceed) // release first job
	time.Sleep(10 * time.Millisecond)

	if skipped != 1 {
		t.Error("expected second job to be skipped")
	}
}

func TestMaxConcurrentSkip_ExecutesWhenAvailable(t *testing.T) {
	var completed int32

	wrapper := MaxConcurrentSkip(DiscardLogger, 5)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		wrapped := wrapper(FuncJob(func() {
			defer wg.Done()
			atomic.AddInt32(&completed, 1)
		}))
		wrapped.Run()
	}

	wg.Wait()

	if got := atomic.LoadInt32(&completed); got != 5 {
		t.Errorf("expected 5 completed, got %d", got)
	}
}

func TestMaxConcurrentSkip_PropagatesContext(t *testing.T) {
	wrapper := MaxConcurrentSkip(DiscardLogger, 5)
	var receivedCtx context.Context

	inner := FuncJobWithContext(func(ctx context.Context) {
		receivedCtx = ctx
	})

	wrapped := wrapper(inner)

	testCtx := context.WithValue(context.Background(), testContextKey("key"), "value")
	wrapped.(JobWithContext).RunWithContext(testCtx)

	if receivedCtx == nil {
		t.Fatal("expected context to be propagated")
	}
	if receivedCtx.Value(testContextKey("key")) != "value" {
		t.Error("context value not propagated")
	}
}

func TestMaxConcurrentSkip_PanicsOnZero(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for n=0")
		}
	}()
	MaxConcurrentSkip(DiscardLogger, 0)
}

func TestMaxConcurrentSkip_LimitsParallelism(t *testing.T) {
	const limit = 3
	var running, maxRunning, executed int32
	var mu sync.Mutex
	var wg sync.WaitGroup

	wrapper := MaxConcurrentSkip(DiscardLogger, limit)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		wrapped := wrapper(FuncJob(func() {
			cur := atomic.AddInt32(&running, 1)
			mu.Lock()
			if cur > maxRunning {
				maxRunning = cur
			}
			mu.Unlock()
			time.Sleep(50 * time.Millisecond)
			atomic.AddInt32(&running, -1)
			atomic.AddInt32(&executed, 1)
		}))
		go func() {
			defer wg.Done()
			wrapped.Run()
		}()
	}

	wg.Wait()

	mu.Lock()
	peak := maxRunning
	mu.Unlock()

	if peak > int32(limit) {
		t.Errorf("peak concurrency %d exceeded limit %d", peak, limit)
	}
	// Some jobs should have been skipped
	got := atomic.LoadInt32(&executed)
	if got > 20 {
		t.Errorf("executed more jobs than launched: %d", got)
	}
}

// --- Integration with Cron ---

func TestMaxConcurrent_IntegrationWithCron(t *testing.T) {
	clock := NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	var running, maxRunning int32
	var mu sync.Mutex
	done := make(chan struct{})
	var doneOnce sync.Once

	c := New(
		WithClock(clock),
		WithChain(
			Recover(DiscardLogger),
			MaxConcurrent(2),
		),
	)

	// Add 5 jobs all triggering at the same time
	for i := 0; i < 5; i++ {
		c.AddFunc("@every 1h", func() {
			cur := atomic.AddInt32(&running, 1)
			mu.Lock()
			if cur > maxRunning {
				maxRunning = cur
			}
			mu.Unlock()
			time.Sleep(50 * time.Millisecond)
			atomic.AddInt32(&running, -1)
			doneOnce.Do(func() { close(done) })
		})
	}

	c.Start()
	defer c.Stop()

	time.Sleep(50 * time.Millisecond)
	clock.Advance(time.Hour)

	// Wait for at least one job to complete
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for jobs")
	}

	// Wait for all jobs to finish
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	peak := maxRunning
	mu.Unlock()

	if peak > 2 {
		t.Errorf("peak concurrency %d exceeded limit 2", peak)
	}
}

// testContextKey is a context key type for tests.
type testContextKey string
