package cron

import (
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

func loadLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("%s timezone not available", name)
	}
	return loc
}

// TestNextDSTGapRun pins the run that Next returns for a time skipped by a
// spring-forward gap of any length and at any hour: the time shifted by the
// length of the gap, as for the one-hour gap at 02:00 (ADR-016).
func TestNextDSTGapRun(t *testing.T) {
	lordHowe := loadLocation(t, "Australia/Lord_Howe") // 2026-10-04 02:00 -> 02:30
	santiago := loadLocation(t, "America/Santiago")    // 2026-09-06 00:00 -> 01:00
	la := loadLocation(t, "America/Los_Angeles")       // 2026-03-08 02:00 -> 03:00
	apia := loadLocation(t, "Pacific/Apia")            // 2011-12-30 skipped as a whole day
	tests := []struct {
		name string
		spec string
		from time.Time
		want time.Time
	}{
		// Issue #444: a 30-minute gap and a gap at midnight.
		{"30-minute gap", "15 2 * * *", time.Date(2026, 10, 3, 2, 15, 0, 0, lordHowe), time.Date(2026, 10, 4, 2, 45, 0, 0, lordHowe)},
		{"30-minute gap, before run in remaining hour", "15,40 2 * * *", time.Date(2026, 10, 4, 1, 0, 0, 0, lordHowe), time.Date(2026, 10, 4, 2, 40, 0, 0, lordHowe)},
		{"30-minute gap, shifted run after real run", "15,40 2 * * *", time.Date(2026, 10, 4, 2, 40, 0, 0, lordHowe), time.Date(2026, 10, 4, 2, 45, 0, 0, lordHowe)},
		{"30-minute gap, after shifted run", "15,40 2 * * *", time.Date(2026, 10, 4, 2, 45, 0, 0, lordHowe), time.Date(2026, 10, 5, 2, 15, 0, 0, lordHowe)},
		{"30-minute gap, run at gap end", "0,30 2 * * *", time.Date(2026, 10, 4, 1, 0, 0, 0, lordHowe), time.Date(2026, 10, 4, 2, 30, 0, 0, lordHowe)},
		{"30-minute gap, coalesced with gap end", "0,30 2 * * *", time.Date(2026, 10, 4, 2, 30, 0, 0, lordHowe), time.Date(2026, 10, 5, 2, 0, 0, 0, lordHowe)},
		{"30-minute gap, one run per gap", "5,15 2 * * *", time.Date(2026, 10, 4, 1, 0, 0, 0, lordHowe), time.Date(2026, 10, 4, 2, 35, 0, 0, lordHowe)},
		{"30-minute gap, no second gap run", "5,15 2 * * *", time.Date(2026, 10, 4, 2, 35, 0, 0, lordHowe), time.Date(2026, 10, 5, 2, 5, 0, 0, lordHowe)},
		{"gap at midnight", "30 0 * * *", time.Date(2026, 9, 5, 0, 30, 0, 0, santiago), time.Date(2026, 9, 6, 1, 30, 0, 0, santiago)},
		{"gap at midnight, day-restricted", "30 0 6 9 *", time.Date(2026, 9, 1, 0, 0, 0, 0, santiago), time.Date(2026, 9, 6, 1, 30, 0, 0, santiago)},
		{"gap at midnight, real hour after", "0 1 * * *", time.Date(2026, 9, 5, 12, 0, 0, 0, santiago), time.Date(2026, 9, 6, 1, 0, 0, 0, santiago)},
		{"gap day, real run after gap", "0 1 6 9 *", time.Date(2026, 9, 1, 0, 0, 0, 0, santiago), time.Date(2026, 9, 6, 1, 0, 0, 0, santiago)},
		{"day before gap day", "0 1 5 9 *", time.Date(2026, 9, 5, 12, 0, 0, 0, santiago), time.Date(2027, 9, 5, 1, 0, 0, 0, santiago)},
		{"control: one-hour gap", "30 2 * * *", time.Date(2026, 3, 7, 2, 30, 0, 0, la), time.Date(2026, 3, 8, 3, 30, 0, 0, la)},
		// Issue #445: Next from the last second before the gap.
		{"last second before gap", "30 2 * * *", time.Date(2026, 3, 8, 1, 59, 59, 0, la), time.Date(2026, 3, 8, 3, 30, 0, 0, la)},
		{"two seconds before gap", "30 2 * * *", time.Date(2026, 3, 8, 1, 59, 58, 0, la), time.Date(2026, 3, 8, 3, 30, 0, 0, la)},
		{"last fraction before gap", "30 2 * * *", time.Date(2026, 3, 8, 1, 59, 59, 999999999, la), time.Date(2026, 3, 8, 3, 30, 0, 0, la)},
		{"at gap end", "30 2 * * *", time.Date(2026, 3, 8, 3, 0, 0, 0, la), time.Date(2026, 3, 8, 3, 30, 0, 0, la)},
		{"inside hour after gap", "30 2 * * *", time.Date(2026, 3, 8, 3, 10, 0, 0, la), time.Date(2026, 3, 8, 3, 30, 0, 0, la)},
		{"last second before shifted run", "30 2 * * *", time.Date(2026, 3, 8, 3, 29, 59, 0, la), time.Date(2026, 3, 8, 3, 30, 0, 0, la)},
		{"at shifted run", "30 2 * * *", time.Date(2026, 3, 8, 3, 30, 0, 0, la), time.Date(2026, 3, 9, 2, 30, 0, 0, la)},
		{"whole skipped day", "0 12 * * *", time.Date(2011, 12, 29, 12, 0, 0, 0, apia), time.Date(2011, 12, 31, 12, 0, 0, 0, apia)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sched, err := ParseStandard(tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			if got := sched.Next(tc.from); !got.Equal(tc.want) {
				t.Errorf("Next(%v) = %v, want %v", tc.from, got, tc.want)
			}
		})
	}
}

