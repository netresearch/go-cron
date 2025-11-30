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
	c := New(WithClock(ClockFunc(func() time.Time { return fixedTime })))
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
		WithClock(ClockFunc(func() time.Time { return fixedTime })),
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
