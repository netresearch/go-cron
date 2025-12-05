package cron

import (
	"container/heap"
	"testing"
	"time"
)

func TestHeapBasicOperations(t *testing.T) {
	h := &entryHeap{}
	heap.Init(h)

	// Test empty heap
	if h.Peek() != nil {
		t.Error("expected nil from empty heap Peek")
	}
	if h.Len() != 0 {
		t.Error("expected empty heap")
	}

	// Add entries with different times
	now := time.Now()
	entries := []*Entry{
		{ID: 1, Next: now.Add(3 * time.Hour), heapIndex: -1},
		{ID: 2, Next: now.Add(1 * time.Hour), heapIndex: -1},
		{ID: 3, Next: now.Add(2 * time.Hour), heapIndex: -1},
	}

	for _, e := range entries {
		heap.Push(h, e)
	}

	if h.Len() != 3 {
		t.Errorf("expected 3 entries, got %d", h.Len())
	}

	// Peek should return earliest (ID=2)
	if h.Peek().ID != 2 {
		t.Errorf("expected ID 2 at top, got %d", h.Peek().ID)
	}

	// Pop should return in order: 2, 3, 1
	expectedOrder := []EntryID{2, 3, 1}
	for _, expectedID := range expectedOrder {
		e := heap.Pop(h).(*Entry)
		if e.ID != expectedID {
			t.Errorf("expected ID %d, got %d", expectedID, e.ID)
		}
	}

	if h.Len() != 0 {
		t.Error("expected empty heap after popping all")
	}
}

func TestHeapZeroTimes(t *testing.T) {
	h := &entryHeap{}
	heap.Init(h)

	now := time.Now()
	entries := []*Entry{
		{ID: 1, Next: time.Time{}, heapIndex: -1}, // Zero time
		{ID: 2, Next: now.Add(1 * time.Hour), heapIndex: -1},
		{ID: 3, Next: time.Time{}, heapIndex: -1}, // Zero time
		{ID: 4, Next: now.Add(2 * time.Hour), heapIndex: -1},
	}

	for _, e := range entries {
		heap.Push(h, e)
	}

	// Non-zero times should come first
	e1 := heap.Pop(h).(*Entry)
	e2 := heap.Pop(h).(*Entry)

	if e1.ID != 2 && e1.ID != 4 {
		t.Errorf("expected ID 2 or 4 first, got %d", e1.ID)
	}
	if e2.ID != 2 && e2.ID != 4 {
		t.Errorf("expected ID 2 or 4 second, got %d", e2.ID)
	}

	// Zero times should be last
	e3 := heap.Pop(h).(*Entry)
	e4 := heap.Pop(h).(*Entry)

	if !e3.Next.IsZero() && !e4.Next.IsZero() {
		t.Error("expected zero times at end")
	}
}

func TestHeapUpdate(t *testing.T) {
	h := &entryHeap{}
	heap.Init(h)

	now := time.Now()
	e1 := &Entry{ID: 1, Next: now.Add(3 * time.Hour), heapIndex: -1}
	e2 := &Entry{ID: 2, Next: now.Add(1 * time.Hour), heapIndex: -1}
	e3 := &Entry{ID: 3, Next: now.Add(2 * time.Hour), heapIndex: -1}

	heap.Push(h, e1)
	heap.Push(h, e2)
	heap.Push(h, e3)

	// Initially e2 should be at top
	if h.Peek().ID != 2 {
		t.Errorf("expected ID 2 at top, got %d", h.Peek().ID)
	}

	// Update e1 to be the earliest
	e1.Next = now.Add(30 * time.Minute)
	h.Update(e1)

	// Now e1 should be at top
	if h.Peek().ID != 1 {
		t.Errorf("expected ID 1 at top after update, got %d", h.Peek().ID)
	}
}

func TestHeapUpdateStaleEntry(t *testing.T) {
	h := &entryHeap{}
	heap.Init(h)

	now := time.Now()
	e1 := &Entry{ID: 1, Next: now.Add(1 * time.Hour), heapIndex: -1}
	e2 := &Entry{ID: 2, Next: now.Add(2 * time.Hour), heapIndex: -1}

	heap.Push(h, e1)
	heap.Push(h, e2)

	// Remove e1 from heap
	heap.Remove(h, e1.heapIndex)

	// e1 now has heapIndex = -1, e2 is still in heap
	// Verify e1's heapIndex is -1 after removal
	if e1.heapIndex != -1 {
		t.Errorf("expected heapIndex -1 after removal, got %d", e1.heapIndex)
	}

	// Try to update e1 (stale entry) - should be a no-op, not corrupt heap
	e1.Next = now.Add(30 * time.Minute)
	e1.heapIndex = 0 // Simulate stale/corrupted heapIndex pointing to e2's position
	h.Update(e1)

	// Heap should still be valid with only e2
	if h.Len() != 1 {
		t.Errorf("expected 1 entry, got %d", h.Len())
	}
	if h.Peek().ID != 2 {
		t.Errorf("expected ID 2 at top, got %d", h.Peek().ID)
	}
}

