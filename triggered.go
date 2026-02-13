package cron

import "time"

// TriggeredSchedule is a schedule that never fires automatically.
// Entries using this schedule remain dormant until explicitly triggered
// via TriggerEntry or TriggerEntryByName. This enables manual/on-demand
// job execution while still benefiting from the scheduler's middleware
// chain (retry, timeout, skip-if-running, etc.).
//
// Use the @triggered, @manual, or @none descriptors to create entries
// with this schedule:
//
//	c.AddFunc("@triggered", myJob, cron.WithName("deploy"))
//	c.TriggerEntryByName("deploy") // Run on demand
type TriggeredSchedule struct{}

// Next always returns the zero time, indicating this schedule never
// activates automatically. The scheduler's run loop treats zero-time
// entries as dormant.
func (TriggeredSchedule) Next(time.Time) time.Time { return time.Time{} }

// Prev always returns the zero time, as triggered schedules have no
// automatic activation history.
func (TriggeredSchedule) Prev(time.Time) time.Time { return time.Time{} }

// IsTriggered reports whether the given schedule is a TriggeredSchedule.
// This can be used to distinguish triggered entries from regularly scheduled ones.
//
//	if cron.IsTriggered(entry.Schedule) {
//	    fmt.Println("This entry only runs when triggered manually")
//	}
func IsTriggered(s Schedule) bool {
	_, ok := s.(TriggeredSchedule)
	return ok
}
