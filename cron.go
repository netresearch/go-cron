package cron

import (
	"container/heap"
	"context"
	"sort"
	"sync"
	"time"
)

// maxIdleDuration is the sleep duration when no entries are scheduled.
// Using a very long duration (~11.4 years) instead of blocking indefinitely
// allows the scheduler loop to still respond to add, remove, and stop operations.
// This is a practical "infinity" that avoids timer overflow concerns.
const maxIdleDuration = 100000 * time.Hour

// Cron keeps track of any number of entries, invoking the associated func as
// specified by the schedule. It may be started, stopped, and the entries may
// be inspected while running.
//
// Entries are stored in a min-heap ordered by next execution time, providing
// O(log n) insertion/removal and O(1) access to the next entry to run.
type Cron struct {
	entries   entryHeap
	chain     Chain
	stop      chan struct{}
	add       chan *Entry
	remove    chan EntryID
	snapshot  chan chan []Entry
	running   bool
	logger    Logger
	runningMu sync.Mutex
	location  *time.Location
	parser    ScheduleParser
	nextID    EntryID
	jobWaiter sync.WaitGroup
	clock     Clock
}

// ScheduleParser is an interface for schedule spec parsers that return a Schedule
type ScheduleParser interface {
	Parse(spec string) (Schedule, error)
}

// Job is an interface for submitted cron jobs.
type Job interface {
	Run()
}

// Schedule describes a job's duty cycle.
type Schedule interface {
	// Next returns the next activation time, later than the given time.
	// Next is invoked initially, and then each time the job is run.
	Next(time.Time) time.Time
}

// EntryID identifies an entry within a Cron instance.
// Using uint64 prevents overflow and ID collisions on all platforms.
type EntryID uint64

// Entry consists of a schedule and the func to execute on that schedule.
type Entry struct {
	// ID is the cron-assigned ID of this entry, which may be used to look up a
	// snapshot or remove it.
	ID EntryID

	// Schedule on which this job should be run.
	Schedule Schedule

	// Next time the job will run, or the zero time if Cron has not been
	// started or this entry's schedule is unsatisfiable
	Next time.Time

	// Prev is the last time this job was run, or the zero time if never.
	Prev time.Time

	// WrappedJob is the thing to run when the Schedule is activated.
	WrappedJob Job

	// Job is the thing that was submitted to cron.
	// It is kept around so that user code that needs to get at the job later,
	// e.g. via Entries() can do so.
	Job Job

	// heapIndex is the entry's index in the scheduler's min-heap.
	// It is maintained by the heap implementation and used for efficient updates.
	heapIndex int
}

// Valid returns true if this is not the zero entry.
func (e Entry) Valid() bool { return e.ID != 0 }

// Run executes the entry's job through the configured chain wrappers.
// This ensures that chain decorators like SkipIfStillRunning, DelayIfStillRunning,
// and Recover are properly applied. Use this method instead of Entry.Job.Run()
// when you need chain behavior to be respected.
// Fix for issue #551: Provides a proper way to run jobs with chain decorators.
func (e Entry) Run() {
	if e.WrappedJob != nil {
		e.WrappedJob.Run()
	}
}

