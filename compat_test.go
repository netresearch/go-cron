// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cron

import (
	"testing"
	"time"
)

// TestRobfigCompat checks that RobfigCompat produces the same results as
// robfig/cron/v3 ParseStandard for specs where the default parser differs.
// The expected times were generated with github.com/robfig/cron/v3 v3.0.1.
func TestRobfigCompat(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	compat := NewParser(Minute | Hour | Dom | Month | Dow | Descriptor | RobfigCompat)

	tests := []struct {
		name string
		spec string
		from time.Time
		want time.Time
	}{
		{"step equal to minute range", "*/60 * * * *", time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC), time.Date(2026, 10, 22, 13, 0, 0, 0, time.UTC)},
		{"step equal to partial range", "30/30 * * * *", time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC), time.Date(2026, 10, 22, 12, 30, 0, 0, time.UTC)},
		{"step larger than explicit range", "0-5/6 * * * *", time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC), time.Date(2026, 10, 22, 13, 0, 0, 0, time.UTC)},
		{"step equal to dom range", "0 0 */31 * *", time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
		{"step equal to month range", "0 0 * */12 *", time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"dom or dow", "0 0 1 * MON", time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC), time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)},
		{"dom or dow range", "0 0 1,15 * 1-5", time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC), time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)},
		{"every below minimum rounds up", "@every 500ms", time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC), time.Date(2026, 10, 22, 12, 0, 1, 0, time.UTC)},
		{"every zero rounds up", "@every 0s", time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC), time.Date(2026, 10, 22, 12, 0, 1, 0, time.UTC)},
		{"spring forward gap is skipped", "CRON_TZ=America/New_York 30 2 * * *", time.Date(2026, 3, 7, 12, 0, 0, 0, ny), time.Date(2026, 3, 9, 2, 30, 0, 0, ny)},
		{"spring forward outside gap", "CRON_TZ=America/New_York 30 1 * * *", time.Date(2026, 3, 7, 12, 0, 0, 0, ny), time.Date(2026, 3, 8, 1, 30, 0, 0, ny)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sched, err := compat.Parse(tc.spec)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tc.spec, err)
			}
			if got := sched.Next(tc.from); !got.Equal(tc.want) {
				t.Errorf("Parse(%q).Next(%v) = %v, want %v", tc.spec, tc.from, got, tc.want)
			}
		})
	}
}

// TestRobfigCompatDefaultsUnchanged checks that the default parser keeps
// rejecting the specs that RobfigCompat accepts.
func TestRobfigCompatDefaultsUnchanged(t *testing.T) {
	for _, spec := range []string{"*/60 * * * *", "0-5/6 * * * *", "@every 500ms"} {
		if _, err := ParseStandard(spec); err == nil {
			t.Errorf("ParseStandard(%q) succeeded, want error", spec)
		}
	}

	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	sched, err := ParseStandard("CRON_TZ=America/New_York 30 2 * * *")
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 3, 7, 12, 0, 0, 0, ny)
	want := time.Date(2026, 3, 8, 3, 30, 0, 0, ny)
	if got := sched.Next(from); !got.Equal(want) {
		t.Errorf("Next(%v) = %v, want %v", from, got, want)
	}
}

// TestRobfigCompatOptionsIndependent checks that each option works on its own.
func TestRobfigCompatOptionsIndependent(t *testing.T) {
	base := Minute | Hour | Dom | Month | Dow | Descriptor

	if _, err := NewParser(base | LargeStep).Parse("*/60 * * * *"); err != nil {
		t.Errorf("LargeStep: unexpected error: %v", err)
	}
	if _, err := NewParser(base | LargeStep).Parse("@every 500ms"); err == nil {
		t.Error("LargeStep: @every 500ms succeeded, want error")
	}
	if _, err := NewParser(base | EveryRoundUp).Parse("@every 500ms"); err != nil {
		t.Errorf("EveryRoundUp: unexpected error: %v", err)
	}
	if _, err := NewParser(base | EveryRoundUp).Parse("*/60 * * * *"); err == nil {
		t.Error("EveryRoundUp: */60 succeeded, want error")
	}

	sched, err := NewParser(base | DSTSkip).Parse("@daily")
	if err != nil {
		t.Fatal(err)
	}
	if spec, ok := sched.(*SpecSchedule); !ok || !spec.DSTSkip {
		t.Errorf("DSTSkip: descriptor schedule = %#v, want DSTSkip set", sched)
	}
}
