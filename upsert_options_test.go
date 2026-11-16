package cron

import (
	"slices"
	"testing"
	"time"
)

// upsertFuncs runs a test once with UpsertJob and once with DrainAndUpsertJob,
// because both take the same options and update the same way.
var upsertFuncs = []struct {
	name   string
	upsert func(c *Cron, spec string, cmd Job, opts ...JobOption) (EntryID, error)
}{
	{"UpsertJob", (*Cron).UpsertJob},
	{"DrainAndUpsertJob", (*Cron).DrainAndUpsertJob},
}

// TestUpsertAppliesOptionsOnUpdate pins issue #456: an update applies the
// options that the call passes, the same way as a create does.
func TestUpsertAppliesOptionsOnUpdate(t *testing.T) {
	prev := time.Date(2025, 6, 1, 9, 0, 0, 0, time.UTC)
	for _, u := range upsertFuncs {
		t.Run(u.name, func(t *testing.T) {
			c := New()
			if _, err := u.upsert(c, "0 9 * * *", FuncJob(func() {}), WithName("report"), WithTags("a")); err != nil {
				t.Fatalf("create: %v", err)
			}
			if _, err := u.upsert(c, "0 10 * * *", FuncJob(func() {}), WithName("report"),
				WithTags("b"),
				WithPrev(prev),
				WithMissedPolicy(MissedRunOnce),
				WithMissedGracePeriod(time.Hour),
				WithPriority(7),
				WithRunOnce(),
			); err != nil {
				t.Fatalf("update: %v", err)
			}
			checkUpdatedOptions(t, c, "report", prev)
		})
	}
}

// checkUpdatedOptions checks the fields that TestUpsertAppliesOptionsOnUpdate
// sets in its update call.
func checkUpdatedOptions(t *testing.T, c *Cron, name string, prev time.Time) {
	t.Helper()
	e := c.EntryByName(name)
	if !slices.Equal(e.Tags, []string{"b"}) {
		t.Errorf("Tags = %v, want [b]", e.Tags)
	}
	if !e.Prev.Equal(prev) {
		t.Errorf("Prev = %v, want %v", e.Prev, prev)
	}
	if e.MissedPolicy != MissedRunOnce {
		t.Errorf("MissedPolicy = %v, want MissedRunOnce", e.MissedPolicy)
	}
	if e.MissedGracePeriod != time.Hour {
		t.Errorf("MissedGracePeriod = %v, want 1h", e.MissedGracePeriod)
	}
	if e.Priority != 7 {
		t.Errorf("Priority = %d, want 7", e.Priority)
	}
	if !c.entryIndex[e.ID].runOnce {
		t.Error("runOnce is not set after the update")
	}
}

// TestUpsertKeepsFieldsWithoutOption verifies that an update leaves a field
// unchanged when the call does not pass its option.
func TestUpsertKeepsFieldsWithoutOption(t *testing.T) {
	for _, u := range upsertFuncs {
		t.Run(u.name, func(t *testing.T) {
			c := New()
			if _, err := u.upsert(c, "0 9 * * *", FuncJob(func() {}), WithName("keep"),
				WithTags("a"), WithPriority(5), WithMissedPolicy(MissedRunAll)); err != nil {
				t.Fatalf("create: %v", err)
			}
			if _, err := u.upsert(c, "0 10 * * *", FuncJob(func() {}), WithName("keep")); err != nil {
				t.Fatalf("update: %v", err)
			}

			e := c.EntryByName("keep")
			if !slices.Equal(e.Tags, []string{"a"}) {
				t.Errorf("Tags = %v, want [a]", e.Tags)
			}
			if e.Priority != 5 {
				t.Errorf("Priority = %d, want 5", e.Priority)
			}
			if e.MissedPolicy != MissedRunAll {
				t.Errorf("MissedPolicy = %v, want MissedRunAll", e.MissedPolicy)
			}
		})
	}
}

// TestUpsertWithPausedOnUpdatePauses verifies that WithPaused on an update
// pauses the entry. DrainAndUpsertJob must not resume it afterwards.
func TestUpsertWithPausedOnUpdatePauses(t *testing.T) {
	for _, u := range upsertFuncs {
		t.Run(u.name, func(t *testing.T) {
			c := New()
			if _, err := u.upsert(c, "@every 1h", FuncJob(func() {}), WithName("p")); err != nil {
				t.Fatalf("create: %v", err)
			}
			if _, err := u.upsert(c, "@every 2h", FuncJob(func() {}), WithName("p"), WithPaused()); err != nil {
				t.Fatalf("update: %v", err)
			}
			if !c.IsEntryPausedByName("p") {
				t.Error("entry is not paused after an update with WithPaused")
			}
		})
	}
}

// TestUpsertWithRunImmediatelyOnUpdateRuns verifies that WithRunImmediately on
// an update of a running scheduler runs the new job at once, without a clock
// advance. For DrainAndUpsertJob this also pins that the resume happens in the
// same run-loop step as the swap: a resume after a separate step would let the
// run loop skip the immediate run while the entry is still paused.
func TestUpsertWithRunImmediatelyOnUpdateRuns(t *testing.T) {
	for _, u := range upsertFuncs {
		t.Run(u.name, func(t *testing.T) {
			fc := NewFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
			c := New(WithClock(fc))
			c.Start()
			defer c.Stop()

			if _, err := u.upsert(c, "@every 1h", FuncJob(func() {}), WithName("now")); err != nil {
				t.Fatalf("create: %v", err)
			}
			ran := make(chan struct{}, 1)
			job := FuncJob(func() {
				select {
				case ran <- struct{}{}:
				default:
				}
			})
			if _, err := u.upsert(c, "@every 1h", job, WithName("now"), WithRunImmediately()); err != nil {
				t.Fatalf("update: %v", err)
			}

			select {
			case <-ran:
			case <-time.After(2 * time.Second):
				t.Fatal("the job did not run immediately after the update")
			}
			if c.IsEntryPausedByName("now") {
				t.Error("entry is paused after the update")
			}
		})
	}
}
