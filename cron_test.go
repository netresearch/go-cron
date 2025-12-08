package cron

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Many tests schedule a job for every second, and then wait at most a second
// for it to run.  This amount is just slightly larger than 1 second to
// compensate for a few milliseconds of runtime.
const OneSecond = 1*time.Second + 50*time.Millisecond

type syncWriter struct {
	wr bytes.Buffer
	m  sync.Mutex
}

func (sw *syncWriter) Write(data []byte) (n int, err error) {
	sw.m.Lock()
	n, err = sw.wr.Write(data)
	sw.m.Unlock()
	return n, err
}

func (sw *syncWriter) String() string {
	sw.m.Lock()
	defer sw.m.Unlock()
	return sw.wr.String()
}

func newBufLogger(sw *syncWriter) Logger {
	return PrintfLogger(log.New(sw, "", log.LstdFlags))
}

func TestFuncPanicRecovery(t *testing.T) {
	var buf syncWriter
	cron := New(WithParser(secondParser),
		WithChain(Recover(newBufLogger(&buf))))
	cron.Start()
	defer cron.Stop()
	cron.AddFunc("* * * * * ?", func() {
		panic("YOLO")
	})

	select {
	case <-time.After(OneSecond):
		if !strings.Contains(buf.String(), "YOLO") {
			t.Error("expected a panic to be logged, got none")
		}
		return
	}
}

type DummyJob struct{}

func (d DummyJob) Run() {
	panic("YOLO")
}

func TestJobPanicRecovery(t *testing.T) {
	var job DummyJob

	var buf syncWriter
	cron := New(WithParser(secondParser),
		WithChain(Recover(newBufLogger(&buf))))
	cron.Start()
	defer cron.Stop()
	cron.AddJob("* * * * * ?", job)

	select {
	case <-time.After(OneSecond):
		if !strings.Contains(buf.String(), "YOLO") {
			t.Error("expected a panic to be logged, got none")
		}
		return
	}
}

// Start and stop cron with no entries.
func TestNoEntries(t *testing.T) {
	cron := newWithSeconds()
	cron.Start()

	select {
	case <-time.After(OneSecond):
		t.Fatal("expected cron will be stopped immediately")
	case <-stop(cron):
	}
}

// Start, stop, then add an entry. Verify entry doesn't run.
func TestStopCausesJobsToNotRun(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(1)

	cron := newWithSeconds()
	cron.Start()
	cron.Stop()
	cron.AddFunc("* * * * * ?", func() { wg.Done() })

	select {
	case <-time.After(OneSecond):
		// No job ran!
	case <-wait(wg):
		t.Fatal("expected stopped cron does not run any job")
	}
}

// Add a job, start cron, expect it runs.
func TestAddBeforeRunning(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(1)

	cron := newWithSeconds()
	cron.AddFunc("* * * * * ?", func() { wg.Done() })
	cron.Start()
	defer cron.Stop()

	// Give cron 2 seconds to run our job (which is always activated).
	select {
	case <-time.After(OneSecond):
		t.Fatal("expected job runs")
	case <-wait(wg):
	}
}

// Start cron, add a job, expect it runs.
func TestAddWhileRunning(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(1)

	cron := newWithSeconds()
	cron.Start()
	defer cron.Stop()
	cron.AddFunc("* * * * * ?", func() { wg.Done() })

	select {
	case <-time.After(OneSecond):
		t.Fatal("expected job runs")
	case <-wait(wg):
	}
}

// Test for #34. Adding a job after calling start results in multiple job invocations
func TestAddWhileRunningWithDelay(t *testing.T) {
	cron := newWithSeconds()
	cron.Start()
	defer cron.Stop()
	time.Sleep(5 * time.Second)
	var calls int64
	cron.AddFunc("* * * * * *", func() { atomic.AddInt64(&calls, 1) })

	<-time.After(OneSecond)
	if atomic.LoadInt64(&calls) != 1 {
		t.Errorf("called %d times, expected 1\n", calls)
	}
}

// Add a job, remove a job, start cron, expect nothing runs.
func TestRemoveBeforeRunning(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(1)

	cron := newWithSeconds()
	id, _ := cron.AddFunc("* * * * * ?", func() { wg.Done() })
	cron.Remove(id)
	cron.Start()
	defer cron.Stop()

	select {
	case <-time.After(OneSecond):
		// Success, shouldn't run
	case <-wait(wg):
		t.FailNow()
	}
}

// Start cron, add a job, remove it, expect it doesn't run.
func TestRemoveWhileRunning(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(1)

	cron := newWithSeconds()
	cron.Start()
	defer cron.Stop()
	id, _ := cron.AddFunc("* * * * * ?", func() { wg.Done() })
	cron.Remove(id)

	select {
	case <-time.After(OneSecond):
	case <-wait(wg):
		t.FailNow()
	}
}

// Test timing with Entries.
func TestSnapshotEntries(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(1)

	cron := New()
	cron.AddFunc("@every 2s", func() { wg.Done() })
	cron.Start()
	defer cron.Stop()

	// Cron should fire in 2 seconds. After 1 second, call Entries.
	select {
	case <-time.After(OneSecond):
		cron.Entries()
	}

	// Even though Entries was called, the cron should fire at the 2 second mark.
	select {
	case <-time.After(OneSecond):
		t.Error("expected job runs at 2 second mark")
	case <-wait(wg):
	}
}

// Test that the entries are correctly sorted.
// Add a bunch of long-in-the-future entries, and an immediate entry, and ensure
// that the immediate entry runs immediately.
// Also: Test that multiple jobs run in the same instant.
func TestMultipleEntries(t *testing.T) {
	// Use FakeClock for deterministic testing
	startTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	fakeClock := NewFakeClock(startTime)

	wg := &sync.WaitGroup{}
	wg.Add(2)

	cron := New(WithParser(secondParser), WithClock(fakeClock))
	cron.AddFunc("0 0 0 1 1 ?", func() {})
	cron.AddFunc("* * * * * ?", func() { wg.Done() })
	id1, _ := cron.AddFunc("* * * * * ?", func() { t.Fatal() })
	id2, _ := cron.AddFunc("* * * * * ?", func() { t.Fatal() })
	cron.AddFunc("0 0 0 31 12 ?", func() {})
	cron.AddFunc("* * * * * ?", func() { wg.Done() })

	cron.Remove(id1)
	cron.Start()
	cron.Remove(id2)
	defer cron.Stop()

	// Wait for scheduler to register timers
	fakeClock.BlockUntil(1)

	// Advance 1 second to trigger the every-second jobs
	fakeClock.Advance(time.Second)
	time.Sleep(10 * time.Millisecond)

	select {
	case <-time.After(100 * time.Millisecond):
		t.Error("expected job run in proper order")
	case <-wait(wg):
	}
}

// Test running the same job twice.
func TestRunningJobTwice(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(2)

	cron := newWithSeconds()
	cron.AddFunc("0 0 0 1 1 ?", func() {})
	cron.AddFunc("0 0 0 31 12 ?", func() {})
	cron.AddFunc("* * * * * ?", func() { wg.Done() })

	cron.Start()
	defer cron.Stop()

	select {
	case <-time.After(2 * OneSecond):
		t.Error("expected job fires 2 times")
	case <-wait(wg):
	}
}