// TestNextDSTGapRun_Seconds covers issue #445 with a seconds field.
func TestNextDSTGapRun_Seconds(t *testing.T) {
	la := loadLocation(t, "America/Los_Angeles")
	sched, err := NewParser(Second | Minute | Hour | Dom | Month | Dow).Parse("0 30 2 * * *")
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 3, 8, 1, 59, 59, 0, la)
	want := time.Date(2026, 3, 8, 3, 30, 0, 0, la)
	if got := sched.Next(from); !got.Equal(want) {
		t.Errorf("Next(%v) = %v, want %v", from, got, want)
	}
}

// TestNextDSTGapRun_DSTSkip checks that DSTSkip drops the gap run for every
// gap length, as robfig/cron does.
func TestNextDSTGapRun_DSTSkip(t *testing.T) {
	lordHowe := loadLocation(t, "Australia/Lord_Howe")
	santiago := loadLocation(t, "America/Santiago")
	la := loadLocation(t, "America/Los_Angeles")
	tests := []struct {
		spec string
		from time.Time
		want time.Time
	}{
		{"15 2 * * *", time.Date(2026, 10, 3, 2, 15, 0, 0, lordHowe), time.Date(2026, 10, 5, 2, 15, 0, 0, lordHowe)},
		{"30 0 * * *", time.Date(2026, 9, 5, 0, 30, 0, 0, santiago), time.Date(2026, 9, 7, 0, 30, 0, 0, santiago)},
		{"30 2 * * *", time.Date(2026, 3, 8, 1, 59, 59, 0, la), time.Date(2026, 3, 9, 2, 30, 0, 0, la)},
	}
	for _, tc := range tests {
		t.Run(tc.spec, func(t *testing.T) {
			sched, err := NewParser(Minute | Hour | Dom | Month | Dow | DSTSkip).Parse(tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			if got := sched.Next(tc.from); !got.Equal(tc.want) {
				t.Errorf("Next(%v) = %v, want %v", tc.from, got, tc.want)
			}
		})
	}
}

// TestPrevNextDSTGapSymmetry checks that Prev yields the same runs as Next
// across gaps that are not one hour at 02:00.
func TestPrevNextDSTGapSymmetry(t *testing.T) {
	lordHowe := loadLocation(t, "Australia/Lord_Howe")
	santiago := loadLocation(t, "America/Santiago")
	la := loadLocation(t, "America/Los_Angeles")
	zones := []struct {
		loc        *time.Location
		start, end time.Time
		specs      []string
	}{
		{
			lordHowe, time.Date(2026, 10, 3, 0, 0, 0, 0, lordHowe), time.Date(2026, 10, 5, 12, 0, 0, 0, lordHowe),
			[]string{"15 2 * * *", "15,40 2 * * *", "0,30 2 * * *", "*/10 2 * * *", "0 * * * *", "45 1,2 * * *"},
		},
		{
			santiago, time.Date(2026, 9, 5, 0, 0, 0, 0, santiago), time.Date(2026, 9, 7, 12, 0, 0, 0, santiago),
			[]string{"30 0 * * *", "0 0 * * *", "*/20 0,1 * * *", "30 0 6 9 *", "59 23 * * *"},
		},
		{
			la, time.Date(2026, 3, 7, 0, 0, 0, 0, la), time.Date(2026, 3, 9, 12, 0, 0, 0, la),
			[]string{"30 2 * * *", "59 1 * * *", "0 3 * * *"},
		},
	}
	for _, z := range zones {
		for _, spec := range z.specs {
			for _, skip := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/DSTSkip=%t", z.loc, spec, skip), func(t *testing.T) {
					checkSymmetry(t, z.loc, spec, skip, z.start, z.end)
				})
			}
		}
	}
}