func TestHeapRemoveAt(t *testing.T) {
	h := &entryHeap{}
	heap.Init(h)

	now := time.Now()
	entries := []*Entry{
		{ID: 1, Next: now.Add(1 * time.Hour), heapIndex: -1},
		{ID: 2, Next: now.Add(2 * time.Hour), heapIndex: -1},
		{ID: 3, Next: now.Add(3 * time.Hour), heapIndex: -1},
	}

	for _, e := range entries {
		heap.Push(h, e)
	}

	// Remove middle entry using RemoveAt
	if !h.RemoveAt(entries[1]) {
		t.Error("expected RemoveAt to return true for existing entry")
	}
	if h.Len() != 2 {
		t.Errorf("expected 2 entries after removal, got %d", h.Len())
	}

	// Try to remove already-removed entry (stale pointer)
	if h.RemoveAt(entries[1]) {
		t.Error("expected RemoveAt to return false for already-removed entry")
	}

	// Try to remove entry with invalid heapIndex
	fakeEntry := &Entry{ID: 99, heapIndex: -1}
	if h.RemoveAt(fakeEntry) {
		t.Error("expected RemoveAt to return false for entry with invalid heapIndex")
	}

	// Verify remaining entries
	e1 := heap.Pop(h).(*Entry)
	e2 := heap.Pop(h).(*Entry)
	if e1.ID != 1 || e2.ID != 3 {
		t.Errorf("unexpected entries after removal: %d, %d", e1.ID, e2.ID)
	}
}

func TestHeapIndices(t *testing.T) {
	h := &entryHeap{}
	heap.Init(h)

	now := time.Now()
	entries := []*Entry{
		{ID: 1, Next: now.Add(3 * time.Hour), heapIndex: -1},
		{ID: 2, Next: now.Add(1 * time.Hour), heapIndex: -1},
		{ID: 3, Next: now.Add(2 * time.Hour), heapIndex: -1},
	}

	for _, e := range entries {
		heap.Push(h, e)
	}

	// Verify all heapIndex values are valid
	for i, e := range *h {
		if e.heapIndex != i {
			t.Errorf("entry %d has heapIndex %d, expected %d", e.ID, e.heapIndex, i)
		}
	}

	// After pop, removed entry should have heapIndex = -1
	popped := heap.Pop(h).(*Entry)
	if popped.heapIndex != -1 {
		t.Errorf("popped entry has heapIndex %d, expected -1", popped.heapIndex)
	}
}

func BenchmarkHeapPush(b *testing.B) {
	now := time.Now()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		h := &entryHeap{}
		heap.Init(h)
		for j := 0; j < 1000; j++ {
			e := &Entry{
				ID:        EntryID(j),
				Next:      now.Add(time.Duration(j) * time.Second),
				heapIndex: -1,
			}
			heap.Push(h, e)
		}
	}
}

func BenchmarkHeapPopPush(b *testing.B) {
	now := time.Now()
	h := &entryHeap{}
	heap.Init(h)

	// Pre-populate with 1000 entries
	for j := 0; j < 1000; j++ {
		e := &Entry{
			ID:        EntryID(j),
			Next:      now.Add(time.Duration(j) * time.Second),
			heapIndex: -1,
		}
		heap.Push(h, e)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := heap.Pop(h).(*Entry)
		e.Next = now.Add(time.Duration(1000+i) * time.Second)
		heap.Push(h, e)
	}
}

func BenchmarkHeapUpdate(b *testing.B) {
	now := time.Now()
	h := &entryHeap{}
	heap.Init(h)

	// Pre-populate with 1000 entries
	entries := make([]*Entry, 1000)
	for j := 0; j < 1000; j++ {
		e := &Entry{
			ID:        EntryID(j),
			Next:      now.Add(time.Duration(j) * time.Second),
			heapIndex: -1,
		}
		entries[j] = e
		heap.Push(h, e)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := entries[i%1000]
		e.Next = now.Add(time.Duration(i) * time.Millisecond)
		h.Update(e)
	}
}

