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
