package cron

import (
	"container/heap"
)

// entryHeap implements heap.Interface for scheduling entries by next execution time.
// This provides O(log n) insertion and removal, and O(1) peek for the next entry.
type entryHeap []*Entry

// Len returns the number of entries in the heap.
func (h entryHeap) Len() int { return len(h) }

// Less reports whether entry i should fire before entry j.
// Zero times are considered "infinite" and sort to the end.
func (h entryHeap) Less(i, j int) bool {
	// Zero times sort to the end (highest priority = earliest time)
	if h[i].Next.IsZero() {
		return false
	}
	if h[j].Next.IsZero() {
		return true
	}
	return h[i].Next.Before(h[j].Next)
}

// Swap swaps elements i and j and updates their heap indices.
func (h entryHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].heapIndex = i
	h[j].heapIndex = j
}

// Push adds an entry to the heap.
// The type assertion is safe as container/heap always passes *Entry.
func (h *entryHeap) Push(x any) {
	entry := x.(*Entry) //nolint:errcheck // heap.Interface contract guarantees type
	entry.heapIndex = len(*h)
	*h = append(*h, entry)
}

// Pop removes and returns the minimum entry (earliest Next time).
// Returns nil if the heap is empty (defensive check for direct calls
// bypassing container/heap which already checks Len() > 0).
func (h *entryHeap) Pop() any {
	old := *h
	n := len(old)
	if n == 0 {
		return nil // defensive: prevent panic on empty heap
	}
	entry := old[n-1]
	old[n-1] = nil       // avoid memory leak
	entry.heapIndex = -1 // mark as removed
	*h = old[0 : n-1]
	return entry
}

// Peek returns the entry with the earliest Next time without removing it.
// Returns nil if the heap is empty.
func (h entryHeap) Peek() *Entry {
	if len(h) == 0 {
		return nil
	}
	return h[0]
}

// Update re-establishes heap ordering after an entry's Next time has changed.
// This is O(log n). The entry must still be in the heap at its recorded heapIndex.
func (h *entryHeap) Update(entry *Entry) {
	if entry.heapIndex >= 0 && entry.heapIndex < len(*h) && (*h)[entry.heapIndex] == entry {
		heap.Fix(h, entry.heapIndex)
	}
}

// RemoveAt removes the entry at the given heap index.
// This is O(log n) when the index is known. The entry pointer is validated
// to ensure the index hasn't become stale due to concurrent modifications.
// Returns true if the entry was found and removed.
func (h *entryHeap) RemoveAt(entry *Entry) bool {
	idx := entry.heapIndex
	if idx < 0 || idx >= len(*h) || (*h)[idx] != entry {
		return false
	}
	heap.Remove(h, idx)
	return true
}
