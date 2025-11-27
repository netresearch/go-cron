package cron

import (
	"testing"
	"time"
)

// FuzzParseStandard tests the standard 5-field parser against arbitrary input.
// It verifies that the parser handles malformed input gracefully without panicking.
func FuzzParseStandard(f *testing.F) {
	// Seed corpus with valid expressions
	f.Add("* * * * *")
	f.Add("0 0 1 1 *")
	f.Add("*/5 * * * *")
	f.Add("0 0 * * MON-FRI")
	f.Add("0 9-17 * * *")
	f.Add("0,30 * * * *")
	f.Add("0 0 1,15 * *")

	// Predefined schedules
	f.Add("@yearly")
	f.Add("@monthly")
	f.Add("@weekly")
	f.Add("@daily")
	f.Add("@hourly")
	f.Add("@annually")
	f.Add("@midnight")

	// Interval schedules
	f.Add("@every 1h")
	f.Add("@every 30m")
	f.Add("@every 1h30m")
	f.Add("@every 5s")

	// Timezone prefixes
	f.Add("TZ=UTC * * * * *")
	f.Add("TZ=America/New_York 0 9 * * *")
	f.Add("CRON_TZ=Europe/London 0 0 * * *")

	// Edge cases
	f.Add("59 23 31 12 6")
	f.Add("0 0 1 1 0")
	f.Add("0-59 0-23 1-31 1-12 0-6")
	f.Add("*/1 */1 */1 */1 */1")

	// Invalid inputs that should not panic
	f.Add("")
	f.Add("    ")
	f.Add("invalid")
	f.Add("* * *")
	f.Add("60 * * * *")
	f.Add("-1 * * * *")
	f.Add("* 25 * * *")
	f.Add("* * 32 * *")
	f.Add("* * * 13 *")
	f.Add("* * * * 8")
	f.Add("*/0 * * * *")
	f.Add("5-3 * * * *")
	f.Add("TZ= * * * * *")
	f.Add("TZ=Invalid/Zone * * * * *")

	f.Fuzz(func(t *testing.T, spec string) {
		// Should not panic regardless of input
		_, _ = ParseStandard(spec)
	})
}

// FuzzParseWithSeconds tests the 6-field parser (with seconds) against arbitrary input.
func FuzzParseWithSeconds(f *testing.F) {
	// Seed corpus with valid 6-field expressions
	f.Add("0 * * * * *")
	f.Add("*/10 * * * * *")
	f.Add("0 0 0 1 1 *")
	f.Add("30 30 12 * * MON")
	f.Add("0-59 0-59 0-23 1-31 1-12 0-6")

	// Edge cases
	f.Add("59 59 23 31 12 6")
	f.Add("0 0 0 1 1 0")

	// Invalid inputs
	f.Add("")
	f.Add("* * * * *") // Missing seconds field for WithSeconds parser
	f.Add("60 * * * * *")
	f.Add("-1 * * * * *")

	parser := NewParser(Second | Minute | Hour | Dom | Month | Dow | Descriptor)

	f.Fuzz(func(t *testing.T, spec string) {
		// Should not panic regardless of input
		_, _ = parser.Parse(spec)
	})
}

// FuzzParseOptionalSeconds tests the parser with optional seconds field.
func FuzzParseOptionalSeconds(f *testing.F) {
	// 5-field expressions (no seconds)
	f.Add("* * * * *")
	f.Add("0 0 * * *")

	// 6-field expressions (with seconds)
	f.Add("0 * * * * *")
	f.Add("*/5 0 * * * *")

	// Mixed edge cases
	f.Add("")
	f.Add("invalid")
	f.Add("* * *")
	f.Add("* * * * * * *") // Too many fields

	parser := NewParser(SecondOptional | Minute | Hour | Dom | Month | Dow | Descriptor)

	f.Fuzz(func(t *testing.T, spec string) {
		// Should not panic regardless of input
		_, _ = parser.Parse(spec)
	})
}

// FuzzScheduleNext tests the schedule's Next() calculation against arbitrary times.
// This ensures the schedule calculation doesn't panic on edge case times.
func FuzzScheduleNext(f *testing.F) {
	// Various timestamps
	f.Add(int64(0))            // Unix epoch
	f.Add(int64(1609459200))   // 2021-01-01 00:00:00 UTC
	f.Add(int64(1735689600))   // 2025-01-01 00:00:00 UTC
	f.Add(int64(4102444800))   // 2100-01-01 00:00:00 UTC
	f.Add(int64(-62135596800)) // 0001-01-01 00:00:00 UTC
	f.Add(int64(253402300799)) // 9999-12-31 23:59:59 UTC
	f.Add(int64(1615705200))   // 2021-03-14 03:00:00 UTC (DST transition)
	f.Add(int64(1636268400))   // 2021-11-07 02:00:00 UTC (DST transition)
	f.Add(time.Now().Unix())   // Current time
	f.Add(time.Now().Add(time.Hour).Unix())

	// Create a simple schedule for testing
	schedule, err := ParseStandard("*/5 * * * *")
	if err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, timestamp int64) {
		// Bound the timestamp to reasonable values to avoid overflow
		if timestamp < -62135596800 || timestamp > 253402300799 {
			return
		}
		tm := time.Unix(timestamp, 0).UTC()
		// Should not panic regardless of input time
		_ = schedule.Next(tm)
	})
}

// FuzzConstantDelaySchedule tests the ConstantDelaySchedule against arbitrary times and durations.
func FuzzConstantDelaySchedule(f *testing.F) {
	f.Add(int64(0), int64(1000000000))             // 1 second
	f.Add(int64(1609459200), int64(60000000000))   // 1 minute
	f.Add(int64(1735689600), int64(3600000000000)) // 1 hour

	f.Fuzz(func(t *testing.T, timestamp int64, delayNanos int64) {
		// Bound inputs
		if timestamp < -62135596800 || timestamp > 253402300799 {
			return
		}
		if delayNanos < 1000000000 { // Minimum 1 second
			delayNanos = 1000000000
		}
		if delayNanos > 86400000000000 { // Maximum 1 day
			delayNanos = 86400000000000
		}

		tm := time.Unix(timestamp, 0).UTC()
		schedule := ConstantDelaySchedule{Delay: time.Duration(delayNanos)}
		// Should not panic
		_ = schedule.Next(tm)
	})
}
