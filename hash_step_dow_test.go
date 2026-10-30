// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cron

import (
	"fmt"
	"math/bits"
	"slices"
	"strings"
	"testing"
)

// hashTestKeys returns n distinct hash keys of the form "job-<i>".
func hashTestKeys(n int) []string {
	keys := make([]string, n)
	for i := range keys {
		keys[i] = fmt.Sprintf("job-%d", i)
	}
	return keys
}

// TestHashStepOneMeansEveryValue pins that H/1 and H(a-b)/1 select every value
// of the field or range, as */1 and a-b do, instead of a single hashed value
// (#455). H/1 carries no star bit, like H/n and an explicit range.
func TestHashStepOneMeansEveryValue(t *testing.T) {
	p := NewParser(Second | Minute | Hour | Dom | Month | Dow | Hash)

	field := map[string]func(*SpecSchedule) uint64{
		"second": func(s *SpecSchedule) uint64 { return s.Second },
		"minute": func(s *SpecSchedule) uint64 { return s.Minute },
		"hour":   func(s *SpecSchedule) uint64 { return s.Hour },
		"dom":    func(s *SpecSchedule) uint64 { return s.Dom },
		"month":  func(s *SpecSchedule) uint64 { return s.Month },
		"dow":    func(s *SpecSchedule) uint64 { return s.Dow },
	}

	tests := []struct {
		field     string
		hashSpec  string
		plainSpec string
	}{
		{"second", "H/1 0 0 1 1 *", "0-59 0 0 1 1 *"},
		{"minute", "0 H/1 0 1 1 *", "0 0-59 0 1 1 *"},
		{"hour", "0 0 H/1 1 1 *", "0 0 0-23 1 1 *"},
		{"dom", "0 0 0 H/1 1 *", "0 0 0 1-31 1 *"},
		{"month", "0 0 0 1 H/1 *", "0 0 0 1 1-12 *"},
		{"dow", "0 0 0 * * H/1", "0 0 0 * * 0-6"},
		{"minute", "0 H(10-20)/1 0 1 1 *", "0 10-20 0 1 1 *"},
		{"minute", "0 H(55-5)/1 0 1 1 *", "0 55-5 0 1 1 *"},
		{"hour", "0 0 H(22-2)/1 1 1 *", "0 0 22-2 1 1 *"},
		{"dom", "0 0 0 H(28-3)/1 1 *", "0 0 0 28-3 1 *"},
		{"month", "0 0 0 1 H(11-2)/1 *", "0 0 0 1 11-2 *"},
		{"dow", "0 0 0 * * H(5-1)/1", "0 0 0 * * 5-1"},
		{"dow", "0 0 0 * * H(0-7)/1", "0 0 0 * * 0-6"},
	}

	for _, tt := range tests {
		t.Run(tt.hashSpec, func(t *testing.T) {
			plain, err := p.Parse(tt.plainSpec)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.plainSpec, err)
			}
			want := field[tt.field](plain.(*SpecSchedule)) &^ starBit

			for _, key := range []string{"job-a", "job-b", "worker-17"} {
				s, err := p.ParseWithHashKey(tt.hashSpec, key)
				if err != nil {
					t.Fatalf("ParseWithHashKey(%q, %q): %v", tt.hashSpec, key, err)
				}
				if got := field[tt.field](s.(*SpecSchedule)); got != want {
					t.Errorf("key %q: %s field = %b, want %b (as %q)", key, tt.field, got, want, tt.plainSpec)
				}
			}
		})
	}
}

// dowHashExprs are the day-of-week H forms checked for Sunday bias. Each entry
// lists the distinct days the expression can select.
var dowHashExprs = []struct {
	expr string
	days []uint // distinct days the expression draws from, Sunday = 0
}{
	{"H", []uint{0, 1, 2, 3, 4, 5, 6}},
	{"H/2", []uint{0, 1, 2, 3, 4, 5, 6}},
	{"H/3", []uint{0, 1, 2, 3, 4, 5, 6}},
	{"H(0-7)", []uint{0, 1, 2, 3, 4, 5, 6}},
	{"H(1-7)", []uint{1, 2, 3, 4, 5, 6, 0}},
	{"H(5-7)", []uint{5, 6, 0}},
	{"H(6-1)", []uint{6, 0, 1}},
	{"H(5-1)", []uint{5, 6, 0, 1}},
	{"H(5-1)/2", []uint{5, 6, 0, 1}},
}