// checkSymmetry checks that Next and Prev list the same runs of spec in loc
// between start and end.
func checkSymmetry(t *testing.T, loc *time.Location, spec string, skip bool, start, end time.Time) {
	t.Helper()
	options := Minute | Hour | Dom | Month | Dow
	if skip {
		options |= DSTSkip
	}
	sched, err := NewParser(options).Parse("TZ=" + loc.String() + " " + spec)
	if err != nil {
		t.Fatal(err)
	}
	forward, backward := runsBetween(sched, start, end)
	if !slices.EqualFunc(forward, backward, time.Time.Equal) {
		t.Errorf("Next and Prev disagree\nnext: %v\nprev: %v", forward, backward)
	}
}

// TestDSTGap_SchedulerRun drives the run loop over the gaps from issues #444
// and #445 and checks that the transition day has exactly one run.
func TestDSTGap_SchedulerRun(t *testing.T) {
	tests := []struct {
		name  string
		zone  string
		spec  string
		start time.Time // wall clock in zone
		want  []time.Time
	}{
		{
			"30-minute gap", "Australia/Lord_Howe", "15 2 * * *", time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC),
			[]time.Time{time.Date(2026, 10, 3, 2, 15, 0, 0, time.UTC), time.Date(2026, 10, 4, 2, 45, 0, 0, time.UTC), time.Date(2026, 10, 5, 2, 15, 0, 0, time.UTC)},
		},
		{
			"gap at midnight", "America/Santiago", "30 0 * * *", time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC),
			[]time.Time{time.Date(2026, 9, 5, 0, 30, 0, 0, time.UTC), time.Date(2026, 9, 6, 1, 30, 0, 0, time.UTC), time.Date(2026, 9, 7, 0, 30, 0, 0, time.UTC)},
		},
		{
			"started in the last second before the gap", "America/Los_Angeles", "30 2 * * *", time.Date(2026, 3, 8, 1, 59, 59, 0, time.UTC),
			[]time.Time{time.Date(2026, 3, 8, 3, 30, 0, 0, time.UTC), time.Date(2026, 3, 9, 2, 30, 0, 0, time.UTC)},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			loc := loadLocation(t, tc.zone)
			inZone := func(w time.Time) time.Time {
				return time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), w.Second(), 0, loc)
			}
			got, runs := stepScheduler(t, loc, tc.spec, inZone(tc.start), len(tc.want))
			for i, w := range tc.want {
				if !got[i].Equal(inZone(w)) {
					t.Errorf("run %d = %v, want %v", i, got[i].In(loc), inZone(w))
				}
			}
			if runs != len(tc.want) {
				t.Errorf("got %d runs, want %d", runs, len(tc.want))
			}
		})
	}
}

// stepScheduler drives the run loop from start through n runs of spec and
// returns their scheduled times and how many times the job ran.
func stepScheduler(t *testing.T, loc *time.Location, spec string, start time.Time, n int) ([]time.Time, int) {
	t.Helper()
	clock := NewFakeClock(start)
	c := New(WithClock(clock), WithLocation(loc))
	var runs atomic.Int32
	if _, err := c.AddFunc(spec, func() { runs.Add(1) }); err != nil {
		t.Fatal(err)
	}
	c.Start()
	defer c.Stop()

	var got []time.Time
	for range n {
		clock.BlockUntil(1)
		next := c.Entries()[0].Next
		got = append(got, next)
		clock.Set(next)
		deadline := time.Now().Add(2 * time.Second)
		for c.Entries()[0].Next.Equal(next) && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
	}
	// Jobs run in their own goroutines; wait for the last one.
	deadline := time.Now().Add(2 * time.Second)
	for int(runs.Load()) < n && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	return got, int(runs.Load())
}