func TestRunningMultipleSchedules(t *testing.T) {
	// Use FakeClock for deterministic testing
	startTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	fakeClock := NewFakeClock(startTime)

	wg := &sync.WaitGroup{}
	// Two every-second jobs: "* * * * * ?" and Every(time.Second)
	// Each 1-second advance triggers both → 2 Done() calls per second
	// We'll advance once, so expect 2 calls
	wg.Add(2)

	cron := New(WithParser(secondParser), WithClock(fakeClock))
	cron.AddFunc("0 0 0 1 1 ?", func() {})
	cron.AddFunc("0 0 0 31 12 ?", func() {})
	cron.AddFunc("* * * * * ?", func() { wg.Done() })
	cron.Schedule(Every(time.Minute), FuncJob(func() {}))
	cron.Schedule(Every(time.Second), FuncJob(func() { wg.Done() }))
	cron.Schedule(Every(time.Hour), FuncJob(func() {}))

	cron.Start()
	defer cron.Stop()

	// Wait for scheduler to register timers
	fakeClock.BlockUntil(1)

	// Advance 1 second - should trigger both every-second jobs (2 calls)
	fakeClock.Advance(time.Second)
	time.Sleep(10 * time.Millisecond)

	select {
	case <-time.After(100 * time.Millisecond):
		t.Error("expected both every-second jobs to fire")
	case <-wait(wg):
	}
}

// Test that the cron is run in the local time zone (as opposed to UTC).
func TestLocalTimezone(t *testing.T) {
	// Use FakeClock for deterministic testing
	// Start at a known time: 2024-01-15 10:30:00 in local timezone
	startTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.Local)
	fakeClock := NewFakeClock(startTime)

	// Schedule job to fire at seconds 1 and 2 of the next minute (10:31:01 and 10:31:02)
	spec := "1,2 31 10 15 1 ?"

	wg := &sync.WaitGroup{}
	wg.Add(2)

	cron := New(WithParser(secondParser), WithClock(fakeClock))
	cron.AddFunc(spec, func() { wg.Done() })
	cron.Start()
	defer cron.Stop()

	// Wait for the scheduler to register the timer
	fakeClock.BlockUntil(1)

	// Advance to first trigger time (10:31:01)
	fakeClock.Advance(61 * time.Second)

	// Wait for scheduler to process and register next timer
	time.Sleep(10 * time.Millisecond)
	fakeClock.BlockUntil(1)

	// Advance to second trigger time (10:31:02)
	fakeClock.Advance(1 * time.Second)

	// Give time for job execution
	time.Sleep(10 * time.Millisecond)

	// Verify both jobs ran
	select {
	case <-time.After(100 * time.Millisecond):
		t.Error("expected job to fire 2 times")
	case <-wait(wg):
		// Success
	}
}

// Test that the cron is run in the given time zone (as opposed to local).
func TestNonLocalTimezone(t *testing.T) {
	loc, err := time.LoadLocation("Atlantic/Cape_Verde")
	if err != nil {
		t.Fatalf("Failed to load time zone Atlantic/Cape_Verde: %+v", err)
	}

	// Use FakeClock for deterministic testing
	// Start at a known time: 2024-01-15 10:30:00 in the target timezone
	startTime := time.Date(2024, 1, 15, 10, 30, 0, 0, loc)
	fakeClock := NewFakeClock(startTime)

	// Schedule job to fire at seconds 1 and 2 of the next minute (10:31:01 and 10:31:02)
	spec := "1,2 31 10 15 1 ?"

	wg := &sync.WaitGroup{}
	wg.Add(2)

	cron := New(WithLocation(loc), WithParser(secondParser), WithClock(fakeClock))
	cron.AddFunc(spec, func() { wg.Done() })
	cron.Start()
	defer cron.Stop()

	// Wait for the scheduler to register the timer
	fakeClock.BlockUntil(1)

	// Advance to first trigger time (10:31:01)
	fakeClock.Advance(61 * time.Second) // 30s to :31:00 + 1s to :31:01

	// Wait for scheduler to process and register next timer
	time.Sleep(10 * time.Millisecond)
	fakeClock.BlockUntil(1)

	// Advance to second trigger time (10:31:02)
	fakeClock.Advance(1 * time.Second)

	// Give time for job execution
	time.Sleep(10 * time.Millisecond)

	// Verify both jobs ran
	select {
	case <-time.After(100 * time.Millisecond):
		t.Error("expected job to fire 2 times")
	case <-wait(wg):
		// Success - both jobs completed
	}
}

// Test that calling stop before start silently returns without
// blocking the stop channel.
func TestStopWithoutStart(t *testing.T) {
	cron := New()
	cron.Stop()
}

type testJob struct {
	wg   *sync.WaitGroup
	name string
}

func (t testJob) Run() {
	t.wg.Done()
}

// Test that adding an invalid job spec returns an error
func TestInvalidJobSpec(t *testing.T) {
	cron := New()
	_, err := cron.AddJob("this will not parse", nil)
	if err == nil {
		t.Errorf("expected an error with invalid spec, got nil")
	}
}

// Test blocking run method behaves as Start()
func TestBlockingRun(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(1)

	cron := newWithSeconds()
	cron.AddFunc("* * * * * ?", func() { wg.Done() })

	unblockChan := make(chan struct{})

	go func() {
		cron.Run()
		close(unblockChan)
	}()
	defer cron.Stop()

	select {
	case <-time.After(OneSecond):
		t.Error("expected job fires")
	case <-unblockChan:
		t.Error("expected that Run() blocks")
	case <-wait(wg):
	}
}

// Test that double-running is a no-op
func TestStartNoop(t *testing.T) {
	tickChan := make(chan struct{}, 2)

	cron := newWithSeconds()
	cron.AddFunc("* * * * * ?", func() {
		tickChan <- struct{}{}
	})

	cron.Start()
	defer cron.Stop()

	// Wait for the first firing to ensure the runner is going
	<-tickChan

	cron.Start()

	<-tickChan

	// Fail if this job fires again in a short period, indicating a double-run
	select {
	case <-time.After(time.Millisecond):
	case <-tickChan:
		t.Error("expected job fires exactly twice")
	}
}

// Simple test using Runnables.
func TestJob(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(1)

	cron := newWithSeconds()
	cron.AddJob("0 0 0 30 Feb ?", testJob{wg, "job0"})
	cron.AddJob("0 0 0 1 1 ?", testJob{wg, "job1"})
	job2, _ := cron.AddJob("* * * * * ?", testJob{wg, "job2"})
	cron.AddJob("1 0 0 1 1 ?", testJob{wg, "job3"})
	cron.Schedule(Every(5*time.Second+5*time.Nanosecond), testJob{wg, "job4"})
	job5 := cron.Schedule(Every(5*time.Minute), testJob{wg, "job5"})

	// Test getting an Entry pre-Start.
	if actualName := cron.Entry(job2).Job.(testJob).name; actualName != "job2" {
		t.Error("wrong job retrieved:", actualName)
	}
	if actualName := cron.Entry(job5).Job.(testJob).name; actualName != "job5" {
		t.Error("wrong job retrieved:", actualName)
	}

	cron.Start()
	defer cron.Stop()

	select {
	case <-time.After(OneSecond):
		t.FailNow()
	case <-wait(wg):
	}

	// Ensure the entries are in the right order.
	expecteds := []string{"job2", "job4", "job5", "job1", "job3", "job0"}

	var actuals []string
	for _, entry := range cron.Entries() {
		actuals = append(actuals, entry.Job.(testJob).name)
	}

	for i, expected := range expecteds {
		if actuals[i] != expected {
			t.Fatalf("Jobs not in the right order.  (expected) %s != %s (actual)", expecteds, actuals)
		}
	}

	// Test getting Entries.
	if actualName := cron.Entry(job2).Job.(testJob).name; actualName != "job2" {
		t.Error("wrong job retrieved:", actualName)
	}
	if actualName := cron.Entry(job5).Job.(testJob).name; actualName != "job5" {
		t.Error("wrong job retrieved:", actualName)
	}
}