// TestHashDowUniform pins that H in the day-of-week field chooses every day
// equally often (#455). The field accepts 0-7 with 0 and 7 both Sunday;
// hashing over eight values made Sunday about twice as likely.
//
// Every expression below selects each of its days in the same expected share
// of keys (1/d for a single value over d days, 1/s for a step s that splits
// the days evenly). With 70,000 keys the smallest count is about 7,800, where
// the binomial standard deviation is about 1% of the mean, so an unbiased hash
// keeps the commonest/rarest ratio far below the 1.3 bound; the Sunday bias
// gave ratios between 1.75 and 2. The keys are fixed, so the test is
// deterministic.
func TestHashDowUniform(t *testing.T) {
	p := NewParser(Minute | Hour | Dom | Month | Dow | Hash)
	keys := hashTestKeys(70000)

	for _, tt := range dowHashExprs {
		t.Run(tt.expr, func(t *testing.T) {
			counts := dowDayCounts(t, p, tt.expr, keys)
			lo, hi := len(keys), 0
			for _, d := range tt.days {
				lo = min(lo, counts[d])
				hi = max(hi, counts[d])
			}
			if lo == 0 || float64(hi)/float64(lo) > 1.3 {
				t.Errorf("biased day selection for %q: counts %v", tt.expr, counts)
			}
			for d, c := range counts {
				if !slices.Contains(tt.days, d) {
					t.Errorf("%q selected day %d outside its range %v (%d keys)", tt.expr, d, tt.days, c)
				}
			}
		})
	}
}

// dowDayCounts parses "0 0 * * <expr>" once per key and counts, per day bit
// of the parsed day-of-week field, how many keys select that day.
func dowDayCounts(t *testing.T, p Parser, expr string, keys []string) map[uint]int {
	t.Helper()
	counts := make(map[uint]int)
	for _, key := range keys {
		s, err := p.ParseWithHashKey("0 0 * * "+expr, key)
		if err != nil {
			t.Fatalf("ParseWithHashKey(%q, %q): %v", expr, key, err)
		}
		dowBits := s.(*SpecSchedule).Dow
		for d := uint(0); d <= 7; d++ {
			if dowBits&(1<<d) != 0 {
				counts[d]++
			}
		}
	}
	return counts
}

// TestHashDowNeverSetsBit7 pins the rule behind the fix for #455: an H
// expression in the day-of-week field never resolves to 7, so Sunday is
// always bit 0 and counted once. The check is on the raw bits from
// getRangeWithHash, before NormalizeDOW, which would hide a 7.
func TestHashDowNeverSetsBit7(t *testing.T) {
	exprs := []string{"H", "H/1", "H/2", "H/3", "H/7", "H/8", "H(0-7)", "H(1-7)", "H(5-7)", "H(6-1)", "H(5-1)/3", "H(7-7)"}
	for _, expr := range exprs {
		for _, key := range hashTestKeys(2000) {
			bits, err := getRangeWithHash(expr, dow, key, true)
			if err != nil {
				t.Fatalf("getRangeWithHash(%q, %q): %v", expr, key, err)
			}
			if bits&dowBit7 != 0 {
				t.Fatalf("getRangeWithHash(%q, %q) = %b: sets bit 7", expr, key, bits)
			}
		}
	}
}

// TestHashDowStepNeverEmpty pins that a step at or above the number of days
// still selects a day in the day-of-week field, for every key.
func TestHashDowStepNeverEmpty(t *testing.T) {
	exprs := []string{"H/6", "H/7", "H/8", "H/9", "H(5-1)/4", "H(5-1)/5", "H(5-1)/9"}
	for _, expr := range exprs {
		for _, key := range hashTestKeys(2000) {
			bits, err := getRangeWithHash(expr, dow, key, true)
			if err != nil {
				t.Fatalf("getRangeWithHash(%q, %q): %v", expr, key, err)
			}
			if bits == 0 {
				t.Fatalf("getRangeWithHash(%q, %q) selects no day", expr, key)
			}
		}
	}
}

