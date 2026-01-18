package cron

import (
	"fmt"
	"testing"
)

func TestWithCapacity(t *testing.T) {
	t.Run("pre-allocates maps and slice with specified capacity", func(t *testing.T) {
		c := New(WithCapacity(100))

		// Verify entryIndex map has capacity
		// Note: Go maps don't expose capacity directly, but we can verify they work
		if c.entryIndex == nil {
			t.Error("expected entryIndex to be initialized")
		}

		// Verify nameIndex map has capacity
		if c.nameIndex == nil {
			t.Error("expected nameIndex to be initialized")
		}

		// Verify entries slice has capacity (can check with cap())
		if cap(c.entries) != 100 {
			t.Errorf("expected entries capacity 100, got %d", cap(c.entries))
		}
	})

	t.Run("zero capacity does not pre-allocate", func(t *testing.T) {
		c := New(WithCapacity(0))

		// With zero capacity, entries should use default (nil or small)
		// The default New() creates entries as nil
		if cap(c.entries) != 0 {
			t.Errorf("expected entries capacity 0 with WithCapacity(0), got %d", cap(c.entries))
		}
	})

	t.Run("negative capacity does not pre-allocate", func(t *testing.T) {
		c := New(WithCapacity(-10))

		// Negative capacity should be treated as no pre-allocation
		if cap(c.entries) != 0 {
			t.Errorf("expected entries capacity 0 with negative capacity, got %d", cap(c.entries))
		}
	})

	t.Run("works with other options", func(t *testing.T) {
		c := New(
			WithCapacity(50),
			WithLocation(nil), // Use default
			WithLogger(DiscardLogger),
		)

		if cap(c.entries) != 50 {
			t.Errorf("expected entries capacity 50, got %d", cap(c.entries))
		}

		if c.logger != DiscardLogger {
			t.Error("expected DiscardLogger to be set")
		}
	})

	t.Run("option order does not matter", func(t *testing.T) {
		// WithCapacity first
		c1 := New(
			WithCapacity(75),
			WithLogger(DiscardLogger),
		)

		// WithCapacity last
		c2 := New(
			WithLogger(DiscardLogger),
			WithCapacity(75),
		)

		if cap(c1.entries) != 75 {
			t.Errorf("c1: expected entries capacity 75, got %d", cap(c1.entries))
		}
		if cap(c2.entries) != 75 {
			t.Errorf("c2: expected entries capacity 75, got %d", cap(c2.entries))
		}
	})
}

func TestWithCapacity_BulkAdditions(t *testing.T) {
	t.Run("adding entries up to capacity does not trigger reallocation", func(t *testing.T) {
		capacity := 100
		c := New(WithCapacity(capacity))

		// Get initial slice pointer
		initialPtr := &c.entries

		// Add entries up to capacity
		for i := 0; i < capacity; i++ {
			_, err := c.AddFunc("@every 1h", func() {})
			if err != nil {
				t.Fatalf("failed to add entry %d: %v", i, err)
			}
		}

		// Verify length matches
		if len(c.entries) != capacity {
			t.Errorf("expected %d entries, got %d", capacity, len(c.entries))
		}

		// Verify capacity hasn't changed (no reallocation needed)
		if cap(c.entries) != capacity {
			t.Errorf("expected capacity to remain %d, got %d (reallocation occurred)", capacity, cap(c.entries))
		}

		// Note: The slice header address should remain the same if no reallocation
		// occurred, but this is implementation detail. We can't easily test this.
		_ = initialPtr
	})

	t.Run("exceeding capacity triggers growth", func(t *testing.T) {
		capacity := 10
		c := New(WithCapacity(capacity))

		// Add more entries than capacity
		for i := 0; i < capacity+5; i++ {
			_, err := c.AddFunc("@every 1h", func() {})
			if err != nil {
				t.Fatalf("failed to add entry %d: %v", i, err)
			}
		}

		// Should have all entries
		if len(c.entries) != capacity+5 {
			t.Errorf("expected %d entries, got %d", capacity+5, len(c.entries))
		}

		// Capacity should have grown
		if cap(c.entries) <= capacity {
			t.Errorf("expected capacity to grow beyond %d, got %d", capacity, cap(c.entries))
		}
	})
}

// BenchmarkWithCapacity_BulkAdd compares bulk addition performance
// with and without pre-allocated capacity.
func BenchmarkWithCapacity_BulkAdd(b *testing.B) {
	numEntries := 1000

	b.Run("without_capacity", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			c := New()
			for j := 0; j < numEntries; j++ {
				c.AddFunc("@every 1h", func() {})
			}
		}
	})

	b.Run("with_capacity", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			c := New(WithCapacity(numEntries))
			for j := 0; j < numEntries; j++ {
				c.AddFunc("@every 1h", func() {})
			}
		}
	})
}

// BenchmarkWithCapacity_MapOperations benchmarks map access patterns
// with pre-allocated capacity.
func BenchmarkWithCapacity_MapOperations(b *testing.B) {
	for _, size := range []int{100, 500, 1000} {
		b.Run(fmt.Sprintf("size_%d", size), func(b *testing.B) {
			b.Run("with_capacity", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					c := New(WithCapacity(size))
					for j := 0; j < size; j++ {
						c.AddFunc("@every 1h", func() {})
					}
					// Access all entries
					_ = c.Entries()
				}
			})

			b.Run("without_capacity", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					c := New()
					for j := 0; j < size; j++ {
						c.AddFunc("@every 1h", func() {})
					}
					// Access all entries
					_ = c.Entries()
				}
			})
		})
	}
}
