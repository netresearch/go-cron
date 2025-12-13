package cron

import (
	"log"
	"strings"
	"testing"
	"time"
)

func TestWithLocation(t *testing.T) {
	c := New(WithLocation(time.UTC))
	if c.location != time.UTC {
		t.Errorf("expected UTC, got %v", c.location)
	}
}

func TestWithParser(t *testing.T) {
	parser := NewParser(Dow)
	c := New(WithParser(parser))
	if c.parser != parser {
		t.Error("expected provided parser")
	}
}

func TestWithVerboseLogger(t *testing.T) {
	var buf syncWriter
	logger := log.New(&buf, "", log.LstdFlags)
	c := New(WithLogger(VerbosePrintfLogger(logger)))
	if c.logger.(printfLogger).logger != logger {
		t.Error("expected provided logger")
	}

	c.AddFunc("@every 1s", func() {})
	c.Start()
	time.Sleep(OneSecond)
	c.Stop()
	out := buf.String()
	if !strings.Contains(out, "schedule,") ||
		!strings.Contains(out, "run,") {
		t.Error("expected to see some actions, got:", out)
	}
}

func TestWithClock(t *testing.T) {
	fixedTime := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	c := New(WithClock(NewFakeClock(fixedTime)))
	if c.clock == nil {
		t.Error("expected clock to be set")
	}

	// Verify now() uses the custom clock
	now := c.now()
	if !now.Equal(fixedTime) {
		t.Errorf("expected %v, got %v", fixedTime, now)
	}
}

func TestWithClockAndLocation(t *testing.T) {
	fixedTime := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	loc, _ := time.LoadLocation("America/New_York")

	c := New(
		WithClock(NewFakeClock(fixedTime)),
		WithLocation(loc),
	)

	// Verify now() converts to the specified location
	now := c.now()
	if now.Location() != loc {
		t.Errorf("expected location %v, got %v", loc, now.Location())
	}

	// The time should be the same instant, just in a different location
	if !now.Equal(fixedTime) {
		t.Errorf("times should represent the same instant: %v vs %v", now, fixedTime)
	}
}

func TestWithClockDefaultRealClock(t *testing.T) {
	// Default clock should be RealClock
	c := New()
	if c.clock == nil {
		t.Error("expected clock to be set by default")
	}
	// Verify it's a RealClock
	if _, ok := c.clock.(RealClock); !ok {
		t.Errorf("expected RealClock, got %T", c.clock)
	}

	before := time.Now()
	now := c.now()
	after := time.Now()

	if now.Before(before) || now.After(after) {
		t.Errorf("now() should return current time, got %v (expected between %v and %v)", now, before, after)
	}
}