// TestHashDowKeepsPreviousDay pins the compatibility rule of the Sunday-bias
// fix: a single-value H in the day-of-week field keeps the day it selected
// before (hash modulo the 0-7 range) for every key whose previous value was
// not 7. Only keys that resolved to 7, a second Sunday, are spread over the
// days of the range.
func TestHashDowKeepsPreviousDay(t *testing.T) {
	ranges := []struct {
		expr       string
		start, end uint
	}{
		{"H", 0, 7},
		{"H(0-7)", 0, 7},
		{"H(6-1)", 6, 1},
		{"H(5-1)", 5, 1},
		{"H(7-2)", 7, 2},
		{"H(5-7)", 5, 7},
	}
	for _, rr := range ranges {
		size := rr.end - rr.start + 1
		if rr.start > rr.end {
			size = (8 - rr.start) + rr.end + 1
		}
		for _, key := range hashTestKeys(5000) {
			previous := (rr.start + uint(computeHash(key)%uint64(size))) % 8
			if previous == 7 {
				continue
			}
			got, err := getRangeWithHash(rr.expr, dow, key, true)
			if err != nil {
				t.Fatalf("getRangeWithHash(%q, %q): %v", rr.expr, key, err)
			}
			if got != 1<<previous {
				t.Fatalf("getRangeWithHash(%q, %q) = %b, want previous day %d", rr.expr, key, got, previous)
			}
		}
	}
}

// TestHashStepAboveRangeSelectsOneValue pins that an H step at or above the
// size of its range selects exactly one value in every field. Before, a hash
// offset beyond the range left the field empty, and Next returned the zero
// time.
func TestHashStepAboveRangeSelectsOneValue(t *testing.T) {
	p := NewParser(Second | Minute | Hour | Dom | Month | Dow | Hash)
	specs := []string{
		"H/61 0 0 1 1 *",
		"0 H/61 0 1 1 *",
		"0 H(0-29)/40 0 1 1 *",
		"0 H(55-5)/20 0 1 1 *",
		"0 0 H/30 1 1 *",
		"0 0 0 H/40 1 *",
		"0 0 0 1 H/13 *",
		"0 0 0 * * H/9",
		"0 0 0 * * H(5-1)/9",
	}
	for _, spec := range specs {
		t.Run(spec, func(t *testing.T) {
			assertOneHashValue(t, p, spec)
		})
	}
}

// assertOneHashValue checks over 2,000 keys that spec sets exactly the field
// bits of one selected value per field (see oneValueBitCount).
func assertOneHashValue(t *testing.T, p Parser, spec string) {
	t.Helper()
	for _, key := range hashTestKeys(2000) {
		s, err := p.ParseWithHashKey(spec, key)
		if err != nil {
			t.Fatalf("ParseWithHashKey(%q, %q): %v", spec, key, err)
		}
		ss := s.(*SpecSchedule)
		n := 0
		for _, f := range []uint64{ss.Second, ss.Minute, ss.Hour, ss.Dom, ss.Month, ss.Dow} {
			n += bits.OnesCount64(f &^ starBit)
		}
		if want := oneValueBitCount(spec); n != want {
			t.Fatalf("ParseWithHashKey(%q, %q): %d bits set, want %d", spec, key, n, want)
		}
	}
}

// oneValueBitCount returns the number of field bits a spec from
// TestHashStepAboveRangeSelectsOneValue sets when its H field selects one
// value. Specs ending in "*" hold one value in each of the first five fields
// and the seven days of the wildcard dow. The dow specs hold one value in
// second, minute, hour and dow, plus 31 days and 12 months of wildcards.
func oneValueBitCount(spec string) int {
	if strings.HasSuffix(spec, " *") {
		return 5 + 7
	}
	return 4 + 31 + 12
}