// gapOracle lists the runs of a minute-resolution schedule between start and
// end without the gap code in spec.go: every existing wall time that matches,
// plus, for each spring-forward gap and unless DSTSkip is set, the first
// skipped matching wall time read in the offset before the gap.
func gapOracle(t *testing.T, s *SpecSchedule, loc *time.Location, start, end time.Time) []time.Time {
	t.Helper()
	o := &gapWalk{s: s, loc: loc}
	_, o.lastOff = start.Add(-time.Minute).In(loc).Zone()
	day := time.Date(start.In(loc).Year(), start.In(loc).Month(), start.In(loc).Day(), 0, 0, 0, 0, time.UTC)
	for ; !day.After(end.In(loc)); day = day.AddDate(0, 0, 1) {
		for w := day; w.Before(day.AddDate(0, 0, 1)); w = w.Add(time.Minute) {
			o.visit(w)
		}
	}
	slices.SortFunc(o.runs, func(a, b time.Time) int { return a.Compare(b) })
	return slices.CompactFunc(o.runs, time.Time.Equal)
}

// gapWalk is gapOracle's state while it walks the wall clock minute by minute.
type gapWalk struct {
	s                *SpecSchedule
	loc              *time.Location
	lastOff          int  // offset of the last wall time that exists
	inGap, gapHasRun bool // inside a gap; the gap already has its run
	runs             []time.Time
}

// visit handles wall time w, coded in UTC.
func (o *gapWalk) visit(w time.Time) {
	s := o.s
	i := time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), 0, 0, o.loc)
	matches := yearMatches(w.Year(), s.Year) && fieldMatches(int(w.Month()), s.Month) &&
		dayMatches(s, w) && fieldMatches(w.Hour(), s.Hour) && fieldMatches(w.Minute(), s.Minute) &&
		fieldMatches(0, s.Second)
	if i.Hour() != w.Hour() || i.Minute() != w.Minute() || i.Day() != w.Day() {
		o.skipped(w, matches)
		return
	}
	o.inGap = false
	_, o.lastOff = i.Zone()
	if matches {
		o.runs = append(o.runs, i)
		o.runs = append(o.runs, otherCopies(i, w)...)
	}
}

// skipped handles wall time w that a spring-forward gap skips: the first
// matching one of each gap runs, read in the offset before the gap.
func (o *gapWalk) skipped(w time.Time, matches bool) {
	if !o.inGap {
		o.inGap, o.gapHasRun = true, false
	}
	if matches && !o.gapHasRun && !o.s.DSTSkip {
		o.gapHasRun = true
		o.runs = append(o.runs, time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), 0, 0, time.FixedZone("", o.lastOff)))
	}
}

// otherCopies returns the other instants that show wall time w, as i does: in
// a fall-back overlap the wall time exists twice, and Next reports both.
func otherCopies(i, w time.Time) []time.Time {
	var copies []time.Time
	for _, d := range []time.Duration{-2 * time.Hour, -time.Hour, -30 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour} {
		if j := i.Add(d); j.Hour() == w.Hour() && j.Minute() == w.Minute() && j.Day() == w.Day() {
			copies = append(copies, j)
		}
	}
	return copies
}