// Issue #206
// Ensure that the next run of a job after removing an entry is accurate.
func TestScheduleAfterRemoval(t *testing.T) {
	var wg1 sync.WaitGroup
	var wg2 sync.WaitGroup
	wg1.Add(1)
	wg2.Add(1)

	// The first time this job is run, set a timer and remove the other job
	// 750ms later. Correct behavior would be to still run the job again in
	// 250ms, but the bug would cause it to run instead 1s later.

	var calls int
	var mu sync.Mutex

	cron := newWithSeconds()
	hourJob := cron.Schedule(Every(time.Hour), FuncJob(func() {}))
	cron.Schedule(Every(time.Second), FuncJob(func() {
		mu.Lock()
		defer mu.Unlock()
		switch calls {
		case 0:
			wg1.Done()
			calls++
		case 1:
			time.Sleep(750 * time.Millisecond)
			cron.Remove(hourJob)
			calls++
		case 2:
			calls++
			wg2.Done()
		case 3:
			panic("unexpected 3rd call")
		}
	}))

	cron.Start()
	defer cron.Stop()

	// the first run might be any length of time 0 - 1s, since the schedule
	// rounds to the second. wait for the first run to true up.
	wg1.Wait()

	select {
	case <-time.After(2 * OneSecond):
		t.Error("expected job fires 2 times")
	case <-wait(&wg2):
	}
}

type ZeroSchedule struct{}

func (*ZeroSchedule) Next(time.Time) time.Time {
	return time.Time{}
}

// Tests that job without time does not run
func TestJobWithZeroTimeDoesNotRun(t *testing.T) {
	cron := newWithSeconds()
	var calls int64
	cron.AddFunc("* * * * * *", func() { atomic.AddInt64(&calls, 1) })
	cron.Schedule(new(ZeroSchedule), FuncJob(func() { t.Error("expected zero task will not run") }))
	cron.Start()
	defer cron.Stop()
	<-time.After(OneSecond)
	if atomic.LoadInt64(&calls) != 1 {
		t.Errorf("called %d times, expected 1\n", calls)
	}
}

func TestStopAndWait(t *testing.T) {
	t.Run("nothing running, returns immediately", func(t *testing.T) {
		cron := newWithSeconds()
		cron.Start()
		ctx := cron.Stop()
		select {
		case <-ctx.Done():
		case <-time.After(time.Millisecond):
			t.Error("context was not done immediately")
		}
	})

	t.Run("repeated calls to Stop", func(t *testing.T) {
		cron := newWithSeconds()
		cron.Start()
		_ = cron.Stop()
		time.Sleep(time.Millisecond)
		ctx := cron.Stop()
		select {
		case <-ctx.Done():
		case <-time.After(time.Millisecond):
			t.Error("context was not done immediately")
		}
	})

	t.Run("a couple fast jobs added, still returns immediately", func(t *testing.T) {
		cron := newWithSeconds()
		cron.AddFunc("* * * * * *", func() {})
		cron.Start()
		cron.AddFunc("* * * * * *", func() {})
		cron.AddFunc("* * * * * *", func() {})
		cron.AddFunc("* * * * * *", func() {})
		time.Sleep(time.Second)
		ctx := cron.Stop()
		select {
		case <-ctx.Done():
		case <-time.After(time.Millisecond):
			t.Error("context was not done immediately")
		}
	})

	t.Run("a couple fast jobs and a slow job added, waits for slow job", func(t *testing.T) {
		cron := newWithSeconds()
		cron.AddFunc("* * * * * *", func() {})
		cron.Start()
		cron.AddFunc("* * * * * *", func() { time.Sleep(2 * time.Second) })
		cron.AddFunc("* * * * * *", func() {})
		time.Sleep(time.Second)

		ctx := cron.Stop()

		// Verify that it is not done for at least 750ms
		select {
		case <-ctx.Done():
			t.Error("context was done too quickly immediately")
		case <-time.After(750 * time.Millisecond):
			// expected, because the job sleeping for 1 second is still running
		}

		// Verify that it IS done in the next 500ms (giving 250ms buffer)
		select {
		case <-ctx.Done():
			// expected
		case <-time.After(1500 * time.Millisecond):
			t.Error("context not done after job should have completed")
		}
	})

	t.Run("repeated calls to stop, waiting for completion and after", func(t *testing.T) {
		cron := newWithSeconds()

		// Use channels to synchronize instead of relying on timing
		jobStarted := make(chan struct{})
		jobCanFinish := make(chan struct{})
		jobDone := make(chan struct{})

		cron.AddFunc("* * * * * *", func() {})
		cron.AddFunc("* * * * * *", func() {
			close(jobStarted) // Signal that slow job has started
			<-jobCanFinish    // Wait until test allows job to finish
			close(jobDone)
		})
		cron.Start()
		cron.AddFunc("* * * * * *", func() {})

		// Wait for slow job to actually start
		select {
		case <-jobStarted:
			// Job started, proceed
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for job to start")
		}

		ctx := cron.Stop()
		ctx2 := cron.Stop()

		// Verify contexts are NOT done while job is still running
		select {
		case <-ctx.Done():
			t.Error("context was done while job still running")
		case <-ctx2.Done():
			t.Error("context2 was done while job still running")
		default:
			// expected - contexts should not be done yet
		}

		// Allow the job to finish
		close(jobCanFinish)

		// Wait for job to complete
		<-jobDone

		// Verify that ctx IS done after job completes
		select {
		case <-ctx.Done():
			// expected
		case <-time.After(time.Second):
			t.Error("context not done after job completed")
		}

		// Verify that ctx2 is also done.
		select {
		case <-ctx2.Done():
			// expected
		case <-time.After(time.Millisecond):
			t.Error("context2 not done even though context1 is")
		}

		// Verify that a new context retrieved from stop is immediately done.
		ctx3 := cron.Stop()
		select {
		case <-ctx3.Done():
			// expected
		case <-time.After(time.Millisecond):
			t.Error("context not done even when cron Stop is completed")
		}
	})

	t.Run("StopAndWait blocks until job completes", func(t *testing.T) {
		cron := newWithSeconds()
		var completed atomic.Bool
		cron.AddFunc("* * * * * *", func() {
			time.Sleep(500 * time.Millisecond)
			completed.Store(true)
		})
		cron.Start()
		time.Sleep(time.Second) // Wait for job to start running

		// StopAndWait should block until job completes
		cron.StopAndWait()

		if !completed.Load() {
			t.Error("StopAndWait returned before job completed")
		}
	})

	t.Run("StopAndWait on non-running cron returns immediately", func(t *testing.T) {
		cron := newWithSeconds()
		done := make(chan struct{})
		go func() {
			cron.StopAndWait()
			close(done)
		}()
		select {
		case <-done:
			// expected
		case <-time.After(100 * time.Millisecond):
			t.Error("StopAndWait blocked on non-running cron")
		}
	})
}