func TestWithMinEveryInterval(t *testing.T) {
	tests := []struct {
		name        string
		minInterval time.Duration
		spec        string
		wantErr     bool
		errContains string
	}{
		{
			name:        "default 1s minimum - valid",
			minInterval: time.Second,
			spec:        "@every 1s",
			wantErr:     false,
		},
		{
			name:        "default 1s minimum - invalid",
			minInterval: time.Second,
			spec:        "@every 500ms",
			wantErr:     true,
			errContains: "at least 1s",
		},
		{
			name:        "sub-second allowed with 0 minimum",
			minInterval: 0,
			spec:        "@every 100ms",
			wantErr:     false,
		},
		{
			name:        "sub-second allowed with explicit minimum",
			minInterval: 100 * time.Millisecond,
			spec:        "@every 100ms",
			wantErr:     false,
		},
		{
			name:        "sub-second below explicit minimum",
			minInterval: 100 * time.Millisecond,
			spec:        "@every 50ms",
			wantErr:     true,
			errContains: "at least 100ms",
		},
		{
			name:        "larger minimum enforced",
			minInterval: time.Minute,
			spec:        "@every 30s",
			wantErr:     true,
			errContains: "at least 1m",
		},
		{
			name:        "larger minimum - valid",
			minInterval: time.Minute,
			spec:        "@every 2m",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := New(WithMinEveryInterval(tt.minInterval))
			_, err := c.AddFunc(tt.spec, func() {})
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errContains)
				} else if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestWithMinEveryInterval_SubSecondExecution(t *testing.T) {
	// Test that sub-second intervals actually work when allowed
	clock := NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	c := New(
		WithMinEveryInterval(0),
		WithClock(clock),
	)

	count := 0
	_, err := c.AddFunc("@every 100ms", func() {
		count++
	})
	if err != nil {
		t.Fatalf("failed to add sub-second job: %v", err)
	}

	c.Start()
	defer c.Stop()

	// Advance time by 350ms - should trigger ~3 executions
	for i := 0; i < 35; i++ {
		clock.Advance(10 * time.Millisecond)
		time.Sleep(1 * time.Millisecond) // Let scheduler process
	}

	if count < 3 {
		t.Errorf("expected at least 3 executions, got %d", count)
	}
}

func TestParserWithMinEveryInterval(t *testing.T) {
	// Test Parser.WithMinEveryInterval directly
	p := NewParser(Minute | Hour | Dom | Month | Dow | Descriptor).
		WithMinEveryInterval(0)

	// Should allow sub-second
	sched, err := p.Parse("@every 100ms")
	if err != nil {
		t.Errorf("expected sub-second to be allowed, got error: %v", err)
	}
	if sched == nil {
		t.Error("expected non-nil schedule")
	}

	// Test with larger minimum
	p = NewParser(Minute | Hour | Dom | Month | Dow | Descriptor).
		WithMinEveryInterval(time.Minute)

	_, err = p.Parse("@every 30s")
	if err == nil {
		t.Error("expected error for duration below minimum")
	}
}

func TestStandardParser(t *testing.T) {
	// Test that StandardParser returns a usable copy
	p := StandardParser()

	// Should work with default 1s minimum
	_, err := p.Parse("@every 1s")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Should fail for sub-second
	_, err = p.Parse("@every 500ms")
	if err == nil {
		t.Error("expected error for sub-second interval")
	}

	// Should be modifiable without affecting global standardParser
	p2 := StandardParser().WithMinEveryInterval(0)
	_, err = p2.Parse("@every 100ms")
	if err != nil {
		t.Errorf("modified parser should allow sub-second: %v", err)
	}

	// Original standardParser should still enforce 1s
	_, err = ParseStandard("@every 500ms")
	if err == nil {
		t.Error("standardParser should still enforce 1s minimum")
	}
}

func TestWithSecondOptional(t *testing.T) {
	c := New(WithSecondOptional())

	// Test 5-field expression (standard) - seconds defaults to 0
	id1, err := c.AddFunc("* * * * *", func() {})
	if err != nil {
		t.Fatalf("AddFunc() with 5 fields failed: %v", err)
	}
	entry1 := c.Entry(id1)
	if entry1.Schedule == nil {
		t.Fatal("Expected schedule for 5-field expression")
	}

	// Verify seconds defaults to 0 for 5-field expression
	now := time.Date(2024, 1, 1, 12, 30, 15, 0, time.UTC)
	next := entry1.Schedule.Next(now)
	if next.Second() != 0 {
		t.Errorf("Expected 5-field expression to have seconds=0, got %d", next.Second())
	}

	// Test 6-field expression (with seconds)
	id2, err := c.AddFunc("30 * * * * *", func() {})
	if err != nil {
		t.Fatalf("AddFunc() with 6 fields failed: %v", err)
	}
	entry2 := c.Entry(id2)
	if entry2.Schedule == nil {
		t.Fatal("Expected schedule for 6-field expression")
	}

	// Verify seconds is 30 for 6-field expression
	next2 := entry2.Schedule.Next(now)
	if next2.Second() != 30 {
		t.Errorf("Expected 6-field expression to have seconds=30, got %d", next2.Second())
	}

	// Test 6-field expression with seconds range
	id3, err := c.AddFunc("*/10 * * * * *", func() {})
	if err != nil {
		t.Fatalf("AddFunc() with 6 fields (range) failed: %v", err)
	}
	entry3 := c.Entry(id3)
	if entry3.Schedule == nil {
		t.Fatal("Expected schedule for 6-field expression with range")
	}
}

func TestWithSecondOptionalDescriptors(t *testing.T) {
	c := New(WithSecondOptional())

	// Descriptors should still work
	_, err := c.AddFunc("@every 1s", func() {})
	if err != nil {
		t.Errorf("@every descriptor should work: %v", err)
	}

	_, err = c.AddFunc("@hourly", func() {})
	if err != nil {
		t.Errorf("@hourly descriptor should work: %v", err)
	}
}

func TestWithSecondOptionalWithTimezone(t *testing.T) {
	c := New(WithSecondOptional())

	// 5-field with timezone
	_, err := c.AddFunc("TZ=UTC * * * * *", func() {})
	if err != nil {
		t.Errorf("5-field with TZ should work: %v", err)
	}

	// 6-field with timezone
	_, err = c.AddFunc("TZ=UTC 30 * * * * *", func() {})
	if err != nil {
		t.Errorf("6-field with TZ should work: %v", err)
	}

	// CRON_TZ prefix variant
	_, err = c.AddFunc("CRON_TZ=America/New_York * * * * *", func() {})
	if err != nil {
		t.Errorf("5-field with CRON_TZ should work: %v", err)
	}
}

func TestParserWithSecondOptional(t *testing.T) {
	// Test Parser.WithSecondOptional() builder method
	p := NewParser(Minute | Hour | Dom | Month | Dow | Descriptor).
		WithSecondOptional()

	// Should accept 5 fields
	sched1, err := p.Parse("* * * * *")
	if err != nil {
		t.Errorf("5-field expression should be accepted: %v", err)
	}
	if sched1 == nil {
		t.Error("Expected non-nil schedule for 5-field expression")
	}

	// Should accept 6 fields
	sched2, err := p.Parse("30 * * * * *")
	if err != nil {
		t.Errorf("6-field expression should be accepted: %v", err)
	}
	if sched2 == nil {
		t.Error("Expected non-nil schedule for 6-field expression")
	}

	// Verify the schedules are different
	now := time.Date(2024, 1, 1, 12, 30, 15, 0, time.UTC)
	next1 := sched1.Next(now)
	next2 := sched2.Next(now)

	if next1.Second() != 0 {
		t.Errorf("5-field schedule should have seconds=0, got %d", next1.Second())
	}
	if next2.Second() != 30 {
		t.Errorf("6-field schedule should have seconds=30, got %d", next2.Second())
	}
}

func TestParserWithSecondOptionalChained(t *testing.T) {
	// Test chaining WithSecondOptional with other builder methods
	p := NewParser(Minute | Hour | Dom | Month | Dow | Descriptor).
		WithSecondOptional().
		WithMinEveryInterval(0).
		WithMaxSearchYears(10)

	// Should accept optional seconds
	_, err := p.Parse("* * * * *")
	if err != nil {
		t.Errorf("5-field expression should be accepted: %v", err)
	}

	// Should allow sub-second @every
	_, err = p.Parse("@every 100ms")
	if err != nil {
		t.Errorf("sub-second @every should be allowed: %v", err)
	}
}

func TestWithSecondOptionalInvalidFieldCount(t *testing.T) {
	c := New(WithSecondOptional())

	// Too few fields (4)
	_, err := c.AddFunc("* * * *", func() {})
	if err == nil {
		t.Error("4-field expression should be rejected")
	}

	// Too many fields (7)
	_, err = c.AddFunc("* * * * * * *", func() {})
	if err == nil {
		t.Error("7-field expression should be rejected")
	}
}

func TestWithMaxSearchYears(t *testing.T) {
	// Test that WithMaxSearchYears creates a cron with the configured parser
	c := New(WithMaxSearchYears(10))

	// Parse an impossible schedule and verify it uses the configured search years
	// We can't directly test the internal parser, but we can test the behavior
	id, err := c.AddFunc("0 0 30 2 *", func() {}) // Feb 30 - impossible
	if err != nil {
		t.Fatalf("AddFunc() unexpected error: %v", err)
	}

	entry := c.Entry(id)
	// The schedule should exist but Next() should return zero time
	if entry.Schedule == nil {
		t.Fatal("Expected entry to have a schedule")
	}

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	next := entry.Schedule.Next(now)
	if !next.IsZero() {
		t.Errorf("Next() for impossible schedule should return zero time, got %v", next)
	}
}