// TestDSTGapSweep checks Next and Prev from every minute around each gap, and
// from the seconds around it, against gapOracle.
func TestDSTGapSweep(t *testing.T) {
	zones := []struct {
		name       string
		start, end time.Time // wall clock, UTC-coded
		specs      []string
	}{
		{
			"Australia/Lord_Howe", time.Date(2026, 10, 3, 22, 0, 0, 0, time.UTC), time.Date(2026, 10, 5, 2, 0, 0, 0, time.UTC),
			[]string{
				"15 2 * * *", "5,15 2 * * *", "15,40 2 * * *", "0,30 2 * * *", "*/10 2 * * *", "0 * * * *", "45 1,2 * * *", "* 2 * * *",
				"0 9 * * *", "15 3 * * *", "45 1 * * *", "30 23 * * *", "0 0 * * *", "10 5 * * *",
			},
		},
		{
			// Fall-back: 2026-10-25 03:00 CEST -> 02:00 CET repeats 02:00-03:00.
			"Europe/Berlin", time.Date(2026, 10, 24, 22, 0, 0, 0, time.UTC), time.Date(2026, 10, 26, 2, 0, 0, 0, time.UTC),
			[]string{"30 2 * * *", "0 2 * * *", "0 */2 * * *", "15 1,2,3 * * *", "0 9 * * *"},
		},
		{
			// Fall-back: 2026-11-01 02:00 EDT -> 01:00 EST.
			"America/New_York", time.Date(2026, 10, 31, 22, 0, 0, 0, time.UTC), time.Date(2026, 11, 2, 2, 0, 0, 0, time.UTC),
			[]string{"30 1 * * *", "0,30 1 * * *", "0 */2 * * *", "0 * * * *", "0 9 * * *", "59 0 * * *"},
		},
		{
			// Fall-back: 2026-10-25 02:00 BST -> 01:00 GMT.
			"Europe/London", time.Date(2026, 10, 24, 22, 0, 0, 0, time.UTC), time.Date(2026, 10, 26, 2, 0, 0, 0, time.UTC),
			[]string{"30 1 * * *", "0 1 * * *", "0 */3 * * *", "0 9 * * *"},
		},
		{
			// Fall-back: 2026-04-05 03:00 AEDT -> 02:00 AEST.
			"Australia/Sydney", time.Date(2026, 4, 4, 22, 0, 0, 0, time.UTC), time.Date(2026, 4, 6, 2, 0, 0, 0, time.UTC),
			[]string{"0 2 * * *", "30 2 * * *", "15 1-3 * * *", "0 9 * * *"},
		},
		{
			// Fall-back: 2026-04-05 03:45 +1345 -> 02:45 +1245 repeats 02:45-03:45.
			"Pacific/Chatham", time.Date(2026, 4, 4, 22, 0, 0, 0, time.UTC), time.Date(2026, 4, 6, 2, 0, 0, 0, time.UTC),
			[]string{"30 2 * * *", "50 2 * * *", "0 3 * * *", "30 3 * * *", "15 * * * *", "0 9 * * *"},
		},
		{
			// Fall-back by two hours: 2026-10-25 03:00 +02 -> 01:00 +00.
			"Antarctica/Troll", time.Date(2026, 10, 24, 22, 0, 0, 0, time.UTC), time.Date(2026, 10, 26, 2, 0, 0, 0, time.UTC),
			[]string{"30 2 * * *", "15 1 * * *", "0 */2 * * *", "*/20 * * * *", "0 9 * * *"},
		},
		{
			// Spring-forward: 2026-09-27 02:45 +1245 -> 03:45 +1345.
			"Pacific/Chatham", time.Date(2026, 9, 26, 22, 0, 0, 0, time.UTC), time.Date(2026, 9, 28, 2, 0, 0, 0, time.UTC),
			[]string{"30 2 * * *", "50 2 * * *", "0 3 * * *", "45 3 * * *", "0 9 * * *"},
		},
		{
			// Fall-back: 2026-04-05 02:00 +11 -> 01:30 +1030 repeats 01:30-02:00.
			"Australia/Lord_Howe", time.Date(2026, 4, 4, 22, 0, 0, 0, time.UTC), time.Date(2026, 4, 6, 2, 0, 0, 0, time.UTC),
			[]string{"0 9 * * *", "45 1 * * *", "15 1 * * *", "0 2 * * *", "*/20 * * * *", "0 0 * * *"},
		},
		{
			"America/Santiago", time.Date(2026, 9, 5, 20, 0, 0, 0, time.UTC), time.Date(2026, 9, 6, 4, 0, 0, 0, time.UTC),
			[]string{"30 0 * * *", "0 0 * * *", "*/20 0,1 * * *", "30 0 6 9 *", "0 1 5 9 *", "0 1 6 9 *", "59 23 * * *", "0 * * * *"},
		},
		{
			"America/Los_Angeles", time.Date(2026, 3, 7, 22, 0, 0, 0, time.UTC), time.Date(2026, 3, 8, 6, 0, 0, 0, time.UTC),
			[]string{"30 2 * * *", "*/15 2 * * *", "59 1 * * *", "0 3 * * *", "30 1-3 * * *", "0 * * * *"},
		},
		{
			"Pacific/Apia", time.Date(2011, 12, 29, 20, 0, 0, 0, time.UTC), time.Date(2011, 12, 31, 4, 0, 0, 0, time.UTC),
			[]string{"0 12 * * *", "0 2 30 12 *", "0 0 * * *", "30 23 * * *"},
		},
	}
	for _, z := range zones {
		loc := loadLocation(t, z.name)
		start := time.Date(z.start.Year(), z.start.Month(), z.start.Day(), z.start.Hour(), 0, 0, 0, loc)
		end := time.Date(z.end.Year(), z.end.Month(), z.end.Day(), z.end.Hour(), 0, 0, 0, loc)
		for _, spec := range z.specs {
			t.Run(z.name+"/"+spec, func(t *testing.T) {
				sweepGapSpec(t, loc, spec, start, end)
			})
		}
	}
}