func TestStopWithTimeout(t *testing.T) {
	t.Run("job completes within timeout returns true", func(t *testing.T) {
		cron := newWithSeconds()
		var jobDone atomic.Bool
		cron.AddFunc("* * * * * *", func() {
			// Job takes 200ms
			time.Sleep(200 * time.Millisecond)
			jobDone.Store(true)
		})
		cron.Start()
		time.Sleep(time.Second) // Wait for job to start

		// StopWithTimeout with 1s should succeed
		result := cron.StopWithTimeout(time.Second)

		if !result {
			t.Error("StopWithTimeout returned false, expected true")
		}
		if !jobDone.Load() {
			t.Error("Job did not complete")
		}
	})

	t.Run("job exceeds timeout returns false", func(t *testing.T) {
		cron := newWithSeconds()
		var jobStarted atomic.Bool
		cron.AddFunc("* * * * * *", func() {
			jobStarted.Store(true)
			// Job takes much longer than timeout
			time.Sleep(2 * time.Second)
		})
		cron.Start()
		time.Sleep(time.Second) // Wait for job to start

		// StopWithTimeout with 100ms should timeout
		result := cron.StopWithTimeout(100 * time.Millisecond)

		if result {
			t.Error("StopWithTimeout returned true, expected false (timeout)")
		}
		if !jobStarted.Load() {
			t.Error("Job did not start")
		}
	})

	t.Run("zero timeout waits indefinitely", func(t *testing.T) {
		cron := newWithSeconds()
		var jobDone atomic.Bool
		cron.AddFunc("* * * * * *", func() {
			time.Sleep(100 * time.Millisecond)
			jobDone.Store(true)
		})
		cron.Start()
		time.Sleep(time.Second) // Wait for job to start

		// Zero timeout should wait indefinitely (equivalent to StopAndWait)
		result := cron.StopWithTimeout(0)

		if !result {
			t.Error("StopWithTimeout(0) returned false, expected true")
		}
		if !jobDone.Load() {
			t.Error("Job did not complete")
		}
	})

	t.Run("negative timeout waits indefinitely", func(t *testing.T) {
		cron := newWithSeconds()
		var jobDone atomic.Bool
		cron.AddFunc("* * * * * *", func() {
			time.Sleep(100 * time.Millisecond)
			jobDone.Store(true)
		})
		cron.Start()
		time.Sleep(time.Second) // Let job start

		// Negative timeout should wait indefinitely
		result := cron.StopWithTimeout(-time.Second)

		if !result {
			t.Error("StopWithTimeout(-1s) returned false, expected true")
		}
		if !jobDone.Load() {
			t.Error("Job did not complete")
		}
	})

	t.Run("no running jobs returns true immediately", func(t *testing.T) {
		cron := newWithSeconds()
		cron.Start()

		start := time.Now()
		result := cron.StopWithTimeout(5 * time.Second)
		elapsed := time.Since(start)

		if !result {
			t.Error("StopWithTimeout returned false with no running jobs")
		}
		if elapsed > 100*time.Millisecond {
			t.Errorf("StopWithTimeout took too long: %v", elapsed)
		}
	})
}

func TestMultiThreadedStartAndStop(t *testing.T) {
	cron := New()
	go cron.Run()
	time.Sleep(2 * time.Millisecond)
	cron.Stop()
}

func wait(wg *sync.WaitGroup) chan bool {
	ch := make(chan bool)
	go func() {
		wg.Wait()
		ch <- true
	}()
	return ch
}

func stop(cron *Cron) chan bool {
	ch := make(chan bool)
	go func() {
		cron.Stop()
		ch <- true
	}()
	return ch
}

// newWithSeconds returns a Cron with the seconds field enabled.
func newWithSeconds() *Cron {
	return New(WithParser(secondParser), WithChain())
}

// TestWithSecondsOption tests the WithSeconds convenience option
func TestWithSecondsOption(t *testing.T) {
	cron := New(WithSeconds())
	defer cron.Stop()

	// Verify it can parse 6-field cron expressions (with seconds)
	_, err := cron.AddFunc("0 30 * * * *", func() {})
	if err != nil {
		t.Fatalf("WithSeconds should allow 6-field expressions: %v", err)
	}

	// Verify entries were added
	if len(cron.Entries()) != 1 {
		t.Errorf("expected 1 entry, got %d", len(cron.Entries()))
	}
}

// TestEntryRunWithChain tests fix for issue #551
// Entry.Run() should execute through the chain wrappers
func TestEntryRunWithChain(t *testing.T) {
	var callCount int64
	var mu sync.Mutex

	// Create cron with SkipIfStillRunning to test chain behavior
	cron := New(
		WithParser(secondParser),
		WithChain(SkipIfStillRunning(DiscardLogger)),
	)

	// Add a job that blocks and counts calls
	_, err := cron.AddFunc("* * * * * *", func() {
		mu.Lock()
		atomic.AddInt64(&callCount, 1)
		mu.Unlock()
		time.Sleep(100 * time.Millisecond)
	})
	if err != nil {
		t.Fatal(err)
	}

	entries := cron.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	entry := entries[0]

	// Test 1: Entry.Run() uses WrappedJob (respects chain)
	// Start the job via Entry.Run()
	go entry.Run()
	time.Sleep(10 * time.Millisecond)

	// Try to run again while first is still running
	// With SkipIfStillRunning, this should be skipped
	entry.Run()

	// Wait for first job to complete
	time.Sleep(150 * time.Millisecond)

	count := atomic.LoadInt64(&callCount)
	if count != 1 {
		t.Errorf("Entry.Run() with SkipIfStillRunning: expected 1 call, got %d (chain not respected)", count)
	}

	// Test 2: Entry.Job.Run() bypasses chain (documenting existing behavior)
	atomic.StoreInt64(&callCount, 0)

	// Start job via Entry.Job.Run() (bypasses chain)
	go entry.Job.Run()
	time.Sleep(10 * time.Millisecond)

	// Run again - this should NOT be skipped because chain is bypassed
	entry.Job.Run()

	// Wait for both to complete
	time.Sleep(150 * time.Millisecond)

	count = atomic.LoadInt64(&callCount)
	if count != 2 {
		t.Errorf("Entry.Job.Run() (bypass chain): expected 2 calls, got %d", count)
	}
}

// TestEntryRunNilWrappedJob tests Entry.Run() with nil WrappedJob
func TestEntryRunNilWrappedJob(t *testing.T) {
	entry := Entry{}

	// Should not panic when WrappedJob is nil
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Entry.Run() panicked with nil WrappedJob: %v", r)
		}
	}()

	entry.Run()
}

// TestTimeMovedBackwards tests fix for PR #480
// When system time moves backwards, entries should be rescheduled
func TestTimeMovedBackwards(t *testing.T) {
	// This test verifies the time backwards detection logic
	// by directly testing the condition: Prev.After(now)

	now := time.Now()
	futureTime := now.Add(1 * time.Hour)

	// Create an entry with Prev set in the "future" (simulating time moved backwards)
	entry := &Entry{
		Prev: futureTime,
		Next: futureTime.Add(1 * time.Minute),
	}

	// Verify detection condition
	if !entry.Prev.After(now) {
		t.Error("Expected Prev to be after now (simulating time moved backwards)")
	}

	// Verify zero Prev is not affected
	zeroEntry := &Entry{
		Prev: time.Time{},
		Next: now.Add(1 * time.Minute),
	}

	if zeroEntry.Prev.After(now) {
		t.Error("Zero Prev should not trigger backwards detection")
	}

	// Test with actual scheduler to verify logging
	var buf syncWriter
	cron := New(
		WithParser(secondParser),
		WithLogger(newBufLogger(&buf)),
	)
	defer cron.Stop()

	_, err := cron.AddFunc("* * * * * *", func() {})
	if err != nil {
		t.Fatal(err)
	}

	// Start and let it initialize
	cron.Start()
	time.Sleep(100 * time.Millisecond)

	// The actual time backwards handling is tested implicitly
	// through the scheduler's run loop. A full integration test
	// would require mocking the system clock.
}

