package cron

import "time"

// NextN returns the next n execution times for the schedule, starting after t.
// Returns nil if schedule is nil or n <= 0.
//
// This is useful for:
//   - Calendar previews showing upcoming executions
//   - Capacity planning
//   - Debugging schedule expressions
//
// Example:
//
//	schedule, _ := cron.ParseStandard("0 9 * * MON-FRI")
//	times := cron.NextN(schedule, time.Now(), 10)
//	for _, t := range times {
//	    fmt.Println("Next run:", t)
//	}
func NextN(schedule Schedule, t time.Time, n int) []time.Time {
	if schedule == nil || n <= 0 {
		return nil
	}

	times := make([]time.Time, 0, n)
	current := t

	for range n {
		next := schedule.Next(current)
		if next.IsZero() {
			break
		}
		times = append(times, next)
		current = next
	}

	return times
}

// Between returns all execution times in the range [start, end).
// The end time is exclusive. Returns nil if schedule is nil.
//
// WARNING: For high-frequency schedules over long ranges, this can return
// many results. Use BetweenWithLimit for bounded queries.
//
// Example:
//
//	schedule, _ := cron.ParseStandard("0 9 * * *")
//	start := time.Now()
//	end := start.AddDate(0, 1, 0) // Next month
//	times := cron.Between(schedule, start, end)
func Between(schedule Schedule, start, end time.Time) []time.Time {
	return BetweenWithLimit(schedule, start, end, 0)
}

// BetweenWithLimit returns execution times in the range [start, end) up to limit.
// If limit is 0 or negative, no limit is applied.
// Returns nil if schedule is nil.
//
// Example:
//
//	schedule, _ := cron.ParseStandard("* * * * *") // Every minute
//	times := cron.BetweenWithLimit(schedule, start, end, 100) // Max 100 results
func BetweenWithLimit(schedule Schedule, start, end time.Time, limit int) []time.Time {
	if schedule == nil {
		return nil
	}

	if !start.Before(end) {
		return nil
	}

	var times []time.Time
	if limit > 0 {
		times = make([]time.Time, 0, limit)
	}

	current := start
	for {
		next := schedule.Next(current)
		if next.IsZero() || !next.Before(end) {
			break
		}
		times = append(times, next)
		current = next

		if limit > 0 && len(times) >= limit {
			break
		}
	}

	return times
}

// Count returns the number of executions in the range [start, end).
// The end time is exclusive. Returns 0 if schedule is nil.
//
// WARNING: For high-frequency schedules over long ranges, this may take
// significant time. Use CountWithLimit for bounded counting.
//
// Example:
//
//	schedule, _ := cron.ParseStandard("0 * * * *")
//	count := cron.Count(schedule, start, end)
//	fmt.Printf("Will run %d times\n", count)
func Count(schedule Schedule, start, end time.Time) int {
	return CountWithLimit(schedule, start, end, 0)
}

// CountWithLimit counts executions in the range [start, end) up to limit.
// If limit is 0 or negative, no limit is applied.
// Returns the count, which will be at most limit if a limit was specified.
// Returns 0 if schedule is nil.
//
// Example:
//
//	schedule, _ := cron.ParseStandard("* * * * *")
//	count := cron.CountWithLimit(schedule, start, end, 10000)
//	if count == 10000 {
//	    fmt.Println("At least 10000 executions")
//	}
func CountWithLimit(schedule Schedule, start, end time.Time, limit int) int {
	if schedule == nil {
		return 0
	}

	if !start.Before(end) {
		return 0
	}

	count := 0
	current := start

	for {
		next := schedule.Next(current)
		if next.IsZero() || !next.Before(end) {
			break
		}
		count++
		current = next

		if limit > 0 && count >= limit {
			break
		}
	}

	return count
}

// Matches reports whether the given time matches the schedule.
// This checks if t would be an execution time for the schedule.
//
// For minute-level schedules, seconds and nanoseconds in t are ignored.
// For second-level schedules, nanoseconds are ignored.
//
// Returns false if schedule is nil or doesn't implement ScheduleWithPrev.
//
// Example:
//
//	schedule, _ := cron.ParseStandard("0 9 * * MON-FRI")
//	if cron.Matches(schedule, time.Now()) {
//	    fmt.Println("Now is a scheduled execution time!")
//	}
func Matches(schedule Schedule, t time.Time) bool {
	if schedule == nil {
		return false
	}

	// Matches requires Prev() support
	sp, ok := schedule.(ScheduleWithPrev)
	if !ok {
		return false
	}

	// Use Prev to find the most recent scheduled time at or before t,
	// then check if it equals t (ignoring sub-second precision).
	//
	// We need to check from a point slightly after t to catch exact matches.
	// Add 1 second to ensure we catch the current time if it's a match.
	checkTime := t.Add(time.Second)
	prev := sp.Prev(checkTime)

	if prev.IsZero() {
		return false
	}

	// Compare at the appropriate precision.
	// For most schedules, minute precision is sufficient.
	// For second-enabled schedules, second precision is needed.
	//
	// We truncate both times to second precision for comparison.
	prevTrunc := prev.Truncate(time.Second)
	tTrunc := t.Truncate(time.Second)

	return prevTrunc.Equal(tTrunc)
}