// sweepGapSpec checks Next and Prev of spec in loc from every minute between
// start and end, and from the seconds around it, against gapOracle. It stops
// after five disagreements.
func sweepGapSpec(t *testing.T, loc *time.Location, spec string, start, end time.Time) {
	t.Helper()
	sched, err := ParseStandard("TZ=" + loc.String() + " " + spec)
	if err != nil {
		t.Fatal(err)
	}
	s := sched.(*SpecSchedule)
	// Pad the oracle by a day on both sides for the runs around the window.
	oracle := gapOracle(t, s, loc, start.AddDate(0, 0, -1), end.AddDate(0, 0, 1))
	failures := 0
	for m := start; m.Before(end) && failures < 5; m = m.Add(time.Minute) {
		for _, from := range []time.Time{m.Add(-time.Second), m, m.Add(time.Second), m.Add(30 * time.Second)} {
			failures += checkAgainstOracle(t, s, loc, oracle, from)
		}
	}
}

// checkAgainstOracle compares Next and Prev from from with the sorted oracle
// runs and returns how many of the two disagree.
func checkAgainstOracle(t *testing.T, s *SpecSchedule, loc *time.Location, oracle []time.Time, from time.Time) int {
	t.Helper()
	failures := 0
	i, found := slices.BinarySearchFunc(oracle, from, func(a, b time.Time) int { return a.Compare(b) })
	j := i
	if found {
		j++
	}
	if got := s.Next(from); j < len(oracle) && !got.Equal(oracle[j]) {
		t.Errorf("Next(%v) = %v, want %v", from.In(loc), got.In(loc), oracle[j].In(loc))
		failures++
	}
	if got := s.Prev(from); i > 0 && !got.Equal(oracle[i-1]) {
		t.Errorf("Prev(%v) = %v, want %v", from.In(loc), got.In(loc), oracle[i-1].In(loc))
		failures++
	}
	return failures
}

// TestNextPrevSecondsOffset checks a zone whose UTC offset has seconds, where
// the minute step truncates to an absolute minute: when a time field wraps,
// the search must keep the state of the previous pass, or every pass moves
// the wall clock back and the search runs out (zero time).
func TestNextPrevSecondsOffset(t *testing.T) {
	loc := time.FixedZone("X", 30)
	p := NewParser(Second | Minute | Hour | Dom | Month | Dow)
	from := time.Date(2026, 10, 30, 12, 29, 15, 0, loc)
	for _, spec := range []string{"0 */13 * * * *", "45 59 * * * *", "0 15,45 * * * *"} {
		t.Run(spec, func(t *testing.T) {
			sched, err := p.Parse(spec)
			if err != nil {
				t.Fatal(err)
			}
			s := sched.(*SpecSchedule)
			s.Location = loc
			for name, got := range map[string]time.Time{"Next": s.Next(from), "Prev": s.Prev(from)} {
				w := got.In(loc)
				if got.IsZero() || !fieldMatches(w.Hour(), s.Hour) || !fieldMatches(w.Minute(), s.Minute) || !fieldMatches(w.Second(), s.Second) {
					t.Errorf("%s(%v) = %v, want a time the spec matches", name, from, got.In(loc))
				}
			}
		})
	}
}

// TestDSTGapRun_SearchLimit checks that a gap run counts only within the
// years that Next and Prev search, and that it keeps the caller's location.
func TestDSTGapRun_SearchLimit(t *testing.T) {
	berlin := loadLocation(t, "Europe/Berlin") // 2032-03-28 02:00 -> 03:00
	p := NewParser(Minute | Hour | Dom | Month | Dow | Year)
	far, err := p.Parse("TZ=Europe/Berlin 30 2 28 3 * 2032")
	if err != nil {
		t.Fatal(err)
	}
	if got := far.Next(time.Date(2026, 6, 1, 0, 0, 0, 0, berlin)); !got.IsZero() {
		t.Errorf("Next beyond the search limit = %v, want zero time", got)
	}
	if got := far.(ScheduleWithPrev).Prev(time.Date(2038, 6, 1, 0, 0, 0, 0, berlin)); !got.IsZero() {
		t.Errorf("Prev beyond the search limit = %v, want zero time", got)
	}

	only, err := p.Parse("TZ=Europe/Berlin 30 2 29 3 * 2026") // 2026-03-29 is a gap day
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, berlin)
	got := only.Next(from)
	want := time.Date(2026, 3, 29, 3, 30, 0, 0, berlin)
	if !got.Equal(want) || got.Location() != berlin {
		t.Errorf("Next(%v) = %v in %v, want %v in %v", from, got, got.Location(), want, berlin)
	}
}