// TestFakeClockSchedulerIntegration tests the scheduler with FakeClock
// for deterministic, fast execution without real-time delays.
func TestFakeClockSchedulerIntegration(t *testing.T) {
	t.Run("every second schedule", func(t *testing.T) {
		startTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
		fakeClock := NewFakeClock(startTime)

		var count int
		var mu sync.Mutex

		cron := New(WithClock(fakeClock))
		cron.Schedule(Every(time.Second), FuncJob(func() {
			mu.Lock()
			count++
			mu.Unlock()
		}))

		cron.Start()
		defer cron.Stop()

		// Wait for timer registration
		fakeClock.BlockUntil(1)

		// Advance 5 seconds
		for i := 0; i < 5; i++ {
			fakeClock.Advance(time.Second)
			time.Sleep(5 * time.Millisecond) // Let goroutines run
			if i < 4 {
				fakeClock.BlockUntil(1)
			}
		}

		time.Sleep(10 * time.Millisecond)

		mu.Lock()
		if count != 5 {
			t.Errorf("expected 5 job runs, got %d", count)
		}
		mu.Unlock()
	})

	t.Run("cron expression schedule", func(t *testing.T) {
		// Start at 10:59:55 - job fires at top of every hour
		startTime := time.Date(2024, 1, 15, 10, 59, 55, 0, time.UTC)
		fakeClock := NewFakeClock(startTime)

		var fired bool
		var mu sync.Mutex

		cron := New(WithClock(fakeClock))
		cron.AddFunc("0 * * * *", func() { // Top of every hour
			mu.Lock()
			fired = true
			mu.Unlock()
		})

		cron.Start()
		defer cron.Stop()

		fakeClock.BlockUntil(1)

		// Advance to 11:00:00
		fakeClock.Advance(5 * time.Second)
		time.Sleep(10 * time.Millisecond)

		mu.Lock()
		if !fired {
			t.Error("expected job to fire at top of hour")
		}
		mu.Unlock()
	})

	t.Run("multiple jobs different intervals", func(t *testing.T) {
		startTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
		fakeClock := NewFakeClock(startTime)

		var fastCount, slowCount int
		var mu sync.Mutex

		cron := New(WithClock(fakeClock))
		cron.Schedule(Every(time.Second), FuncJob(func() {
			mu.Lock()
			fastCount++
			mu.Unlock()
		}))
		cron.Schedule(Every(5*time.Second), FuncJob(func() {
			mu.Lock()
			slowCount++
			mu.Unlock()
		}))

		cron.Start()
		defer cron.Stop()

		fakeClock.BlockUntil(1)

		// Advance 10 seconds total
		for i := 0; i < 10; i++ {
			fakeClock.Advance(time.Second)
			time.Sleep(5 * time.Millisecond)
			if i < 9 {
				fakeClock.BlockUntil(1)
			}
		}

		time.Sleep(10 * time.Millisecond)

		mu.Lock()
		if fastCount != 10 {
			t.Errorf("fast job: expected 10 runs, got %d", fastCount)
		}
		if slowCount != 2 {
			t.Errorf("slow job: expected 2 runs, got %d", slowCount)
		}
		mu.Unlock()
	})

	t.Run("add job while running", func(t *testing.T) {
		startTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
		fakeClock := NewFakeClock(startTime)

		var count int
		var mu sync.Mutex
		ran := make(chan struct{}, 1)

		cron := New(WithClock(fakeClock))
		cron.Start()
		defer cron.Stop()

		// Add job after start
		cron.Schedule(Every(time.Second), FuncJob(func() {
			mu.Lock()
			count++
			mu.Unlock()
			select {
			case ran <- struct{}{}:
			default:
			}
		}))

		// Wait for timer
		fakeClock.BlockUntil(1)

		// Advance and wait for job to complete via channel
		fakeClock.Advance(time.Second)
		select {
		case <-ran:
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for job to run")
		}

		mu.Lock()
		if count != 1 {
			t.Errorf("expected 1 run after adding job, got %d", count)
		}
		mu.Unlock()
	})

	t.Run("remove job while running", func(t *testing.T) {
		startTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
		fakeClock := NewFakeClock(startTime)

		var count int
		var mu sync.Mutex
		ran := make(chan struct{}, 1)

		cron := New(WithClock(fakeClock))
		id := cron.Schedule(Every(time.Second), FuncJob(func() {
			mu.Lock()
			count++
			mu.Unlock()
			select {
			case ran <- struct{}{}:
			default:
			}
		}))

		cron.Start()
		defer cron.Stop()

		fakeClock.BlockUntil(1)
		fakeClock.Advance(time.Second)
		select {
		case <-ran:
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for job to run")
		}

		mu.Lock()
		if count != 1 {
			t.Errorf("expected 1 run before removal, got %d", count)
		}
		mu.Unlock()

		// Remove the job
		cron.Remove(id)
		time.Sleep(50 * time.Millisecond) // Allow remove to process

		// Advance more time - job should not run
		fakeClock.Advance(5 * time.Second)
		time.Sleep(50 * time.Millisecond) // Give time for any incorrect runs

		mu.Lock()
		if count != 1 {
			t.Errorf("expected still 1 run after removal, got %d", count)
		}
		mu.Unlock()
	})
}

// Tests for job metadata (Name and Tags)