// TestDowWraparoundStepCountsSundayOnce pins that a plain day-of-week
// wraparound range with a step walks the seven days once: 5-1 is Fri, Sat,
// Sun, Mon, so 5-1/2 is Fri and Sun. Before, the walk went through 7 and 0
// as two Sundays.
func TestDowWraparoundStepCountsSundayOnce(t *testing.T) {
	p := NewParser(Minute | Hour | Dom | Month | Dow)
	tests := []struct {
		spec string
		want uint64
	}{
		{"0 0 * * 5-1/2", 1<<5 | 1<<0},
		{"0 0 * * 6-1/2", 1<<6 | 1<<1},
		{"0 0 * * 4-2/3", 1<<4 | 1<<0},
		{"0 0 * * FRI-MON/2", 1<<5 | 1<<0},
		{"0 0 * * 7-2/2", 1<<0 | 1<<2},
	}
	for _, tt := range tests {
		s, err := p.Parse(tt.spec)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tt.spec, err)
		}
		if got := s.(*SpecSchedule).Dow &^ starBit; got != tt.want {
			t.Errorf("Parse(%q).Dow = %07b, want %07b", tt.spec, got, tt.want)
		}
	}
}

// TestHashStepAboveRangeKeepsPreviousValue pins the compatibility rule of the
// large-step fix: a key whose offset (hash modulo step) was inside the range
// keeps its value; only keys that selected nothing get a new one.
func TestHashStepAboveRangeKeepsPreviousValue(t *testing.T) {
	tests := []struct {
		expr       string
		r          bounds
		start      uint
		size, step uint
	}{
		{"H/61", minutes, 0, 60, 61},
		{"H(0-29)/40", minutes, 0, 30, 40},
		{"H/30", hours, 0, 24, 30},
		{"H/9", dow, 0, 7, 9},
	}
	for _, tt := range tests {
		for _, key := range hashTestKeys(5000) {
			offset := uint(computeHash(key) % uint64(tt.step))
			if offset >= tt.size {
				continue
			}
			got, err := getRangeWithHash(tt.expr, tt.r, key, true)
			if err != nil {
				t.Fatalf("getRangeWithHash(%q, %q): %v", tt.expr, key, err)
			}
			if want := uint64(1) << (tt.start + offset); got != want {
				t.Fatalf("getRangeWithHash(%q, %q) = %b, want previous value %d", tt.expr, key, got, tt.start+offset)
			}
		}
	}
}

// TestHashRangeOutsideBoundsIsError pins that an H range whose start lies
// above the field maximum, or whose end lies below the field minimum, is a
// parse error in every field, like the same range without H. Such a range
// passed the existing checks as a wraparound range, and its size underflowed.
func TestHashRangeOutsideBoundsIsError(t *testing.T) {
	const opts = Second | Minute | Hour | Dom | Month | Dow | Hash
	p := NewParser(opts).WithHashKey("job-a")
	specs := []string{
		"H(61-0) 0 0 1 1 *", "H(61-0)/2 0 0 1 1 *",
		"0 H(61-0) 0 1 1 *", "0 H(61-0)/2 0 1 1 *",
		"0 0 H(25-0) 1 1 *", "0 0 H(25-0)/2 1 1 *",
		"0 0 0 H(32-1) 1 *", "0 0 0 H(32-1)/2 1 *",
		"0 0 0 H(5-0) 1 *", "0 0 0 H(5-0)/2 1 *",
		"0 0 0 1 H(13-1) *", "0 0 0 1 H(13-1)/2 *",
		"0 0 0 1 H(5-0) *", "0 0 0 1 H(5-0)/2 *",
		"0 0 0 * * H(15-7)", "0 0 0 * * H(15-7)/2",
		"0 0 0 * * H(8-0)", "0 0 0 * * H(8-0)/2",
	}
	for _, spec := range specs {
		t.Run(spec, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic: %v", r)
				}
			}()
			if _, err := p.Parse(spec); err == nil {
				t.Errorf("Parse(%q): want an error", spec)
			}
			if err := ValidateSpecWith(spec, p); err == nil {
				t.Errorf("ValidateSpecWith(%q): want an error", spec)
			}
			if a := AnalyzeSpecWithHash(spec, opts, "job-a"); a.Valid {
				t.Errorf("AnalyzeSpecWithHash(%q): want invalid", spec)
			}
		})
	}
}