// TestNextDSTFallBackFirstCopy pins that Next returns the first copy of a
// repeated wall time also where time.Date picks the second copy (zones east
// of UTC); the scheduler then skips the second copy (ADR-016).
func TestNextDSTFallBackFirstCopy(t *testing.T) {
	tests := []struct {
		zone, spec string
		from       time.Time // wall clock, UTC-coded
		first      time.Time // UTC
		second     time.Time // UTC
	}{
		{"Europe/Berlin", "30 2 * * *", time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC), time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC), time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC)},
		{"Europe/London", "30 1 * * *", time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC), time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC), time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC)},
		{"Australia/Sydney", "0 2 * * *", time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC), time.Date(2026, 4, 4, 15, 0, 0, 0, time.UTC), time.Date(2026, 4, 4, 16, 0, 0, 0, time.UTC)},
		{"America/New_York", "30 1 * * *", time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC), time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC)},
	}
	for _, tc := range tests {
		t.Run(tc.zone, func(t *testing.T) {
			loc := loadLocation(t, tc.zone)
			sched, err := ParseStandard("TZ=" + tc.zone + " " + tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			from := time.Date(tc.from.Year(), tc.from.Month(), tc.from.Day(), tc.from.Hour(), 0, 0, 0, loc)
			first := sched.Next(from)
			if !first.Equal(tc.first) {
				t.Fatalf("Next(%v) = %v, want %v", from, first.In(loc), tc.first.In(loc))
			}
			if second := sched.Next(first); !second.Equal(tc.second) {
				t.Errorf("Next(%v) = %v, want %v", first.In(loc), second.In(loc), tc.second.In(loc))
			}
		})
	}
}

// TestNextAcrossYearEnds walks Next and Prev over the year ends up to 2096.
// For years past its stored transitions, ZoneBounds reports a bound at the
// end of the year whose end can lie before its argument (2040-12-31 00:00
// UTC with system tzdata, 2028-12-31 with Go's embedded copy); a transition
// walk that trusts it never advances.
func TestNextAcrossYearEnds(t *testing.T) {
	berlin := loadLocation(t, "Europe/Berlin")
	sched, err := ParseStandard("TZ=Europe/Berlin 0 9 * * *")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan string, 1)
	go func() {
		for y := 2026; y <= 2096; y++ {
			from := time.Date(y, 12, 30, 12, 0, 0, 0, berlin)
			want := time.Date(y, 12, 31, 9, 0, 0, 0, berlin)
			if got := sched.Next(from); !got.Equal(want) {
				done <- fmt.Sprintf("Next(%v) = %v, want %v", from, got, want)
				return
			}
			if got := sched.(ScheduleWithPrev).Prev(want.Add(time.Hour)); !got.Equal(want) {
				done <- fmt.Sprintf("Prev(%v) = %v, want %v", want.Add(time.Hour), got, want)
				return
			}
		}
		done <- ""
	}()
	select {
	case msg := <-done:
		if msg != "" {
			t.Error(msg)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Next or Prev did not return within 10s")
	}
}

// TestNextChathamMinuteHourChange covers a minute step that changes the hour
// while the minute goes up: Pacific/Chatham moves from 03:44 to 02:45 and
// from 02:44 to 03:45.
func TestNextChathamMinuteHourChange(t *testing.T) {
	chatham := loadLocation(t, "Pacific/Chatham")
	at := func(y int, mo time.Month, d, h, mi, off int) time.Time {
		return time.Date(y, mo, d, h, mi, 0, 0, time.FixedZone("", off))
	}
	const std, dst = 12*3600 + 45*60, 13*3600 + 45*60
	tests := []struct {
		spec    string
		options ParseOption
		from    time.Time
		want    time.Time
	}{
		{"50 3 * * *", 0, at(2026, 4, 5, 3, 40, dst), at(2026, 4, 5, 3, 50, std)},
		{"45-59 3 * * *", 0, at(2026, 4, 5, 3, 40, dst), at(2026, 4, 5, 3, 45, std)},
		{"50 3 * * *", DSTSkip, at(2026, 4, 5, 3, 40, dst), at(2026, 4, 5, 3, 50, std)},
		{"50 2 * * *", DSTSkip, at(2026, 9, 27, 2, 40, std), at(2026, 9, 28, 2, 50, dst)},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%s/DSTSkip=%t", tc.spec, tc.options&DSTSkip != 0), func(t *testing.T) {
			sched, err := NewParser(Minute | Hour | Dom | Month | Dow | tc.options).Parse("TZ=Pacific/Chatham " + tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			if got := sched.Next(tc.from); !got.Equal(tc.want) {
				t.Errorf("Next(%v) = %v, want %v", tc.from.In(chatham), got.In(chatham), tc.want.In(chatham))
			}
		})
	}
}

