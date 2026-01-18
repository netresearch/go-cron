# Troubleshooting Guide

Common issues and solutions when using go-cron.

## Table of Contents

- [Job Not Running](#job-not-running)
- [Wrong Execution Time](#wrong-execution-time)
- [Timezone Issues](#timezone-issues)
- [DST Problems](#dst-problems)
- [Panics and Crashes](#panics-and-crashes)
- [Performance Issues](#performance-issues)
- [Debugging Techniques](#debugging-techniques)

---

## Job Not Running

### Symptom: Job never executes

**Check 1: Did you call `Start()`?**

```go
c := cron.New()
c.AddFunc("@every 1s", myJob)
// MISSING: c.Start()
select {} // Hangs forever, job never runs
```

**Fix:** Always call `c.Start()` after adding jobs.

---

**Check 2: Is the schedule valid?**

```go
id, err := c.AddFunc("invalid spec", myJob)
if err != nil {
    log.Printf("Invalid schedule: %v", err)
}
```

Use `ValidateSpec()` to check expressions:

```go
if err := cron.ValidateSpec("0 0 31 2 *"); err != nil {
    // "day 31 in February never matches"
}
```

---

**Check 3: Does the schedule ever match?**

```go
// This only runs on Feb 31st (never)
c.AddFunc("0 0 31 2 *", myJob)
```

Use `AnalyzeSpec()` to check for issues:

```go
result, _ := cron.AnalyzeSpec("0 0 31 2 *")
if result.NeverMatches {
    log.Println("Schedule will never match!")
}
```

---

**Check 4: Is DOM/DOW blocking it?**

With AND logic (default), both day-of-month AND day-of-week must match:

```go
// Only runs on Friday the 13th
c.AddFunc("0 0 13 * 5", myJob)
```

If you want OR logic (any Friday OR the 13th):

```go
c := cron.New(cron.WithParser(cron.NewParser(
    cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.DowOrDom,
)))
```

---

### Symptom: Job runs but does nothing

**Check: Is your job panicking silently?**

Without `Recover()` wrapper, panics are logged but may be missed:

```go
c := cron.New(cron.WithChain(
    cron.Recover(cron.DefaultLogger),
))
```

Check logs for panic messages.

---

### Symptom: Job skipped

**Check: Is `SkipIfStillRunning` active?**

If a job takes longer than its interval, subsequent runs are skipped:

```go
c := cron.New(cron.WithChain(
    cron.SkipIfStillRunning(logger), // Skips if previous still running
))
c.AddFunc("@every 1s", func() {
    time.Sleep(5 * time.Second) // Takes 5s, skips next 4 runs
})
```

Check logs for "skip" messages.

---

## Wrong Execution Time

### Symptom: Job runs at unexpected hour

**Cause: Timezone mismatch**

The scheduler uses `time.Local` by default. If your server is in UTC but you expect local time:

```go
// Runs at 9 AM in server's timezone (probably UTC)
c.AddFunc("0 9 * * *", myJob)

// Fix: Specify timezone explicitly
loc, _ := time.LoadLocation("America/New_York")
c := cron.New(cron.WithLocation(loc))
c.AddFunc("0 9 * * *", myJob) // Now 9 AM Eastern
```

Or use `TZ=` prefix:

```go
c.AddFunc("TZ=America/New_York 0 9 * * *", myJob)
```

---

### Symptom: Job runs multiple times per trigger

**Cause: Multiple scheduler instances**

If you have multiple pods/processes running the same code, each will trigger the job independently.

**Fix:** Use distributed locking or run scheduler on single instance.

---

### Symptom: Job runs late

**Cause: System clock drift or load**

```go
hooks := cron.ObservabilityHooks{
    OnJobStart: func(id cron.EntryID, name string, scheduled time.Time) {
        lag := time.Since(scheduled)
        if lag > time.Second {
            log.Printf("Job %s started %v late", name, lag)
        }
    },
}
c := cron.New(cron.WithObservability(hooks))
```

**Causes:**
- High CPU load delays scheduler tick
- Long-running jobs block new ones (with `DelayIfStillRunning`)
- System clock adjusted

---

## Timezone Issues

### Symptom: TZ= prefix causes panic

**Cause: Invalid timezone name**

```go
// PANIC: unknown time zone "Invalid/Zone"
c.AddFunc("TZ=Invalid/Zone 0 0 * * *", myJob)
```

**Fix:** Use valid IANA timezone names:

```go
// Validate timezone exists
if _, err := time.LoadLocation("America/New_York"); err != nil {
    log.Fatal("Invalid timezone")
}
```

---

### Symptom: Timezone not recognized in container

**Cause: Missing timezone database**

Alpine and minimal containers may lack timezone data.

**Fix for Alpine:**

```dockerfile
RUN apk add --no-cache tzdata
```

**Fix for scratch containers:**

```dockerfile
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
```

---

### Symptom: Time appears shifted by hours

**Cause: Mixing UTC and local time**

```go
// If your app logs in UTC but schedule is in local time
log.Printf("Next run: %v", entry.Next) // Shows UTC
log.Printf("Next run: %v", entry.Next.In(loc)) // Shows local
```

**Fix:** Be consistent. Use `WithLocation` and convert times for display.

---

## DST Problems

### Symptom: Job skipped during spring forward

During "spring forward" (e.g., 2 AM → 3 AM), the 2:xx hour doesn't exist.

**go-cron behavior:** Jobs scheduled during skipped hour run immediately at the new time (ISC cron compatible).

```go
// Scheduled for 2:30 AM on DST transition day
// Actually runs at ~3:00 AM when clock jumps
c.AddFunc("30 2 * * *", myJob)
```

See [DST_HANDLING.md](DST_HANDLING.md) for details.

---

### Symptom: Job runs twice during fall back

During "fall back" (e.g., 2 AM → 1 AM), the 1:xx hour occurs twice.

**go-cron behavior:** Job runs once, during the first occurrence.

---

### Symptom: Daily job drifts by an hour after DST

**Cause: Using fixed offset instead of timezone**

```go
// BAD: Fixed offset doesn't handle DST
loc := time.FixedZone("EST", -5*3600)

// GOOD: Named timezone handles DST automatically
loc, _ := time.LoadLocation("America/New_York")
```

---

## Panics and Crashes

### Symptom: Scheduler crashes on job panic

**Cause: Missing `Recover()` wrapper**

```go
c := cron.New(cron.WithChain(
    cron.Recover(cron.DefaultLogger),
))
```

Without `Recover()`, job panics terminate the goroutine but are logged.

---

### Symptom: "panic: interface conversion" on Entry

**Cause: Accessing invalid entry**

```go
entry := c.Entry(nonExistentID)
// entry is zero value, entry.Job is nil

entry.Job.Run() // PANIC: nil pointer dereference
```

**Fix:** Check `entry.Valid()` first:

```go
if entry := c.Entry(id); entry.Valid() {
    entry.Run()
}
```

---

### Symptom: Panic in observability hook

Hooks run in goroutines; panics are isolated but should be handled:

```go
hooks := cron.ObservabilityHooks{
    OnJobComplete: func(id cron.EntryID, name string, dur time.Duration, rec any) {
        defer func() {
            if r := recover(); r != nil {
                log.Printf("Hook panic: %v", r)
            }
        }()
        // Your hook code
    },
}
```

---

## Performance Issues

### Symptom: High memory usage with many add/remove cycles

**Cause: Go maps don't shrink**

The scheduler automatically compacts indexes after 1000 deletions. For extreme cases:

```go
// Check current entry count
entries := c.Entries()
log.Printf("Active entries: %d", len(entries))
```

---

### Symptom: Slow Add() with thousands of entries

**Cause: Map resizing**

Pre-allocate for bulk operations:

```go
c := cron.New(cron.WithCapacity(10000))
```

---

### Symptom: Entries() is slow

`Entries()` copies all entries and sorts them. For large entry counts, use `Entry(id)` for single lookups:

```go
// Slow for 10k entries
entries := c.Entries()

// Fast: O(1) lookup
entry := c.Entry(knownID)
```

---

## Debugging Techniques

### Enable verbose logging

```go
type verboseLogger struct{}

func (l verboseLogger) Info(msg string, keysAndValues ...any) {
    log.Printf("INFO: %s %v", msg, keysAndValues)
}

func (l verboseLogger) Error(err error, msg string, keysAndValues ...any) {
    log.Printf("ERROR: %s %v %v", msg, err, keysAndValues)
}

c := cron.New(cron.WithLogger(verboseLogger{}))
```

---

### Inspect schedule behavior

```go
spec := "0 9 * * 1-5"
schedule, _ := cron.ParseStandard(spec)

// Check next 5 occurrences
now := time.Now()
for i := 0; i < 5; i++ {
    next := schedule.Next(now)
    log.Printf("Run %d: %v", i+1, next)
    now = next
}
```

---

### Use FakeClock for deterministic testing

```go
clock := cron.NewFakeClock(time.Date(2024, 3, 10, 1, 59, 0, 0, loc))
c := cron.New(cron.WithClock(clock))
c.AddFunc("0 2 * * *", myJob)
c.Start()

// Advance to trigger job
clock.Advance(2 * time.Minute)
time.Sleep(10 * time.Millisecond) // Let scheduler process
```

---

### Check schedule analysis

```go
result, err := cron.AnalyzeSpec("0 0 31 2 *")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Never matches: %v\n", result.NeverMatches)
fmt.Printf("Fields: %+v\n", result.Fields)
```

---

### Monitor with hooks

```go
hooks := cron.ObservabilityHooks{
    OnJobStart: func(id cron.EntryID, name string, scheduled time.Time) {
        log.Printf("[START] Job %s (ID=%d) scheduled=%v", name, id, scheduled)
    },
    OnJobComplete: func(id cron.EntryID, name string, dur time.Duration, rec any) {
        if rec != nil {
            log.Printf("[PANIC] Job %s: %v", name, rec)
        } else {
            log.Printf("[DONE] Job %s took %v", name, dur)
        }
    },
}
c := cron.New(cron.WithObservability(hooks))
```

---

## Still Stuck?

1. Check [FAQ.md](FAQ.md) for common questions
2. Review [ARCHITECTURE.md](ARCHITECTURE.md) for internal behavior
3. See [DST_HANDLING.md](DST_HANDLING.md) for timezone edge cases
4. Open an issue: https://github.com/netresearch/go-cron/issues