func TestRemoveAt(t *testing.T) {
	h := &entryHeap{}
	heap.Init(h)

	now := time.Now()
	entries := []*Entry{
		{ID: 1, Next: now.Add(1 * time.Hour), heapIndex: -1},
		{ID: 2, Next: now.Add(2 * time.Hour), heapIndex: -1},
		{ID: 3, Next: now.Add(3 * time.Hour), heapIndex: -1},
	}

	for _, e := range entries {
		heap.Push(h, e)
	}

	// Remove middle entry using RemoveAt
	if !h.RemoveAt(entries[1]) {
		t.Error("expected RemoveAt to return true for existing entry")
	}
	if h.Len() != 2 {
		t.Errorf("expected 2 entries after removal, got %d", h.Len())
	}

	// Try to remove entry that's no longer in heap (stale reference)
	if h.RemoveAt(entries[1]) {
		t.Error("expected RemoveAt to return false for already-removed entry")
	}

	// Verify remaining entries
	e1 := heap.Pop(h).(*Entry)
	e2 := heap.Pop(h).(*Entry)
	if e1.ID != 1 || e2.ID != 3 {
		t.Errorf("unexpected entries after removal: %d, %d", e1.ID, e2.ID)
	}
}

func TestRemoveAtStaleIndex(t *testing.T) {
	h := &entryHeap{}
	heap.Init(h)

	now := time.Now()
	e1 := &Entry{ID: 1, Next: now.Add(1 * time.Hour), heapIndex: -1}
	e2 := &Entry{ID: 2, Next: now.Add(2 * time.Hour), heapIndex: -1}

	heap.Push(h, e1)
	heap.Push(h, e2)

	// Remove e1 via RemoveAt
	h.RemoveAt(e1)

	// Create a fake entry with a stale heapIndex pointing to e2's position
	stale := &Entry{ID: 99, Next: now, heapIndex: 0}
	if h.RemoveAt(stale) {
		t.Error("expected RemoveAt to return false when entry pointer doesn't match")
	}

	// Heap should still have e2
	if h.Len() != 1 {
		t.Errorf("expected 1 entry, got %d", h.Len())
	}
	if h.Peek().ID != 2 {
		t.Errorf("expected ID 2 at top, got %d", h.Peek().ID)
	}
}

// TestUpdateBoundaryConditions tests the heap Update function boundary checks
// at heap.go:70 to kill the mutation where `entry.heapIndex < len(*h)` could
// become `entry.heapIndex <= len(*h)`.
func TestUpdateBoundaryConditions(t *testing.T) {
	h := &entryHeap{}
	heap.Init(h)

	now := time.Now()

	t.Run("heapIndex equals len is out of bounds", func(t *testing.T) {
		// Add one entry
		e1 := &Entry{ID: 1, Next: now.Add(1 * time.Hour), heapIndex: -1}
		heap.Push(h, e1)

		// Create a fake entry with heapIndex = len(heap), which is out of bounds
		fake := &Entry{ID: 99, Next: now, heapIndex: len(*h)} // heapIndex = 1, but len = 1

		// Update should be a no-op because heapIndex >= len(*h)
		h.Update(fake)

		// Heap should still be valid with just e1
		if h.Len() != 1 {
			t.Errorf("expected 1 entry, got %d", h.Len())
		}
		if h.Peek().ID != 1 {
			t.Errorf("expected ID 1 at top, got %d", h.Peek().ID)
		}
	})

	t.Run("heapIndex at boundary minus one is valid", func(t *testing.T) {
		// Reset heap
		*h = nil
		heap.Init(h)

		e1 := &Entry{ID: 1, Next: now.Add(2 * time.Hour), heapIndex: -1}
		e2 := &Entry{ID: 2, Next: now.Add(1 * time.Hour), heapIndex: -1}
		heap.Push(h, e1)
		heap.Push(h, e2)

		// e2 should be at top (earlier time)
		if h.Peek().ID != 2 {
			t.Fatalf("expected ID 2 at top initially, got %d", h.Peek().ID)
		}

		// Update e1 to have earlier time - this should reorder the heap
		e1.Next = now.Add(30 * time.Minute)
		h.Update(e1)

		// Now e1 should be at top
		if h.Peek().ID != 1 {
			t.Errorf("expected ID 1 at top after update, got %d", h.Peek().ID)
		}
	})

	t.Run("heapIndex negative is rejected", func(t *testing.T) {
		// Reset heap
		*h = nil
		heap.Init(h)

		e1 := &Entry{ID: 1, Next: now.Add(1 * time.Hour), heapIndex: -1}
		heap.Push(h, e1)

		// Create entry with negative heapIndex
		fake := &Entry{ID: 99, Next: now, heapIndex: -1}

		// Update should be a no-op
		h.Update(fake)

		// Heap should still be valid
		if h.Len() != 1 {
			t.Errorf("expected 1 entry, got %d", h.Len())
		}
	})

	t.Run("heapIndex pointing to wrong entry is rejected", func(t *testing.T) {
		// Reset heap
		*h = nil
		heap.Init(h)

		e1 := &Entry{ID: 1, Next: now.Add(1 * time.Hour), heapIndex: -1}
		e2 := &Entry{ID: 2, Next: now.Add(2 * time.Hour), heapIndex: -1}
		heap.Push(h, e1)
		heap.Push(h, e2)

		// Create fake entry pointing to e1's position but not being e1
		fake := &Entry{ID: 99, Next: now, heapIndex: e1.heapIndex}

		// Update should be a no-op because (*h)[heapIndex] != fake
		h.Update(fake)

		// Heap order should be unchanged
		if h.Peek().ID != 1 {
			t.Errorf("expected ID 1 at top, got %d", h.Peek().ID)
		}
	})
}