// TestPrevMonthStartInRepeatedHour covers a month whose midnight lies in a
// repeated fall-back hour, where time.Date picks the second copy of 00:00 and
// one second earlier is still in the same month: Asia/Gaza and Asia/Hebron
// fell back from 01:00 to 00:00 on 2004-10-01, Europe/Lisbon on 1916-11-01
// (#450). Prev must step back into the previous month.
func TestPrevMonthStartInRepeatedHour(t *testing.T) {
	tests := []struct {
		name, zone, spec string
		options          ParseOption
		maxYears         int
		from             [6]int // year, month, day, hour, minute, second in zone
		want             [6]int // zero value: no run
	}{
		{"Gaza/no run", "Asia/Gaza", "0 0 31 2 *", Minute | Hour | Dom | Month | Dow, 25, [6]int{2028, 10, 22, 1, 6, 22}, [6]int{}},
		{"Gaza/last day before", "Asia/Gaza", "59 23 30 9 *", Minute | Hour | Dom | Month | Dow, 0, [6]int{2004, 10, 15, 0, 0, 0}, [6]int{2004, 9, 30, 23, 59, 0}},
		{"Hebron/last day before", "Asia/Hebron", "59 23 30 9 *", Minute | Hour | Dom | Month | Dow, 0, [6]int{2004, 10, 15, 0, 0, 0}, [6]int{2004, 9, 30, 23, 59, 0}},
		{"Lisbon/no run", "Europe/Lisbon", "* */13 15-22 31-31 9 6 *", Second | Minute | Hour | Dom | Month | Dow | Year, 2, [6]int{1917, 3, 1, 2, 9, 13}, [6]int{}},
		{"Lisbon/last day before", "Europe/Lisbon", "0 23 31 10 *", Minute | Hour | Dom | Month | Dow, 0, [6]int{1916, 11, 15, 0, 0, 0}, [6]int{1916, 10, 31, 23, 0, 0}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			loc := loadLocation(t, tc.zone)
			sched, err := NewParser(tc.options).WithMaxSearchYears(tc.maxYears).Parse("TZ=" + tc.zone + " " + tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			at := func(v [6]int) time.Time {
				return time.Date(v[0], time.Month(v[1]), v[2], v[3], v[4], v[5], 0, loc)
			}
			from := at(tc.from)
			want := time.Time{}
			if tc.want != [6]int{} {
				want = at(tc.want)
			}
			done := make(chan time.Time, 1)
			go func() { done <- sched.(ScheduleWithPrev).Prev(from) }()
			select {
			case got := <-done:
				if !got.Equal(want) {
					t.Errorf("Prev(%v) = %v, want %v", from, got, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("Prev(%v) did not return within 5s", from)
			}
		})
	}
}

// TestPrevAcrossDateGoingBack covers Prev around Antarctica/Casey on
// 2010-03-05, where the clock went back from 02:00 to 23:00 of the day
// before, with a day field that does not match the 03-05 run being looked
// for. Prev must return, and it must return a time before its input.
func TestPrevAcrossDateGoingBack(t *testing.T) {
	loc := loadLocation(t, "Antarctica/Casey")
	tests := []struct {
		spec       string
		from, want time.Time
	}{
		{"0 0 0 1 * *", time.Date(2010, 3, 5, 10, 0, 0, 0, loc), time.Date(2010, 3, 1, 0, 0, 0, 0, loc)},
		{"0 0 0 1 * *", time.Date(2010, 3, 4, 23, 30, 0, 0, loc), time.Date(2010, 3, 1, 0, 0, 0, 0, loc)},
		{"0 0 0 5 * *", time.Date(2010, 3, 4, 10, 0, 0, 0, loc), time.Date(2010, 2, 5, 0, 0, 0, 0, loc)},
	}
	for _, tc := range tests {
		t.Run(tc.spec+"/"+tc.from.Format(time.RFC3339), func(t *testing.T) {
			sched, err := NewParser(Second | Minute | Hour | Dom | Month | Dow).Parse("TZ=Antarctica/Casey " + tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan time.Time, 1)
			go func() { done <- sched.(ScheduleWithPrev).Prev(tc.from) }()
			select {
			case got := <-done:
				if !got.Equal(tc.want) {
					t.Errorf("Prev(%v) = %v, want %v", tc.from, got, tc.want)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("Prev(%v) did not return within 5s", tc.from)
			}
		})
	}
}
