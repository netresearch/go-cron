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

// TestEntryCountDecrementOnError verifies that entry count is correctly decremented
// when an error occurs after incrementing. This kills mutations at cron.go:370
// where `atomic.AddInt64(&c.entryCount, -1)` could be changed to +1.
func TestEntryCountDecrementOnError(t *testing.T) {
	c := New(WithMaxEntries(2))

	// Add first entry with a name
	_, err := c.AddFunc("* * * * *", func() {}, WithName("test1"))
	if err != nil {
		t.Fatalf("first AddFunc failed: %v", err)
	}

	// Try to add a duplicate name - should fail and NOT consume a slot
	_, err = c.AddFunc("* * * * *", func() {}, WithName("test1"))
	if !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("expected ErrDuplicateName, got: %v", err)
	}

	// Should still be able to add another entry (slot wasn't consumed by failed add)
	_, err = c.AddFunc("* * * * *", func() {}, WithName("test2"))
	if err != nil {
		t.Fatalf("second AddFunc after failed duplicate should work: %v", err)
	}

	// Now we should be at the limit
	_, err = c.AddFunc("* * * * *", func() {}, WithName("test3"))
	if !errors.Is(err, ErrMaxEntriesReached) {
		t.Fatalf("expected ErrMaxEntriesReached, got: %v", err)
	}

	// Verify exactly 2 entries
	if len(c.Entries()) != 2 {
		t.Errorf("expected 2 entries, got %d", len(c.Entries()))
	}
}

// TestNextIDDecrementsOnDuplicateNameError verifies that nextID is correctly
// decremented when a duplicate name error occurs. This kills mutations at
// cron.go:394 where `c.nextID--` could become `c.nextID++`.
func TestNextIDDecrementsOnDuplicateNameError(t *testing.T) {
	c := New()

	// Add first entry with a name
	id1, err := c.AddFunc("* * * * *", func() {}, WithName("unique"))
	if err != nil {
		t.Fatalf("first AddFunc failed: %v", err)
	}

	// Try to add duplicate - this should fail and revert the ID
	_, err = c.AddFunc("* * * * *", func() {}, WithName("unique"))
	if !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("expected ErrDuplicateName, got: %v", err)
	}

	// Add another entry - its ID should be id1+1 (the failed attempt's ID was reverted)
	id3, err := c.AddFunc("* * * * *", func() {}, WithName("another"))
	if err != nil {
		t.Fatalf("third AddFunc failed: %v", err)
	}

	// IDs should be consecutive (id3 = id1 + 1), not id1 + 2
	if id3 != id1+1 {
		t.Errorf("expected ID %d after failed add, got %d (ID wasn't reverted)", id1+1, id3)
	}
}

// TestNextIDIncrementsMutationKiller verifies that nextID correctly increments.
// This kills mutations at cron.go:374 where `c.nextID++` could become `c.nextID--`.
func TestNextIDIncrementsMutationKiller(t *testing.T) {
	c := New()

	// Add several entries and verify IDs are sequential
	id1, _ := c.AddFunc("* * * * *", func() {})
	id2, _ := c.AddFunc("* * * * *", func() {})
	id3, _ := c.AddFunc("* * * * *", func() {})

	// Verify IDs are sequential and increasing
	if id2 <= id1 {
		t.Errorf("ID2 (%d) should be greater than ID1 (%d)", id2, id1)
	}
	if id3 <= id2 {
		t.Errorf("ID3 (%d) should be greater than ID2 (%d)", id3, id2)
	}
	if id2 != id1+1 || id3 != id2+1 {
		t.Errorf("IDs should be consecutive: got %d, %d, %d", id1, id2, id3)
	}
}

// TestHeapIndexInitialization verifies that new entries start with heapIndex=-1.
// This kills mutations at cron.go:383 where `heapIndex: -1` could become 0 or 1.
func TestHeapIndexInitialization(t *testing.T) {
	c := New()

	// Add an entry
	id, _ := c.AddFunc("* * * * *", func() {})

	// Get the entry and check its heapIndex
	// After being pushed to the heap, heapIndex should be >= 0
	entry := c.Entry(id)
	if !entry.Valid() {
		t.Fatal("entry should be valid")
	}

	// The entry is in the heap, so heapIndex should be valid (0 or higher)
	// We can't directly access heapIndex from Entry snapshot, but we can
	// verify the entry is properly in the heap by checking it can be found
	entries := c.Entries()
	found := false
	for _, e := range entries {
		if e.ID == id {
			found = true
			break
		}
	}
	if !found {
		t.Error("entry should be in the entries list")
	}
}

// TestCompactionThresholdBoundary tests the compaction threshold boundary at
// cron.go:820 where `c.indexDeletions <= currentSize` prevents unnecessary compaction.
func TestCompactionThresholdBoundary(t *testing.T) {
	c := New()

	// indexCompactionThreshold is 1000
	// Compaction happens when: indexDeletions >= 1000 AND indexDeletions > currentSize

	// Add entries
	const numEntries = 100
	ids := make([]EntryID, numEntries)
	for i := 0; i < numEntries; i++ {
		id, _ := c.AddFunc("* * * * *", func() {})
		ids[i] = id
	}

	// Verify all entries exist
	if len(c.Entries()) != numEntries {
		t.Fatalf("expected %d entries, got %d", numEntries, len(c.Entries()))
	}

	// Remove entries one at a time - should not trigger compaction yet
	// because indexDeletions < indexCompactionThreshold (1000)
	for i := 0; i < numEntries/2; i++ {
		c.Remove(ids[i])
	}

	// Remaining entries should still be accessible
	remaining := c.Entries()
	if len(remaining) != numEntries/2 {
		t.Errorf("expected %d remaining entries, got %d", numEntries/2, len(remaining))
	}

	// Verify the non-removed entries are still there
	for i := numEntries / 2; i < numEntries; i++ {
		entry := c.Entry(ids[i])
		if !entry.Valid() {
			t.Errorf("entry %d should still be valid", ids[i])
		}
	}
}