// TestRemoveAtBoundaryIdxEqualsLen tests heap.go:81 boundary condition where
// idx >= len(*h) check must correctly reject idx == len(*h).
// This kills the CONDITIONALS_BOUNDARY mutation where >= could become >.
func TestRemoveAtBoundaryIdxEqualsLen(t *testing.T) {
	h := &entryHeap{}
	heap.Init(h)

	now := time.Now()

	// Add exactly 2 entries to heap
	e1 := &Entry{ID: 1, Next: now.Add(1 * time.Hour), heapIndex: -1}
	e2 := &Entry{ID: 2, Next: now.Add(2 * time.Hour), heapIndex: -1}
	heap.Push(h, e1)
	heap.Push(h, e2)

	// Heap has len = 2, valid indices are 0 and 1
	// Create fake entry with heapIndex = 2 (equals len, out of bounds)
	fake := &Entry{ID: 99, Next: now, heapIndex: 2}

	// RemoveAt should return false because idx >= len(*h)
	// With mutation >= → >, it would incorrectly pass the check when idx == len
	if h.RemoveAt(fake) {
		t.Error("RemoveAt should return false when heapIndex equals len(*h)")
	}

	// Heap should still have exactly 2 entries
	if h.Len() != 2 {
		t.Errorf("expected 2 entries, got %d", h.Len())
	}

	// Verify order is preserved
	if h.Peek().ID != 1 {
		t.Errorf("expected ID 1 at top, got %d", h.Peek().ID)
	}
}

// TestRemoveAtIdxLastValid tests that idx = len(*h) - 1 (last valid index) works.
// This is the "just inside boundary" case.
func TestRemoveAtIdxLastValid(t *testing.T) {
	h := &entryHeap{}
	heap.Init(h)

	now := time.Now()

	// Add exactly 3 entries
	e1 := &Entry{ID: 1, Next: now.Add(1 * time.Hour), heapIndex: -1}
	e2 := &Entry{ID: 2, Next: now.Add(2 * time.Hour), heapIndex: -1}
	e3 := &Entry{ID: 3, Next: now.Add(3 * time.Hour), heapIndex: -1}
	heap.Push(h, e1)
	heap.Push(h, e2)
	heap.Push(h, e3)

	// Find which entry is at the last position (len-1)
	lastIdx := len(*h) - 1
	lastEntry := (*h)[lastIdx]

	// RemoveAt should succeed for valid last index
	if !h.RemoveAt(lastEntry) {
		t.Error("RemoveAt should succeed for entry at last valid index")
	}

	if h.Len() != 2 {
		t.Errorf("expected 2 entries after removal, got %d", h.Len())
	}
}

// BenchmarkRemoveAtWithIndex benchmarks the new O(1) lookup + O(log n) removal.
// Uses a pre-built heap and index, measuring only the removal operation.
func BenchmarkRemoveAtWithIndex(b *testing.B) {
	const size = 1000
	now := time.Now()

	// Pre-build heap and index once
	h := &entryHeap{}
	heap.Init(h)
	index := make(map[EntryID]*Entry)
	for j := 0; j < size; j++ {
		e := &Entry{
			ID:        EntryID(j + 1),
			Next:      now.Add(time.Duration(j) * time.Second),
			heapIndex: -1,
		}
		heap.Push(h, e)
		index[e.ID] = e
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Remove and re-add to keep heap size constant
		targetID := EntryID((i % size) + 1)
		if entry, ok := index[targetID]; ok {
			h.RemoveAt(entry)
			delete(index, targetID)
		}
		e := &Entry{
			ID:        targetID,
			Next:      now.Add(time.Duration(size+i) * time.Second),
			heapIndex: -1,
		}
		heap.Push(h, e)
		index[e.ID] = e
	}
}
