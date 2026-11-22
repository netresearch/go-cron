// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cron

import (
	"container/heap"
	"fmt"
	"testing"
	"time"
)

// BenchmarkParseStandard benchmarks parsing standard cron expressions.
func BenchmarkParseStandard(b *testing.B) {
	specs := []string{
		"* * * * *",
		"0 0 * * *",
		"*/5 * * * *",
		"0 9-17 * * 1-5",
		"30 4 1,15 * *",
	}

	i := 0
	for b.Loop() {
		spec := specs[i%len(specs)]
		_, err := ParseStandard(spec)
		if err != nil {
			b.Fatal(err)
		}
		i++
	}
}

// BenchmarkParseWithTimezone benchmarks parsing cron expressions with timezones.
func BenchmarkParseWithTimezone(b *testing.B) {
	specs := []string{
		"CRON_TZ=America/New_York 0 9 * * *",
		"CRON_TZ=Europe/London 30 8 * * 1-5",
		"TZ=Asia/Tokyo 0 0 * * *",
	}

	i := 0
	for b.Loop() {
		spec := specs[i%len(specs)]
		_, err := ParseStandard(spec)
		if err != nil {
			b.Fatal(err)
		}
		i++
	}
}

// BenchmarkParseDescriptor benchmarks parsing descriptor expressions.
func BenchmarkParseDescriptor(b *testing.B) {
	specs := []string{
		"@hourly",
		"@daily",
		"@weekly",
		"@monthly",
		"@yearly",
		"@every 5m",
		"@every 1h30m",
	}

	i := 0
	for b.Loop() {
		spec := specs[i%len(specs)]
		_, err := ParseStandard(spec)
		if err != nil {
			b.Fatal(err)
		}
		i++
	}
}

// BenchmarkNext benchmarks calculating the next execution time.
func BenchmarkNext(b *testing.B) {
	schedule, _ := ParseStandard("*/5 * * * *")
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	for b.Loop() {
		schedule.Next(now)
	}
}

// BenchmarkNextComplex benchmarks Next() with complex schedule.
func BenchmarkNextComplex(b *testing.B) {
	schedule, _ := ParseStandard("15,45 9-17 1,15 1-6 1-5")
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	for b.Loop() {
		schedule.Next(now)
	}
}

// BenchmarkNextWithTimezone benchmarks Next() with timezone conversion.
func BenchmarkNextWithTimezone(b *testing.B) {
	schedule, _ := ParseStandard("CRON_TZ=America/New_York 0 9 * * *")
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	for b.Loop() {
		schedule.Next(now)
	}
}

// BenchmarkAddJob benchmarks adding jobs to a cron instance.
func BenchmarkAddJob(b *testing.B) {
	c := New()
	job := FuncJob(func() {})

	for b.Loop() {
		c.AddJob("* * * * *", job)
	}
}

// BenchmarkCronWithManyJobs benchmarks cron operations with many jobs.
func BenchmarkCronWithManyJobs(b *testing.B) {
	for _, numJobs := range []int{10, 100, 1000} {
		b.Run(formatJobCount(numJobs), func(b *testing.B) {
			benchmarkCronWithJobs(b, numJobs)
		})
	}
}

func formatJobCount(n int) string {
	switch n {
	case 10:
		return "10_jobs"
	case 100:
		return "100_jobs"
	case 1000:
		return "1000_jobs"
	default:
		return "jobs"
	}
}

func benchmarkCronWithJobs(b *testing.B, numJobs int) {
	c := New()
	job := FuncJob(func() {})

	for range numJobs {
		c.AddJob("* * * * *", job)
	}

	for b.Loop() {
		c.Entries()
	}
}

// BenchmarkChainWrappers benchmarks job wrapper chain execution.
func BenchmarkChainWrappers(b *testing.B) {
	job := FuncJob(func() {})
	chain := NewChain(Recover(DiscardLogger))
	wrapped := chain.Then(job)

	for b.Loop() {
		wrapped.Run()
	}
}