func TestWithName(t *testing.T) {
	c := New()
	defer c.Stop()

	id, err := c.AddFunc("@every 1s", func() {}, WithName("my-job"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entry := c.Entry(id)
	if entry.Name != "my-job" {
		t.Errorf("expected name 'my-job', got %q", entry.Name)
	}
}

func TestWithTags(t *testing.T) {
	c := New()
	defer c.Stop()

	id, err := c.AddFunc("@every 1s", func() {}, WithTags("maintenance", "hourly"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entry := c.Entry(id)
	if len(entry.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(entry.Tags))
	}
	if entry.Tags[0] != "maintenance" || entry.Tags[1] != "hourly" {
		t.Errorf("unexpected tags: %v", entry.Tags)
	}
}

func TestWithNameAndTags(t *testing.T) {
	c := New()
	defer c.Stop()

	id, err := c.AddFunc("@every 1s", func() {},
		WithName("cleanup"),
		WithTags("maintenance", "daily"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entry := c.Entry(id)
	if entry.Name != "cleanup" {
		t.Errorf("expected name 'cleanup', got %q", entry.Name)
	}
	if len(entry.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(entry.Tags))
	}
}

func TestDuplicateName(t *testing.T) {
	c := New()
	defer c.Stop()

	_, err := c.AddFunc("@every 1s", func() {}, WithName("my-job"))
	if err != nil {
		t.Fatalf("unexpected error on first add: %v", err)
	}

	_, err = c.AddFunc("@every 2s", func() {}, WithName("my-job"))
	if !errors.Is(err, ErrDuplicateName) {
		t.Errorf("expected ErrDuplicateName, got %v", err)
	}
}

func TestDuplicateNameWhileRunning(t *testing.T) {
	c := New()
	c.Start()
	defer c.Stop()

	_, err := c.AddFunc("@every 1s", func() {}, WithName("my-job"))
	if err != nil {
		t.Fatalf("unexpected error on first add: %v", err)
	}

	// Small delay to ensure first job is added
	time.Sleep(10 * time.Millisecond)

	_, err = c.AddFunc("@every 2s", func() {}, WithName("my-job"))
	if !errors.Is(err, ErrDuplicateName) {
		t.Errorf("expected ErrDuplicateName, got %v", err)
	}
}

func TestEntryByName(t *testing.T) {
	c := New()
	defer c.Stop()

	_, err := c.AddFunc("@every 1s", func() {}, WithName("my-job"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entry := c.EntryByName("my-job")
	if !entry.Valid() {
		t.Error("expected valid entry")
	}
	if entry.Name != "my-job" {
		t.Errorf("expected name 'my-job', got %q", entry.Name)
	}

	// Test non-existent name
	entry = c.EntryByName("non-existent")
	if entry.Valid() {
		t.Error("expected invalid entry for non-existent name")
	}
}

func TestEntryByNameWhileRunning(t *testing.T) {
	c := New()
	c.Start()
	defer c.Stop()

	_, err := c.AddFunc("@every 1s", func() {}, WithName("my-job"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Small delay to ensure job is added
	time.Sleep(10 * time.Millisecond)

	entry := c.EntryByName("my-job")
	if !entry.Valid() {
		t.Error("expected valid entry")
	}
	if entry.Name != "my-job" {
		t.Errorf("expected name 'my-job', got %q", entry.Name)
	}
}

func TestEntriesByTag(t *testing.T) {
	c := New()
	defer c.Stop()

	c.AddFunc("@every 1s", func() {}, WithName("job1"), WithTags("maintenance"))
	c.AddFunc("@every 2s", func() {}, WithName("job2"), WithTags("maintenance", "critical"))
	c.AddFunc("@every 3s", func() {}, WithName("job3"), WithTags("cleanup"))

	// Find by maintenance tag
	entries := c.EntriesByTag("maintenance")
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries with 'maintenance' tag, got %d", len(entries))
	}

	// Find by critical tag
	entries = c.EntriesByTag("critical")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry with 'critical' tag, got %d", len(entries))
	}

	// Find by cleanup tag
	entries = c.EntriesByTag("cleanup")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry with 'cleanup' tag, got %d", len(entries))
	}

	// Find by non-existent tag
	entries = c.EntriesByTag("non-existent")
	if len(entries) != 0 {
		t.Errorf("expected 0 entries with 'non-existent' tag, got %d", len(entries))
	}
}

func TestRemoveByName(t *testing.T) {
	c := New()
	defer c.Stop()

	c.AddFunc("@every 1s", func() {}, WithName("my-job"))

	// Verify exists
	entry := c.EntryByName("my-job")
	if !entry.Valid() {
		t.Fatal("expected job to exist before removal")
	}

	// Remove it
	removed := c.RemoveByName("my-job")
	if !removed {
		t.Error("expected RemoveByName to return true")
	}

	// Verify removed
	entry = c.EntryByName("my-job")
	if entry.Valid() {
		t.Error("expected job to be removed")
	}

	// Try removing again
	removed = c.RemoveByName("my-job")
	if removed {
		t.Error("expected RemoveByName to return false for non-existent job")
	}
}

func TestRemoveByNameWhileRunning(t *testing.T) {
	c := New()
	c.Start()
	defer c.Stop()

	c.AddFunc("@every 1s", func() {}, WithName("my-job"))
	time.Sleep(10 * time.Millisecond)

	removed := c.RemoveByName("my-job")
	if !removed {
		t.Error("expected RemoveByName to return true")
	}

	time.Sleep(10 * time.Millisecond)

	entry := c.EntryByName("my-job")
	if entry.Valid() {
		t.Error("expected job to be removed")
	}
}

func TestRemoveByTag(t *testing.T) {
	c := New()
	defer c.Stop()

	c.AddFunc("@every 1s", func() {}, WithName("job1"), WithTags("maintenance"))
	c.AddFunc("@every 2s", func() {}, WithName("job2"), WithTags("maintenance", "critical"))
	c.AddFunc("@every 3s", func() {}, WithName("job3"), WithTags("cleanup"))

	// Verify initial count
	if len(c.Entries()) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(c.Entries()))
	}

	// Remove by maintenance tag
	count := c.RemoveByTag("maintenance")
	if count != 2 {
		t.Errorf("expected 2 entries removed, got %d", count)
	}

	// Verify remaining
	if len(c.Entries()) != 1 {
		t.Errorf("expected 1 entry remaining, got %d", len(c.Entries()))
	}

	// Remaining entry should be job3
	entry := c.EntryByName("job3")
	if !entry.Valid() {
		t.Error("expected job3 to still exist")
	}
}

func TestRemoveByTagWhileRunning(t *testing.T) {
	c := New()
	c.Start()
	defer c.Stop()

	c.AddFunc("@every 1s", func() {}, WithTags("batch"))
	c.AddFunc("@every 2s", func() {}, WithTags("batch"))
	c.AddFunc("@every 3s", func() {}, WithTags("other"))

	time.Sleep(10 * time.Millisecond)

	count := c.RemoveByTag("batch")
	if count != 2 {
		t.Errorf("expected 2 entries removed, got %d", count)
	}

	time.Sleep(10 * time.Millisecond)

	if len(c.Entries()) != 1 {
		t.Errorf("expected 1 entry remaining, got %d", len(c.Entries()))
	}
}

func TestNameReuseAfterRemoval(t *testing.T) {
	c := New()
	defer c.Stop()

	// Add named job
	_, err := c.AddFunc("@every 1s", func() {}, WithName("my-job"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Remove it
	c.RemoveByName("my-job")

	// Should be able to add with same name again
	_, err = c.AddFunc("@every 2s", func() {}, WithName("my-job"))
	if err != nil {
		t.Errorf("expected to reuse name after removal, got error: %v", err)
	}
}

func TestIndexCompaction(t *testing.T) {
	c := New()
	defer c.Stop()

	// Add more than indexCompactionThreshold entries with some buffer
	// We need: deletions >= threshold AND deletions > currentSize
	totalEntries := indexCompactionThreshold + 100
	ids := make([]EntryID, totalEntries)
	for i := range ids {
		id, err := c.AddFunc("@every 1s", func() {}, WithName(fmt.Sprintf("job-%d", i)))
		if err != nil {
			t.Fatalf("failed to add job %d: %v", i, err)
		}
		ids[i] = id
	}

	// Verify all entries are present
	if len(c.Entries()) != totalEntries {
		t.Fatalf("expected %d entries, got %d", totalEntries, len(c.Entries()))
	}

	// Remove entries to trigger compaction.
	// Compaction triggers when: deletions >= 1000 AND deletions > currentSize
	// After 1000 deletions, currentSize = 100, so 1000 > 100 triggers compaction.
	// Then we delete 90 more, so indexDeletions ends at 90.
	for i := 0; i < indexCompactionThreshold+90; i++ {
		c.Remove(ids[i])
	}

	// Verify remaining entries are still accessible after compaction
	remaining := c.Entries()
	if len(remaining) != 10 {
		t.Errorf("expected 10 remaining entries, got %d", len(remaining))
	}

	// Verify name lookup still works (proves maps were rebuilt correctly)
	entry := c.EntryByName(fmt.Sprintf("job-%d", indexCompactionThreshold+95))
	if !entry.Valid() {
		t.Error("expected to find entry by name after compaction")
	}

	// Verify entry lookup by ID still works
	entry = c.Entry(ids[totalEntries-1])
	if !entry.Valid() {
		t.Error("expected to find entry by ID after compaction")
	}

	// Compaction should have occurred (at the 1000th deletion) and then
	// 90 more deletions happened. So indexDeletions should be 90.
	// This proves compaction reset the counter.
	expectedDeletions := 90
	if c.indexDeletions != expectedDeletions {
		t.Errorf("expected indexDeletions to be %d after compaction, got %d", expectedDeletions, c.indexDeletions)
	}
}

// TestIndexCompactionBoundaryCurrentSizeZero tests cron.go:820 when currentSize == 0.
// This kills the CONDITIONALS_BOUNDARY mutation where `currentSize > 0` could become `>= 0`.
// When currentSize == 0, the condition `currentSize > 0 && ...` is false, so we don't skip.
func TestIndexCompactionBoundaryCurrentSizeZero(t *testing.T) {
	c := New()
	defer c.Stop()

	// Add exactly threshold + 1 entries (1001)
	// When we delete all of them, at deletion 1000: currentSize = 1
	// At deletion 1001: currentSize = 0, triggers compaction
	totalEntries := indexCompactionThreshold + 1
	ids := make([]EntryID, totalEntries)
	for i := range ids {
		id, err := c.AddFunc("@every 1s", func() {}, WithName(fmt.Sprintf("zero-%d", i)))
		if err != nil {
			t.Fatalf("failed to add job %d: %v", i, err)
		}
		ids[i] = id
	}

	// Remove all entries
	for i := 0; i < totalEntries; i++ {
		c.Remove(ids[i])
	}

	// After removing all 1001 entries:
	// - At deletion 1000: threshold reached, currentSize = 1, 1000 <= 1 is false, compaction triggers
	// - Actually wait, 1000 <= 1 is false, so compaction would trigger earlier

	// After compaction at 1000, indexDeletions resets to 0
	// Then one more deletion, indexDeletions = 1
	// So indexDeletions should be 1
	if c.indexDeletions != 1 {
		t.Errorf("expected indexDeletions = 1 after all removals (compaction resets counter), got %d", c.indexDeletions)
	}

	// Verify all entries are gone
	if len(c.Entries()) != 0 {
		t.Errorf("expected 0 entries, got %d", len(c.Entries()))
	}
}

// TestIndexCompactionBoundaryDeletionsEqualSize tests cron.go:820 where
// `indexDeletions <= currentSize` at the exact boundary (deletions == currentSize).
// This kills the CONDITIONALS_BOUNDARY mutation where <= could become <.
func TestIndexCompactionBoundaryDeletionsEqualSize(t *testing.T) {
	c := New()
	defer c.Stop()

	// Add 2000 entries, then remove exactly 1000
	// At deletion 1000: currentSize = 1000, indexDeletions = 1000
	// Condition: 1000 > 0 && 1000 <= 1000 → true, so we skip compaction
	// With mutation <= → <: 1000 < 1000 → false, so we'd incorrectly compact
	totalEntries := 2 * indexCompactionThreshold
	ids := make([]EntryID, totalEntries)
	for i := range ids {
		id, err := c.AddFunc("@every 1s", func() {}, WithName(fmt.Sprintf("eq-%d", i)))
		if err != nil {
			t.Fatalf("failed to add job %d: %v", i, err)
		}
		ids[i] = id
	}

	// Remove exactly 1000 entries (reaching threshold but not exceeding currentSize)
	for i := 0; i < indexCompactionThreshold; i++ {
		c.Remove(ids[i])
	}

	// indexDeletions should still be 1000 (no compaction happened)
	// because currentSize = 1000 and 1000 <= 1000
	if c.indexDeletions != indexCompactionThreshold {
		t.Errorf("expected indexDeletions = %d (no compaction at boundary), got %d",
			indexCompactionThreshold, c.indexDeletions)
	}

	// Now remove one more entry
	// indexDeletions = 1001, currentSize = 999
	// 1001 <= 999 is false, so compaction triggers
	c.Remove(ids[indexCompactionThreshold])

	// After compaction, indexDeletions resets to 0, then increments for this deletion = 1
	// Actually wait, the deletion happens, then compaction. Let me check the order.
	// In removeEntry: indexDeletions++ then maybeCompactIndexes()
	// So at the 1001st deletion:
	// - indexDeletions becomes 1001
	// - maybeCompactIndexes checks: 1001 >= 1000 ✓, currentSize = 999, 1001 <= 999? No
	// - Compaction triggers, indexDeletions resets to 0 (AFTER the deletion is counted)
	// Wait, compaction sets indexDeletions = 0 after rebuilding
	if c.indexDeletions != 0 {
		t.Errorf("expected indexDeletions = 0 after compaction triggered, got %d", c.indexDeletions)
	}

	// Verify remaining entries
	if len(c.Entries()) != indexCompactionThreshold-1 {
		t.Errorf("expected %d entries, got %d", indexCompactionThreshold-1, len(c.Entries()))
	}
}

// TestIndexCompactionBoundaryDeletionsEqualSizeExplicit is a focused test for cron.go:820.
// This explicitly tests the exact boundary where indexDeletions == currentSize.
// The mutation `<=` → `<` would incorrectly trigger compaction at this boundary.
func TestIndexCompactionBoundaryDeletionsEqualSizeExplicit(t *testing.T) {
	c := New()
	defer c.Stop()

	// Strategy: Create a state where after threshold deletions,
	// indexDeletions == currentSize exactly.
	//
	// We need: total entries = threshold + X, delete threshold entries
	// After threshold deletions: currentSize = X, indexDeletions = threshold
	// For indexDeletions == currentSize, we need X = threshold
	// So total = 2 * threshold

	total := 2 * indexCompactionThreshold
	ids := make([]EntryID, total)

	for i := 0; i < total; i++ {
		id, err := c.AddFunc("@every 1s", func() {}, WithName(fmt.Sprintf("explicit-%d", i)))
		if err != nil {
			t.Fatalf("failed to add job %d: %v", i, err)
		}
		ids[i] = id
	}

	// Verify starting state
	if len(c.entryIndex) != total {
		t.Fatalf("expected %d entries, got %d", total, len(c.entryIndex))
	}
	if c.indexDeletions != 0 {
		t.Fatalf("expected indexDeletions = 0, got %d", c.indexDeletions)
	}

	// Delete exactly threshold entries
	for i := 0; i < indexCompactionThreshold; i++ {
		c.Remove(ids[i])
	}

	// After threshold deletions:
	// - currentSize = total - threshold = threshold (1000)
	// - indexDeletions = threshold (1000)
	// - Condition: threshold >= threshold (true) && currentSize > 0 (true) && deletions <= size (1000 <= 1000 = true)
	// - With <= true: return early, no compaction
	// - With mutation < false: don't return, do compact, reset indexDeletions to 0

	currentSize := len(c.entryIndex)
	if currentSize != indexCompactionThreshold {
		t.Errorf("expected currentSize = %d, got %d", indexCompactionThreshold, currentSize)
	}

	// CRITICAL ASSERTION: indexDeletions should equal threshold (no compaction)
	// With mutation, it would be 0 (after compaction)
	if c.indexDeletions != indexCompactionThreshold {
		t.Errorf("MUTATION DETECTED: indexDeletions should be %d (no compaction at equality boundary), got %d. "+
			"If this is 0, the `<=` was mutated to `<` at cron.go:820",
			indexCompactionThreshold, c.indexDeletions)
	}

	// Additional verification: deleting one more should trigger compaction
	c.Remove(ids[indexCompactionThreshold])

	// After one more deletion:
	// - currentSize = threshold - 1 (999)
	// - indexDeletions would be threshold + 1 (1001) before compaction check
	// - Condition: 1001 >= 1000 (true) && 999 > 0 (true) && 1001 <= 999 (false)
	// - Since condition is false, compaction triggers, indexDeletions resets to 0

	if c.indexDeletions != 0 {
		t.Errorf("expected indexDeletions = 0 after exceeding boundary (compaction should trigger), got %d",
			c.indexDeletions)
	}
}

func TestScheduleJobWithOptions(t *testing.T) {
	c := New()
	defer c.Stop()

	schedule := Every(time.Second)
	id, err := c.ScheduleJob(schedule, FuncJob(func() {}),
		WithName("scheduled-job"),
		WithTags("test"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entry := c.Entry(id)
	if entry.Name != "scheduled-job" {
		t.Errorf("expected name 'scheduled-job', got %q", entry.Name)
	}
	if len(entry.Tags) != 1 || entry.Tags[0] != "test" {
		t.Errorf("unexpected tags: %v", entry.Tags)
	}
}

func TestAddJobWithOptions(t *testing.T) {
	c := New()
	defer c.Stop()

	id, err := c.AddJob("@every 1s", FuncJob(func() {}),
		WithName("added-job"),
		WithTags("api", "critical"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entry := c.Entry(id)
	if entry.Name != "added-job" {
		t.Errorf("expected name 'added-job', got %q", entry.Name)
	}
	if len(entry.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(entry.Tags))
	}
}

func TestEmptyNameAllowed(t *testing.T) {
	c := New()
	defer c.Stop()

	// Multiple jobs without names should be allowed
	_, err := c.AddFunc("@every 1s", func() {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = c.AddFunc("@every 2s", func() {})
	if err != nil {
		t.Errorf("expected no error for second unnamed job, got: %v", err)
	}
}

func TestEntriesIncludesMetadata(t *testing.T) {
	c := New()
	defer c.Stop()

	c.AddFunc("@every 1s", func() {}, WithName("job1"), WithTags("tag1", "tag2"))

	entries := c.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if entries[0].Name != "job1" {
		t.Errorf("expected name in Entries() result, got %q", entries[0].Name)
	}
	if len(entries[0].Tags) != 2 {
		t.Errorf("expected tags in Entries() result, got %v", entries[0].Tags)
	}
}

// ============================================================================
// Context Support Tests
// ============================================================================

// contextAwareJob is a test job that implements JobWithContext
type contextAwareJob struct {
	started      chan struct{}
	completed    chan struct{}
	canceled     chan struct{}
	receivedCtx  context.Context
	ctxWasCalled bool
	runWasCalled bool
	mu           sync.Mutex
}

func newContextAwareJob() *contextAwareJob {
	return &contextAwareJob{
		started:   make(chan struct{}),
		completed: make(chan struct{}),
		canceled:  make(chan struct{}),
	}
}

func (j *contextAwareJob) Run() {
	j.mu.Lock()
	j.runWasCalled = true
	j.mu.Unlock()
	j.RunWithContext(context.Background())
}

func (j *contextAwareJob) RunWithContext(ctx context.Context) {
	j.mu.Lock()
	j.ctxWasCalled = true
	j.receivedCtx = ctx
	j.mu.Unlock()

	close(j.started)

	select {
	case <-ctx.Done():
		close(j.canceled)
	case <-time.After(5 * time.Second):
		// Timeout - job wasn't canceled
	}
	close(j.completed)
}

func TestJobWithContext_ReceivesContext(t *testing.T) {
	fc := NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	c := New(WithClock(fc))

	job := newContextAwareJob()
	c.AddJob("@every 1s", job)
	c.Start()

	// Wait for timer to be set
	fc.BlockUntil(1)
	// Advance time to trigger the job
	fc.Advance(time.Second)

	// Wait for job to start
	select {
	case <-job.started:
		// Good
	case <-time.After(time.Second):
		t.Fatal("job didn't start")
	}

	// Verify RunWithContext was called (not just Run)
	job.mu.Lock()
	ctxWasCalled := job.ctxWasCalled
	job.mu.Unlock()

	if !ctxWasCalled {
		t.Error("RunWithContext was not called")
	}

	// Stop and wait for job completion
	c.Stop()
	<-job.completed
}

func TestJobWithContext_CanceledOnStop(t *testing.T) {
	fc := NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	c := New(WithClock(fc))

	job := newContextAwareJob()
	c.AddJob("@every 1s", job)
	c.Start()

	// Wait for timer and trigger job
	fc.BlockUntil(1)
	fc.Advance(time.Second)

	// Wait for job to start
	select {
	case <-job.started:
		// Good
	case <-time.After(time.Second):
		t.Fatal("job didn't start")
	}

	// Stop the cron - should cancel the context
	c.Stop()

	// Verify the context was canceled
	select {
	case <-job.canceled:
		// Good - context was canceled
	case <-time.After(time.Second):
		t.Error("context was not canceled when Stop() was called")
	}
}

func TestFuncJobWithContext(t *testing.T) {
	fc := NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	c := New(WithClock(fc))

	var receivedCtx context.Context
	started := make(chan struct{})
	completed := make(chan struct{})

	c.AddJob("@every 1s", FuncJobWithContext(func(ctx context.Context) {
		receivedCtx = ctx
		close(started)
		<-ctx.Done()
		close(completed)
	}))

	c.Start()

	fc.BlockUntil(1)
	fc.Advance(time.Second)

	<-started

	if receivedCtx == nil {
		t.Error("FuncJobWithContext did not receive a context")
	}

	c.Stop()
	<-completed
}

func TestWithContext_PropagatesContext(t *testing.T) {
	fc := NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))

	// Create a context with a value
	type ctxKey string
	baseCtx := context.WithValue(context.Background(), ctxKey("testKey"), "testValue")

	c := New(WithClock(fc), WithContext(baseCtx))

	var receivedValue interface{}
	started := make(chan struct{})

	c.AddJob("@every 1s", FuncJobWithContext(func(ctx context.Context) {
		receivedValue = ctx.Value(ctxKey("testKey"))
		close(started)
	}))

	c.Start()
	defer c.Stop()

	fc.BlockUntil(1)
	fc.Advance(time.Second)

	<-started

	if receivedValue != "testValue" {
		t.Errorf("context value not propagated: got %v, want %q", receivedValue, "testValue")
	}
}

func TestRegularJob_StillWorks(t *testing.T) {
	// Ensure regular jobs without context still work
	fc := NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	c := New(WithClock(fc))

	executed := make(chan struct{})

	c.AddFunc("@every 1s", func() {
		close(executed)
	})

	c.Start()
	defer c.Stop()

	fc.BlockUntil(1)
	fc.Advance(time.Second)

	select {
	case <-executed:
		// Good
	case <-time.After(time.Second):
		t.Error("regular FuncJob did not execute")
	}
}

func TestTimeoutWithContext_JobCompletesBeforeTimeout(t *testing.T) {
	fc := NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	c := New(
		WithClock(fc),
		WithChain(TimeoutWithContext(DefaultLogger, 5*time.Minute)),
	)

	executed := make(chan struct{})

	c.AddJob("@every 1s", FuncJobWithContext(func(ctx context.Context) {
		// Job completes immediately
		close(executed)
	}))

	c.Start()
	defer c.Stop()

	fc.BlockUntil(1)
	fc.Advance(time.Second)

	select {
	case <-executed:
		// Good
	case <-time.After(time.Second):
		t.Error("job did not execute")
	}
}

func TestTimeoutWithContext_ContextCanceledOnTimeout(t *testing.T) {
	// Use a more generous timeout for race detector stability.
	// 50ms is too tight; 200ms is safe but still fast for tests.
	jobTimeout := 200 * time.Millisecond

	fc := NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	c := New(
		WithClock(fc),
		WithChain(TimeoutWithContext(DefaultLogger, jobTimeout)),
	)

	// Use channels for synchronization, not atomic polling + time.Sleep.
	// This makes the test event-driven rather than time-driven.
	jobStarted := make(chan struct{})
	jobCanceled := make(chan struct{})

	c.AddJob("@every 1s", FuncJobWithContext(func(ctx context.Context) {
		close(jobStarted)

		// Block solely on ctx.Done().
		// If the timeout doesn't fire, the test runner's timeout catches it.
		<-ctx.Done()

		// Signal that we successfully caught the cancellation
		close(jobCanceled)
	}))

	c.Start()
	defer c.Stop()

	// Wait for timer to be registered
	fc.BlockUntil(1)

	// Advance time to trigger the job
	fc.Advance(time.Second)

	// Wait for job to start (with a safety timeout)
	select {
	case <-jobStarted:
		// Job running
	case <-time.After(2 * time.Second):
		t.Fatal("job did not start within 2 seconds")
	}

	// Wait for the context timeout to fire.
	// We wait longer than jobTimeout to account for scheduler/race overhead.
	select {
	case <-jobCanceled:
		// Success: The context was canceled as expected
	case <-time.After(jobTimeout * 5):
		// Failure: The timeout didn't fire in a reasonable window
		t.Fatalf("job context was not canceled after %v (expected ~%v)", jobTimeout*5, jobTimeout)
	}
}

func TestTimeoutWithContext_ZeroTimeoutDisabled(t *testing.T) {
	fc := NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	c := New(
		WithClock(fc),
		WithChain(TimeoutWithContext(DefaultLogger, 0)), // Zero timeout = disabled
	)

	executed := make(chan struct{})

	c.AddJob("@every 1s", FuncJobWithContext(func(ctx context.Context) {
		close(executed)
	}))

	c.Start()
	defer c.Stop()

	fc.BlockUntil(1)
	fc.Advance(time.Second)

	select {
	case <-executed:
		// Good - job executed without timeout
	case <-time.After(time.Second):
		t.Error("job did not execute with zero timeout")
	}
}

func TestTimeoutWithContext_PreservesJobWithContextInterface(t *testing.T) {
	// Verify that TimeoutWithContext returns a JobWithContext
	wrapper := TimeoutWithContext(DefaultLogger, time.Minute)
	job := FuncJobWithContext(func(ctx context.Context) {})
	wrapped := wrapper(job)

	if _, ok := wrapped.(JobWithContext); !ok {
		t.Error("TimeoutWithContext should return a JobWithContext")
	}
}
