package cron

import (
	"errors"
	"testing"
	"time"
)

func TestWithMaxEntries_AddFuncReturnsError(t *testing.T) {
	c := New(WithMaxEntries(2))

	// Add first entry - should succeed
	id1, err := c.AddFunc("* * * * *", func() {})
	if err != nil {
		t.Fatalf("first AddFunc failed: %v", err)
	}
	if id1 == 0 {
		t.Error("first entry got invalid ID")
	}

	// Add second entry - should succeed
	id2, err := c.AddFunc("* * * * *", func() {})
	if err != nil {
		t.Fatalf("second AddFunc failed: %v", err)
	}
	if id2 == 0 {
		t.Error("second entry got invalid ID")
	}

	// Add third entry - should fail
	id3, err := c.AddFunc("* * * * *", func() {})
	if !errors.Is(err, ErrMaxEntriesReached) {
		t.Errorf("expected ErrMaxEntriesReached, got: %v", err)
	}
	if id3 != 0 {
		t.Error("third entry should have invalid ID")
	}
}

func TestWithMaxEntries_AddJobReturnsError(t *testing.T) {
	c := New(WithMaxEntries(1))

	// Add first entry - should succeed
	_, err := c.AddJob("* * * * *", FuncJob(func() {}))
	if err != nil {
		t.Fatalf("first AddJob failed: %v", err)
	}

	// Add second entry - should fail
	_, err = c.AddJob("* * * * *", FuncJob(func() {}))
	if !errors.Is(err, ErrMaxEntriesReached) {
		t.Errorf("expected ErrMaxEntriesReached, got: %v", err)
	}
}

func TestWithMaxEntries_ScheduleReturnsZero(t *testing.T) {
	c := New(
		WithMaxEntries(1),
		WithLogger(DiscardLogger), // Suppress error log
	)

	// Schedule first entry - should succeed
	id1 := c.Schedule(Every(time.Hour), FuncJob(func() {}))
	if id1 == 0 {
		t.Error("first Schedule should return valid ID")
	}

	// Schedule second entry - should return 0
	id2 := c.Schedule(Every(time.Hour), FuncJob(func() {}))
	if id2 != 0 {
		t.Error("second Schedule should return 0 when at limit")
	}
}

func TestWithMaxEntries_ZeroMeansUnlimited(t *testing.T) {
	c := New() // Default is 0 (unlimited)

	// Should be able to add many entries
	for i := 0; i < 100; i++ {
		_, err := c.AddFunc("* * * * *", func() {})
		if err != nil {
			t.Fatalf("AddFunc failed at iteration %d: %v", i, err)
		}
	}

	if len(c.Entries()) != 100 {
		t.Errorf("expected 100 entries, got %d", len(c.Entries()))
	}
}

func TestWithMaxEntries_ExplicitZeroIsUnlimited(t *testing.T) {
	c := New(WithMaxEntries(0)) // Explicit 0 = unlimited

	for i := 0; i < 50; i++ {
		_, err := c.AddFunc("* * * * *", func() {})
		if err != nil {
			t.Fatalf("AddFunc failed at iteration %d: %v", i, err)
		}
	}
}

func TestWithMaxEntries_RemoveAllowsNewEntries(t *testing.T) {
	c := New(WithMaxEntries(2))

	id1, _ := c.AddFunc("* * * * *", func() {})
	id2, _ := c.AddFunc("* * * * *", func() {})

	// At limit - should fail
	_, err := c.AddFunc("* * * * *", func() {})
	if !errors.Is(err, ErrMaxEntriesReached) {
		t.Fatal("expected ErrMaxEntriesReached")
	}

	// Remove one entry
	c.Remove(id1)

	// Should be able to add again
	id3, err := c.AddFunc("* * * * *", func() {})
	if err != nil {
		t.Fatalf("AddFunc after Remove failed: %v", err)
	}
	if id3 == 0 {
		t.Error("new entry should have valid ID")
	}

	// Verify correct entries remain
	entries := c.Entries()
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}

	var foundID2, foundID3 bool
	for _, e := range entries {
		if e.ID == id2 {
			foundID2 = true
		}
		if e.ID == id3 {
			foundID3 = true
		}
	}
	if !foundID2 || !foundID3 {
		t.Errorf("expected entries %d and %d, got %v", id2, id3, entries)
	}
}

func TestWithMaxEntries_WhileRunning(t *testing.T) {
	clock := NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	c := New(
		WithClock(clock),
		WithMaxEntries(2),
		WithLogger(DiscardLogger),
	)

	c.Start()
	defer c.Stop()

	// Add while running
	id1, err := c.AddFunc("@every 1h", func() {})
	if err != nil {
		t.Fatalf("first AddFunc while running failed: %v", err)
	}

	// Give time for the add to be processed
	time.Sleep(50 * time.Millisecond)

	id2, err := c.AddFunc("@every 1h", func() {})
	if err != nil {
		t.Fatalf("second AddFunc while running failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Third should fail
	id3, err := c.AddFunc("@every 1h", func() {})
	if !errors.Is(err, ErrMaxEntriesReached) {
		t.Errorf("expected ErrMaxEntriesReached, got: %v (id=%d)", err, id3)
	}

	// Verify entries
	entries := c.Entries()
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}

	_ = id1
	_ = id2
}

func TestWithMaxEntries_RemoveWhileRunning(t *testing.T) {
	clock := NewFakeClock(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	c := New(
		WithClock(clock),
		WithMaxEntries(1),
		WithLogger(DiscardLogger),
	)

	c.Start()
	defer c.Stop()

	// Add while running
	id1, err := c.AddFunc("@every 1h", func() {})
	if err != nil {
		t.Fatalf("first AddFunc failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// At limit - should fail
	_, err = c.AddFunc("@every 1h", func() {})
	if !errors.Is(err, ErrMaxEntriesReached) {
		t.Fatal("expected ErrMaxEntriesReached")
	}

	// Remove entry while running
	c.Remove(id1)
	time.Sleep(50 * time.Millisecond)

	// Should be able to add now
	_, err = c.AddFunc("@every 1h", func() {})
	if err != nil {
		t.Fatalf("AddFunc after Remove while running failed: %v", err)
	}
}

func TestWithMaxEntries_ErrorMessage(t *testing.T) {
	if ErrMaxEntriesReached.Error() != "cron: max entries limit reached" {
		t.Errorf("unexpected error message: %s", ErrMaxEntriesReached.Error())
	}
}

func TestWithMaxEntries_LimitOne(t *testing.T) {
	c := New(
		WithMaxEntries(1),
		WithLogger(DiscardLogger),
	)

	// First entry should work
	id, err := c.AddFunc("* * * * *", func() {})
	if err != nil {
		t.Fatalf("AddFunc failed: %v", err)
	}
	if id == 0 {
		t.Error("expected valid ID")
	}

	// Second should fail
	id2, err := c.AddFunc("* * * * *", func() {})
	if err == nil {
		t.Error("expected error")
	}
	if id2 != 0 {
		t.Error("expected invalid ID")
	}
}