// BenchmarkJobTrackerStartFinish measures the per-run bookkeeping of an
// entry's job tracker when nobody waits for the entry.
func BenchmarkJobTrackerStartFinish(b *testing.B) {
	tr := &jobTracker{}
	b.ReportAllocs()
	for b.Loop() {
		tr.start()
		tr.finish()
	}
}

// BenchmarkHandleTimeBackwards measures the check for a backwards clock that
// runs on every wake of the scheduler. With moved=none no entry ran after now,
// which is the case on nearly every wake; with moved=all every entry ran after
// now and is rescheduled.
func BenchmarkHandleTimeBackwards(b *testing.B) {
	for _, n := range []int{100, 10000} {
		b.Run(fmt.Sprintf("entries=%d/moved=none", n), func(b *testing.B) {
			benchmarkHandleTimeBackwards(b, n, 12) // ran before now: nothing to reschedule
		})
		b.Run(fmt.Sprintf("entries=%d/moved=all", n), func(b *testing.B) {
			benchmarkHandleTimeBackwards(b, n, 13) // ran after now: every entry is rescheduled
		})
	}
}

// BenchmarkProcessDueEntries measures one scheduler tick that dispatches n due
// entries with the default logger, which drops Info.
func BenchmarkProcessDueEntries(b *testing.B) {
	for _, n := range []int{1, 100} {
		b.Run(fmt.Sprintf("due=%d", n), func(b *testing.B) {
			now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			c := New(WithSeconds(), WithLogger(DiscardLogger), WithLocation(time.UTC), WithClock(NewFakeClock(now)))
			for range n {
				if _, err := c.AddFunc("* * * * * *", func() {}); err != nil {
					b.Fatal(err)
				}
			}
			for _, e := range c.entries {
				c.scheduleEntryNext(e, now)
			}
			heap.Init(&c.entries)
			b.ReportAllocs()
			i := 0
			for b.Loop() {
				now = now.Add(time.Second)
				c.processDueEntries(now)
				if i++; i%64 == 0 {
					c.jobWaiter.Wait()
				}
			}
			c.jobWaiter.Wait()
		})
	}
}

// benchmarkHandleTimeBackwards runs handleTimeBackwards at 12:30 over n hourly
// entries that last ran at prevHour:00.
func benchmarkHandleTimeBackwards(b *testing.B, n, prevHour int) {
	b.Helper()
	c := New(WithLogger(DiscardLogger))
	schedule, err := ParseStandard("0 * * * *")
	if err != nil {
		b.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 12, 30, 0, 0, time.UTC)
	for i := range n {
		heap.Push(&c.entries, &Entry{
			ID:        EntryID(i + 1),
			Schedule:  schedule,
			Prev:      time.Date(2026, 1, 1, prevHour, 0, 0, 0, time.UTC),
			Next:      time.Date(2026, 1, 1, 14, 0, 0, 0, time.UTC),
			heapIndex: -1,
		})
	}
	b.ReportAllocs()
	for b.Loop() {
		c.handleTimeBackwards(now)
	}
}

// BenchmarkRunLoopTick drives the run loop through one wake per iteration with
// a FakeClock and waits until the single due job has run.
func BenchmarkRunLoopTick(b *testing.B) {
	fc := NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	c := New(WithSeconds(), WithLogger(DiscardLogger), WithLocation(time.UTC), WithClock(fc))
	ran := make(chan struct{}, 1)
	if _, err := c.AddFunc("* * * * * *", func() { ran <- struct{}{} }); err != nil {
		b.Fatal(err)
	}
	c.Start()
	defer c.Stop()
	fc.BlockUntil(1)
	b.ReportAllocs()
	for b.Loop() {
		fc.Advance(time.Second)
		<-ran
		fc.BlockUntil(1)
	}
}