// New returns a new Cron job runner, modified by the given options.
//
// Available Settings
//
//	Time Zone
//	  Description: The time zone in which schedules are interpreted
//	  Default:     time.Local
//
//	Parser
//	  Description: Parser converts cron spec strings into cron.Schedules.
//	  Default:     Accepts this spec: https://en.wikipedia.org/wiki/Cron
//
//	Chain
//	  Description: Wrap submitted jobs to customize behavior.
//	  Default:     A chain that recovers panics and logs them to stderr.
//
// See "cron.With*" to modify the default behavior.
func New(opts ...Option) *Cron {
	c := &Cron{
		entries:   nil,
		chain:     NewChain(),
		add:       make(chan *Entry),
		stop:      make(chan struct{}),
		snapshot:  make(chan chan []Entry),
		remove:    make(chan EntryID),
		running:   false,
		runningMu: sync.Mutex{},
		logger:    DefaultLogger,
		location:  time.Local,
		parser:    standardParser,
		clock:     RealClock{},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// FuncJob is a wrapper that turns a func() into a cron.Job
type FuncJob func()

// Run calls the wrapped function.
func (f FuncJob) Run() { f() }

// AddFunc adds a func to the Cron to be run on the given schedule.
// The spec is parsed using the time zone of this Cron instance as the default.
// An opaque ID is returned that can be used to later remove it.
func (c *Cron) AddFunc(spec string, cmd func()) (EntryID, error) {
	return c.AddJob(spec, FuncJob(cmd))
}

// AddJob adds a Job to the Cron to be run on the given schedule.
// The spec is parsed using the time zone of this Cron instance as the default.
// An opaque ID is returned that can be used to later remove it.
func (c *Cron) AddJob(spec string, cmd Job) (EntryID, error) {
	schedule, err := c.parser.Parse(spec)
	if err != nil {
		return 0, err
	}
	return c.Schedule(schedule, cmd), nil
}

// Schedule adds a Job to the Cron to be run on the given schedule.
// The job is wrapped with the configured Chain.
func (c *Cron) Schedule(schedule Schedule, cmd Job) EntryID {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	c.nextID++
	if c.nextID == 0 {
		c.nextID = 1 // Skip 0; Entry.Valid() uses 0 as invalid sentinel
	}
	entry := &Entry{
		ID:         c.nextID,
		Schedule:   schedule,
		WrappedJob: c.chain.Then(cmd),
		Job:        cmd,
		heapIndex:  -1,
	}
	if !c.running {
		heap.Push(&c.entries, entry)
	} else {
		c.add <- entry
	}
	return entry.ID
}

// Entries returns a snapshot of the cron entries.
func (c *Cron) Entries() []Entry {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	if c.running {
		replyChan := make(chan []Entry, 1)
		c.snapshot <- replyChan
		return <-replyChan
	}
	return c.entrySnapshot()
}

// Location gets the time zone location
func (c *Cron) Location() *time.Location {
	return c.location
}

// Entry returns a snapshot of the given entry, or nil if it couldn't be found.
func (c *Cron) Entry(id EntryID) Entry {
	for _, entry := range c.Entries() {
		if id == entry.ID {
			return entry
		}
	}
	return Entry{}
}

// Remove an entry from being run in the future.
func (c *Cron) Remove(id EntryID) {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	if c.running {
		c.remove <- id
	} else {
		c.removeEntry(id)
	}
}

// Start the cron scheduler in its own goroutine, or no-op if already started.
func (c *Cron) Start() {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	if c.running {
		return
	}
	c.running = true
	go c.run()
}

// Run the cron scheduler, or no-op if already running.
func (c *Cron) Run() {
	c.runningMu.Lock()
	if c.running {
		c.runningMu.Unlock()
		return
	}
	c.running = true
	c.runningMu.Unlock()
	c.run()
}

// run the scheduler.. this is private just due to the need to synchronize
// access to the 'running' state variable.

// handleTimeBackwards reschedules entries when system time moves backwards.
// This can happen due to NTP correction or VM snapshot restore.
func (c *Cron) handleTimeBackwards(now time.Time) {
	// Iterate over a copy since Update() reorders the heap.
	entriesCopy := make([]*Entry, len(c.entries))
	copy(entriesCopy, c.entries)
	for _, e := range entriesCopy {
		if !e.Prev.IsZero() && e.Prev.After(now) {
			e.Next = e.Schedule.Next(now)
			c.entries.Update(e)
			c.logger.Info("reschedule", "reason", "time moved backwards",
				"entry", e.ID, "prev", e.Prev, "now", now, "next", e.Next)
		}
	}
}

func (c *Cron) run() {
	c.logger.Info("start")

	// Figure out the next activation times for each entry and initialize heap.
	now := c.now()
	for _, entry := range c.entries {
		entry.Next = entry.Schedule.Next(now)
		c.logger.Info("schedule", "now", now, "entry", entry.ID, "next", entry.Next)
	}
	heap.Init(&c.entries)

	for {
		// Determine the next entry to run using the heap (O(1) peek).
		var timer Timer
		next := c.entries.Peek()
		if next == nil || next.Next.IsZero() {
			// If there are no entries yet, just sleep - it still handles new entries
			// and stop requests.
			timer = c.clock.NewTimer(maxIdleDuration)
		} else {
			timer = c.clock.NewTimer(next.Next.Sub(now))
		}

		for {
			select {
			case now = <-timer.C():
				now = now.In(c.location)
				c.logger.Info("wake", "now", now)

				// Handle system time moving backwards (NTP correction, VM snapshot restore).
				c.handleTimeBackwards(now)

				// Run every entry whose next time was less than now.
				// Keep popping from heap while entries are due.
				for c.entries.Peek() != nil {
					e := c.entries.Peek()
					if e.Next.After(now) || e.Next.IsZero() {
						break
					}
					c.startJob(e.WrappedJob)
					e.Prev = e.Next
					e.Next = e.Schedule.Next(now)
					c.entries.Update(e) // Re-heapify after updating Next time
					c.logger.Info("run", "now", now, "entry", e.ID, "next", e.Next)
				}

			case newEntry := <-c.add:
				timer.Stop()
				now = c.now()
				newEntry.Next = newEntry.Schedule.Next(now)
				heap.Push(&c.entries, newEntry)
				c.logger.Info("added", "now", now, "entry", newEntry.ID, "next", newEntry.Next)

			case replyChan := <-c.snapshot:
				replyChan <- c.entrySnapshot()
				continue

			case <-c.stop:
				timer.Stop()
				c.logger.Info("stop")
				return

			case id := <-c.remove:
				timer.Stop()
				now = c.now()
				c.removeEntry(id)
				c.logger.Info("removed", "entry", id)
			}

			break
		}
	}
}

// startJob runs the given job in a new goroutine.
func (c *Cron) startJob(j Job) {
	c.jobWaiter.Add(1)
	go func() {
		defer c.jobWaiter.Done()
		j.Run()
	}()
}

// now returns current time in c location.
// Uses the configured clock (defaults to RealClock).
func (c *Cron) now() time.Time {
	return c.clock.Now().In(c.location)
}

// Stop stops the cron scheduler if it is running; otherwise it does nothing.
// A context is returned so the caller can wait for running jobs to complete.
func (c *Cron) Stop() context.Context {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	if c.running {
		c.stop <- struct{}{}
		c.running = false
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		c.jobWaiter.Wait()
		cancel()
	}()
	return ctx
}

// StopAndWait stops the cron scheduler and blocks until all running jobs complete.
// This is a convenience method equivalent to:
//
//	ctx := c.Stop()
//	<-ctx.Done()
//
// For timeout-based shutdown, use Stop() directly and select on the returned context.
func (c *Cron) StopAndWait() {
	<-c.Stop().Done()
}

// entrySnapshot returns a copy of the current cron entry list, sorted by next execution time.
func (c *Cron) entrySnapshot() []Entry {
	entries := make([]Entry, len(c.entries))
	for i, e := range c.entries {
		entries[i] = *e
	}
	// Sort the snapshot by next execution time (heap internal order is not sorted).
	sortEntriesByTime(entries)
	return entries
}

// sortEntriesByTime sorts entries by Next time, with zero times at the end.
func sortEntriesByTime(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		// Zero times sort to the end (highest priority = earliest time)
		if entries[i].Next.IsZero() {
			return false
		}
		if entries[j].Next.IsZero() {
			return true
		}
		return entries[i].Next.Before(entries[j].Next)
	})
}

func (c *Cron) removeEntry(id EntryID) {
	c.entries.Remove(id)
}
