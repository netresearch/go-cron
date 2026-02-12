package cron

import (
	"container/heap"
	"context"
	"errors"
	"maps"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// ErrMaxEntriesReached is returned when adding an entry would exceed the configured
// maximum number of entries (see WithMaxEntries).
var ErrMaxEntriesReached = errors.New("cron: max entries limit reached")

// ErrDuplicateName is returned when adding an entry with a name that already exists.
var ErrDuplicateName = errors.New("cron: duplicate entry name")

// ErrEntryNotFound is returned by UpdateSchedule, UpdateScheduleByName,
// UpdateJob, UpdateJobByName, UpdateEntry, UpdateEntryByName,
// UpdateEntryJob, and UpdateEntryJobByName when the specified entry does
// not exist in this Cron instance.
var ErrEntryNotFound = errors.New("cron: entry not found")

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
// An index map provides O(1) entry lookup by ID.
type Cron struct {
	entries     entryHeap
	entryIndex  map[EntryID]*Entry // O(1) lookup by ID
	nameIndex   map[string]*Entry  // O(1) lookup by Name
	chain       Chain
	stop        chan struct{}
	add         chan request[*Entry, struct{}]
	remove      chan request[EntryID, struct{}]
	update      chan request[updateScheduleRequest, error]
	snapshot    chan chan []Entry
	entryLookup chan entryLookupRequest // O(1) single-entry lookup when running
	nameLookup  chan nameLookupRequest  // O(1) entry lookup by name when running
	running     bool
	logger      Logger
	runningMu   sync.Mutex
	location    *time.Location
	parser      ScheduleParser
	nextID      EntryID
	jobWaiter   sync.WaitGroup
	clock       Clock
	hooks       *ObservabilityHooks
	maxEntries  int                // 0 means unlimited
	entryCount  int64              // atomic counter for race-free limit checking
	baseCtx     context.Context    // base context for all jobs
	cancelCtx   context.CancelFunc // cancels baseCtx when Stop() is called

	// indexDeletions tracks removals from index maps since last compaction.
	// Go maps don't release memory when entries are deleted, so we periodically
	// rebuild maps to reclaim memory in high-churn scenarios.
	indexDeletions int
}

// ScheduleParser is an interface for schedule spec parsers that return a Schedule.
type ScheduleParser interface {
	Parse(spec string) (Schedule, error)
}

// Job is an interface for submitted cron jobs.
type Job interface {
	Run()
}

// ErrorJob is an optional interface for jobs that return errors instead of panicking.
// Jobs implementing this interface can use error-based retry wrappers like RetryOnError,
// which is more idiomatic Go than the panic-based RetryWithBackoff.
//
// When a job implements ErrorJob, wrappers that understand errors (like RetryOnError)
// will call RunE() and use the returned error for retry decisions. The standard Run()
// method should still be implemented (typically delegating to RunE and panicking on error)
// for compatibility with wrappers that don't understand ErrorJob.
//
// Example:
//
//	type APIJob struct{ url string }
//
//	func (j *APIJob) Run() {
//	    if err := j.RunE(); err != nil {
//	        panic(err)
//	    }
//	}
//
//	func (j *APIJob) RunE() error {
//	    resp, err := http.Get(j.url)
//	    if err != nil {
//	        return fmt.Errorf("API call failed: %w", err)
//	    }
//	    defer resp.Body.Close()
//	    return nil
//	}
type ErrorJob interface {
	Job
	RunE() error
}

// FuncErrorJob is a wrapper that turns a func() error into an ErrorJob.
// This enables error-returning jobs using simple functions.
//
// Example:
//
//	c.AddJob("@every 5m", cron.FuncErrorJob(func() error {
//	    return callExternalAPI()
//	}))
type FuncErrorJob func() error

// Run implements Job by calling RunE and panicking on error.
// This ensures compatibility with panic-based wrappers like Recover.
func (f FuncErrorJob) Run() {
	if err := f.RunE(); err != nil {
		panic(err)
	}
}

// RunE implements ErrorJob by calling the wrapped function.
func (f FuncErrorJob) RunE() error {
	return f()
}

// JobWithContext is an optional interface for jobs that support context.Context.
// If a job implements this interface, RunWithContext is called instead of Run,
// allowing the job to:
//   - Receive cancellation signals when Stop() is called
//   - Respect deadlines and timeouts
//   - Access request-scoped values (trace IDs, correlation IDs, etc.)
//
// Jobs that don't implement this interface will continue to work unchanged
// via their Run() method.
//
// Example:
//
//	type MyJob struct{}
//
//	func (j *MyJob) Run() { j.RunWithContext(context.Background()) }
//
//	func (j *MyJob) RunWithContext(ctx context.Context) {
//	    select {
//	    case <-ctx.Done():
//	        return // Job canceled
//	    case <-time.After(time.Minute):
//	        // Do work
//	    }
//	}
type JobWithContext interface {
	Job
	RunWithContext(ctx context.Context)
}

// Schedule describes a job's duty cycle.
type Schedule interface {
	// Next returns the next activation time, later than the given time.
	// Next is invoked initially, and then each time the job is run.
	Next(time.Time) time.Time
}

// ScheduleWithPrev is an optional interface that schedules can implement
// to support backward time traversal. This is useful for detecting missed
// executions or determining the last scheduled run time.
//
// Built-in schedules (SpecSchedule, ConstantDelaySchedule) implement this
// interface. Custom Schedule implementations may optionally implement it.
//
// Use type assertion to check for support:
//
//	if sp, ok := schedule.(ScheduleWithPrev); ok {
//	    prev := sp.Prev(time.Now())
//	}
type ScheduleWithPrev interface {
	Schedule
	// Prev returns the previous activation time, earlier than the given time.
	// Returns zero time if no previous time can be found.
	Prev(time.Time) time.Time
}

// EntryID identifies an entry within a Cron instance.
// Using uint64 prevents overflow and ID collisions on all platforms.
type EntryID uint64

// entryLookupRequest is used for O(1) entry lookup via the run loop.
type entryLookupRequest struct {
	id    EntryID
	reply chan Entry
}

// nameLookupRequest is used for O(1) entry lookup by name via the run loop.
type nameLookupRequest struct {
	name  string
	reply chan Entry
}

// updateScheduleRequest is used to update an entry's schedule (required) and optionally its job.
type updateScheduleRequest struct {
	id       EntryID
	schedule Schedule // new schedule for the entry (must be provided)
	job      Job      // nil means keep existing job
}

// request is a generic way to make a request to the run loop.
type request[T, C any] struct {
	value T
	reply chan C
}

// makeReq creates a request with the given value and a buffered reply channel of size 1.
// This is used for synchronous operations where a response is expected.
func makeReq[T, C any](v T) request[T, C] {
	return request[T, C]{value: v, reply: make(chan C, 1)}
}

// Entry consists of a schedule and the func to execute on that schedule.
type Entry struct {
	// ID is the cron-assigned ID of this entry, which may be used to look up a
	// snapshot or remove it.
	ID EntryID

	// Name is an optional human-readable identifier for this entry.
	// If set, names must be unique within a Cron instance.
	// Use WithName() when adding an entry to set this field.
	Name string

	// Tags is an optional set of labels for categorizing and filtering entries.
	// Multiple entries can share the same tags.
	// Use WithTags() when adding an entry to set this field.
	Tags []string

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

	// runImmediately is an internal flag set by WithRunImmediately().
	// When true, the entry will be scheduled to run immediately upon registration.
	runImmediately bool

	// runOnce is an internal flag set by WithRunOnce().
	// When true, the entry will be automatically removed after its first execution.
	runOnce bool

	// MissedPolicy defines how to handle missed executions when the scheduler
	// starts or when an entry is added. See MissedPolicy constants.
	// Set via WithMissedPolicy(). Default is MissedSkip.
	MissedPolicy MissedPolicy

	// MissedGracePeriod defines the maximum age of a missed execution that
	// should be caught up. If zero, all missed executions (within safety limits)
	// are eligible for catch-up. Set via WithMissedGracePeriod().
	MissedGracePeriod time.Duration

	// entryCtx is a per-entry context derived from the Cron's baseCtx.
	// It is canceled when the entry is removed or when its job is replaced
	// via UpdateEntry. Jobs implementing JobWithContext receive this context,
	// allowing per-entry cancellation without stopping the entire scheduler.
	entryCtx context.Context

	// cancelEntryCtx cancels entryCtx. Called on Remove or job replacement.
	cancelEntryCtx context.CancelFunc
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
		entries:     nil,
		entryIndex:  make(map[EntryID]*Entry),
		nameIndex:   make(map[string]*Entry),
		chain:       NewChain(),
		add:         make(chan request[*Entry, struct{}]),
		stop:        make(chan struct{}),
		snapshot:    make(chan chan []Entry),
		entryLookup: make(chan entryLookupRequest),
		nameLookup:  make(chan nameLookupRequest),
		remove:      make(chan request[EntryID, struct{}]),
		update:      make(chan request[updateScheduleRequest, error]),
		running:     false,
		runningMu:   sync.Mutex{},
		logger:      DefaultLogger,
		location:    time.Local,
		parser:      standardParser,
		clock:       RealClock{},
		baseCtx:     context.Background(), // Default base context
	}
	for _, opt := range opts {
		opt(c)
	}
	// Create cancellable context derived from baseCtx (which may have been set by WithContext)
	c.baseCtx, c.cancelCtx = context.WithCancel(c.baseCtx)
	return c
}

// FuncJob is a wrapper that turns a func() into a cron.Job.
type FuncJob func()

// Run calls the wrapped function.
func (f FuncJob) Run() { f() }

// FuncJobWithContext is a wrapper that turns a func(context.Context) into a JobWithContext.
// This enables context-aware jobs using simple functions.
//
// Example:
//
//	c.AddJob("@every 1m", cron.FuncJobWithContext(func(ctx context.Context) {
//	    select {
//	    case <-ctx.Done():
//	        return // Canceled
//	    default:
//	        // Do work
//	    }
//	}))
type FuncJobWithContext func(ctx context.Context)

// Run implements Job interface by calling RunWithContext with context.Background().
func (f FuncJobWithContext) Run() {
	f.RunWithContext(context.Background())
}

// RunWithContext implements JobWithContext interface.
func (f FuncJobWithContext) RunWithContext(ctx context.Context) {
	f(ctx)
}

// JobOption configures an Entry when adding a job to Cron.
type JobOption func(*Entry)

// WithName sets a unique name for the job entry.
// Names must be unique within a Cron instance; adding a job with a duplicate
// name will return ErrDuplicateName.
//
// Named jobs can be retrieved with EntryByName() or removed with RemoveByName().
//
// Example:
//
//	c.AddFunc("@every 1h", cleanup, cron.WithName("hourly-cleanup"))
func WithName(name string) JobOption {
	return func(e *Entry) {
		e.Name = name
	}
}

// WithTags sets tags for categorizing the job entry.
// Multiple entries can share the same tags, enabling group operations.
//
// Tagged jobs can be filtered with EntriesByTag() or removed with RemoveByTag().
//
// Example:
//
//	c.AddFunc("@every 1h", cleanup, cron.WithTags("maintenance", "hourly"))
func WithTags(tags ...string) JobOption {
	return func(e *Entry) {
		e.Tags = tags
	}
}

// WithPrev sets the previous execution time for an entry.
// This is useful for:
//   - Schedule migration: preserving execution history when moving jobs between schedulers
//   - Missed execution detection: combined with schedule analysis to detect missed runs
//   - Process restarts: preserving interval-based job continuity across restarts
//
// Example:
//
//	// Migrate job with preserved history
//	lastRun := loadLastRunFromDB()
//	c.AddFunc("@every 1h", cleanup, cron.WithPrev(lastRun))
func WithPrev(prev time.Time) JobOption {
	return func(e *Entry) {
		e.Prev = prev
	}
}

// WithRunImmediately causes the job to run immediately upon registration,
// then follow the normal schedule thereafter.
// This is useful for:
//   - Initial sync: running a sync job once at startup before regular schedule
//   - Health checks: ensuring service connectivity is verified immediately
//   - Cache warming: populating caches before the first scheduled refresh
//
// Example:
//
//	// Run immediately, then every hour
//	c.AddFunc("@every 1h", syncData, cron.WithRunImmediately())
func WithRunImmediately() JobOption {
	return func(e *Entry) {
		e.runImmediately = true
	}
}

// WithRunOnce causes the job to be automatically removed after its first execution.
// This is useful for:
//   - One-time scheduled tasks: "send reminder in 24 hours"
//   - Deferred execution: schedule a task for later without manual cleanup
//   - Temporary events: schedule something for a specific time, then forget it
//
// The job is removed from the scheduler after it is dispatched, regardless of
// whether the job succeeds or fails. The job's goroutine continues to run
// independently after the entry is removed.
//
// WithRunOnce works correctly with job wrappers like Recover and RetryWithBackoff:
// the entry is removed after dispatch, but retries happen within the job's goroutine.
//
// Can be combined with WithRunImmediately to run once immediately:
//
//	// Run once right now
//	c.AddFunc("@every 1h", task, cron.WithRunOnce(), cron.WithRunImmediately())
//
// Example:
//
//	// Send reminder in 24 hours, then remove from scheduler
//	c.AddFunc("@in 24h", sendReminder, cron.WithRunOnce())
//
//	// Run at specific time, then remove
//	c.AddFunc("0 9 25 12 *", sendChristmasGreeting, cron.WithRunOnce())
func WithRunOnce() JobOption {
	return func(e *Entry) {
		e.runOnce = true
	}
}

// WithMissedPolicy configures how the scheduler handles missed job executions.
// A job is considered "missed" if it was scheduled to run while the scheduler
// was not running (e.g., during application restart).
//
// This feature requires WithPrev() to provide the last run time. Without a known
// last run time, no catch-up will occur regardless of the policy.
//
// Available policies:
//   - MissedSkip (default): Do not catch up; wait for next scheduled time
//   - MissedRunOnce: Run once immediately if any executions were missed
//   - MissedRunAll: Run for every missed execution (use with caution)
//
// Example:
//
//	lastRun := loadFromDatabase("daily-report")
//	c.AddFunc("0 9 * * *", dailyReport,
//	    cron.WithPrev(lastRun),
//	    cron.WithMissedPolicy(cron.MissedRunOnce),
//	)
func WithMissedPolicy(policy MissedPolicy) JobOption {
	return func(e *Entry) {
		e.MissedPolicy = policy
	}
}

// WithMissedGracePeriod sets the maximum age of a missed execution that should
// be caught up. If zero (default), all missed executions are eligible for catch-up
// (subject to the safety limit of 100 executions for MissedRunAll).
//
// This is useful to avoid running very old missed jobs that are no longer relevant.
// For example, if a daily report job was missed 3 days ago, you might not want
// to generate reports for all those days.
//
// Example:
//
//	// Only catch up if missed within the last 2 hours
//	c.AddFunc("0 9 * * *", dailyReport,
//	    cron.WithPrev(lastRun),
//	    cron.WithMissedPolicy(cron.MissedRunOnce),
//	    cron.WithMissedGracePeriod(2*time.Hour),
//	)
func WithMissedGracePeriod(d time.Duration) JobOption {
	return func(e *Entry) {
		e.MissedGracePeriod = d
	}
}

// ValidateSpec validates a cron expression using this Cron instance's configured parser.
// It returns nil if the spec is valid, or an error describing the problem.
//
// This is useful for pre-validating user input before calling AddFunc or AddJob,
// especially when the Cron instance uses a custom parser (e.g., with seconds or hash support).
//
// Example:
//
//	c := cron.New(cron.WithSeconds())
//	if err := c.ValidateSpec("0 30 * * * *"); err != nil {
//	    return fmt.Errorf("invalid cron expression: %w", err)
//	}
func (c *Cron) ValidateSpec(spec string) error {
	if c == nil {
		return errors.New("cron: called ValidateSpec on nil Cron instance")
	}
	if c.parser == nil {
		return errors.New("cron: parser is nil; ensure cron.New is not configured with a nil parser")
	}
	_, err := c.parser.Parse(spec)
	return err
}

// AddFunc adds a func to the Cron to be run on the given schedule.
// The spec is parsed using the time zone of this Cron instance as the default.
// An opaque ID is returned that can be used to later remove it.
//
// Optional JobOption arguments can be provided to set metadata like Name and Tags:
//
//	c.AddFunc("@every 1h", cleanup, cron.WithName("cleanup"), cron.WithTags("maintenance"))
//
// Returns ErrDuplicateName if a name is provided and already exists.
func (c *Cron) AddFunc(spec string, cmd func(), opts ...JobOption) (EntryID, error) {
	return c.AddJob(spec, FuncJob(cmd), opts...)
}

// AddOnceFunc adds a func to run once on the given schedule, then automatically remove itself.
// This is a convenience wrapper that combines AddFunc with WithRunOnce().
//
// Example:
//
//	// Send reminder in 24 hours
//	c.AddOnceFunc("@in 24h", sendReminder)
//
//	// Run at specific time
//	c.AddOnceFunc("0 9 25 12 *", sendChristmasGreeting, cron.WithName("christmas"))
func (c *Cron) AddOnceFunc(spec string, cmd func(), opts ...JobOption) (EntryID, error) {
	opts = append(opts, WithRunOnce())
	return c.AddFunc(spec, cmd, opts...)
}

// AddOnceJob adds a Job to run once on the given schedule, then automatically remove itself.
// This is a convenience wrapper that combines AddJob with WithRunOnce().
//
// Example:
//
//	c.AddOnceJob("@in 1h", myJob, cron.WithName("one-time-task"))
func (c *Cron) AddOnceJob(spec string, cmd Job, opts ...JobOption) (EntryID, error) {
	opts = append(opts, WithRunOnce())
	return c.AddJob(spec, cmd, opts...)
}

// AddJob adds a Job to the Cron to be run on the given schedule.
// The spec is parsed using the time zone of this Cron instance as the default.
// An opaque ID is returned that can be used to later remove it.
//
// Optional JobOption arguments can be provided to set metadata like Name and Tags:
//
//	c.AddJob("@every 1h", myJob, cron.WithName("my-job"), cron.WithTags("critical"))
//
// Returns ErrMaxEntriesReached if the maximum entry limit has been reached.
// Returns ErrDuplicateName if a name is provided and already exists.
func (c *Cron) AddJob(spec string, cmd Job, opts ...JobOption) (EntryID, error) {
	schedule, err := c.parser.Parse(spec)
	if err != nil {
		return 0, err
	}
	id, err := c.ScheduleJob(schedule, cmd, opts...)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// Schedule adds a Job to the Cron to be run on the given schedule.
// The job is wrapped with the configured Chain.
//
// If a maximum entry limit is configured (via WithMaxEntries) and the limit
// has been reached, Schedule returns 0 (an invalid EntryID) and logs a warning.
// Use AddJob or AddFunc to get an error return when the limit is exceeded.
//
// Note: When the cron is running, the limit check is approximate due to
// concurrent entry additions. The actual count may briefly exceed the limit
// by the number of concurrent Schedule calls in flight.
//
// Deprecated: Use ScheduleJob instead for error handling and metadata support.
func (c *Cron) Schedule(schedule Schedule, cmd Job) EntryID {
	id, err := c.ScheduleJob(schedule, cmd)
	if err != nil {
		c.logger.Error(err, "schedule failed")
		return 0
	}
	return id
}

// ScheduleOnceJob adds a Job to run once on the given schedule, then automatically remove itself.
// This is a convenience wrapper that combines ScheduleJob with WithRunOnce().
//
// Example:
//
//	// Run once at a specific time
//	schedule := cron.Every(24 * time.Hour)
//	c.ScheduleOnceJob(schedule, myJob, cron.WithName("one-time"))
func (c *Cron) ScheduleOnceJob(schedule Schedule, cmd Job, opts ...JobOption) (EntryID, error) {
	opts = append(opts, WithRunOnce())
	return c.ScheduleJob(schedule, cmd, opts...)
}

// ScheduleJob adds a Job to the Cron to be run on the given schedule.
// The job is wrapped with the configured Chain.
//
// Optional JobOption arguments can be provided to set metadata like Name and Tags:
//
//	c.ScheduleJob(schedule, myJob, cron.WithName("my-job"), cron.WithTags("critical"))
//
// Returns ErrMaxEntriesReached if the maximum entry limit has been reached.
// Returns ErrDuplicateName if a name is provided and already exists.
//
// Note: When the cron is running, the limit check is approximate due to
// concurrent entry additions. The actual count may briefly exceed the limit
// by the number of concurrent ScheduleJob calls in flight.
func (c *Cron) ScheduleJob(schedule Schedule, cmd Job, opts ...JobOption) (EntryID, error) {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()

	// Atomically check and increment entry count to prevent race conditions.
	// Must be done before any other work to ensure we can decrement on error.
	if !c.tryIncrementEntryCount() {
		return 0, ErrMaxEntriesReached
	}
	// Track that we've incremented; must decrement on any error path
	countIncremented := true
	defer func() {
		if countIncremented {
			// Error path - decrement the count we incremented
			atomic.AddInt64(&c.entryCount, -1)
		}
	}()

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

	// Apply job options
	for _, opt := range opts {
		opt(entry)
	}

	// Create per-entry context derived from the cron's base context.
	// This allows per-entry cancellation on Remove or job replacement.
	entry.entryCtx, entry.cancelEntryCtx = context.WithCancel(c.baseCtx)

	// Log info if both DOM and DOW are restricted (AND logic in effect)
	if spec, ok := schedule.(*SpecSchedule); ok {
		if spec.Dom&starBit == 0 && spec.Dow&starBit == 0 && !spec.DowOrDom {
			c.logger.Info("schedule uses AND logic for day matching",
				"reason", "both day-of-month and day-of-week are restricted",
				"hint", "use DowOrDom parser option for legacy OR behavior",
				"entry", entry.Name)
		}
	}

	// Check for duplicate name
	if entry.Name != "" {
		if _, exists := c.nameIndex[entry.Name]; exists {
			c.nextID-- // Revert ID allocation
			return 0, ErrDuplicateName
		}
		// Reserve name immediately to prevent TOCTOU race when running
		c.nameIndex[entry.Name] = entry
	}

	if !c.running {
		heap.Push(&c.entries, entry)
		c.entryIndex[entry.ID] = entry
	} else {
		req := makeReq[*Entry, struct{}](entry)
		c.add <- req
		<-req.reply
	}
	// Success - don't decrement count in deferred function
	countIncremented = false
	return entry.ID, nil
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

// Location gets the time zone location.
func (c *Cron) Location() *time.Location {
	return c.location
}

// Entry returns a snapshot of the given entry, or nil if it couldn't be found.
// This operation is O(1) in all cases using the internal index map.
func (c *Cron) Entry(id EntryID) Entry {
	c.runningMu.Lock()
	if c.running {
		c.runningMu.Unlock()
		// When running, use dedicated lookup channel for O(1) access
		replyChan := make(chan Entry, 1)
		c.entryLookup <- entryLookupRequest{id: id, reply: replyChan}
		return <-replyChan
	}
	// When not running, use direct map lookup (O(1))
	entry, ok := c.entryIndex[id]
	c.runningMu.Unlock()
	if ok {
		return *entry
	}
	return Entry{}
}

// Remove an entry from being run in the future.
func (c *Cron) Remove(id EntryID) {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	if c.running {
		req := makeReq[EntryID, struct{}](id)
		c.remove <- req
		<-req.reply
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

// scheduleEntryNext calculates and sets the next run time for an entry.
// If runImmediately is set, the entry runs at 'now'; otherwise it uses the schedule.
func (c *Cron) scheduleEntryNext(entry *Entry, now time.Time) {
	if entry.runImmediately {
		entry.Next = now
		entry.runImmediately = false // Clear flag after use
	} else {
		entry.Next = entry.Schedule.Next(now)
	}
}

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

// processDueEntries runs all entries whose scheduled time has passed.
// Entries are processed in order from the heap and rescheduled for their next run.
// Run-once entries are removed after being dispatched.
func (c *Cron) processDueEntries(now time.Time) {
	for c.entries.Peek() != nil {
		e := c.entries.Peek()
		if e.Next.After(now) || e.Next.IsZero() {
			break
		}
		scheduledTime := e.Next
		c.startJob(e.entryCtx, e.ID, e.Job, e.WrappedJob, scheduledTime)
		e.Prev = e.Next

		if e.runOnce {
			// Remove run-once entries after dispatching the job.
			// The job continues running in its own goroutine.
			c.removeEntry(e.ID)
			c.logger.Info("run-once", "now", now, "entry", e.ID, "removed", true)
		} else {
			e.Next = e.Schedule.Next(now)
			c.hooks.callOnSchedule(e.ID, e.Job, e.Next)
			c.entries.Update(e) // Re-heapify after updating Next time
			c.logger.Info("run", "now", now, "entry", e.ID, "next", e.Next)
		}
	}
}

func (c *Cron) run() {
	c.logger.Info("start")

	// Figure out the next activation times for each entry and initialize heap.
	now := c.now()
	for _, entry := range c.entries {
		// Check for missed executions before scheduling next run
		c.processMissedRuns(entry, now)
		c.scheduleEntryNext(entry, now)
		c.hooks.callOnSchedule(entry.ID, entry.Job, entry.Next)
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
				c.processDueEntries(now)

			case req := <-c.add:
				newEntry := req.value
				timer.Stop()
				now = c.now()
				// Check for missed executions before scheduling next run
				c.processMissedRuns(newEntry, now)
				c.scheduleEntryNext(newEntry, now)
				heap.Push(&c.entries, newEntry)
				c.entryIndex[newEntry.ID] = newEntry
				req.reply <- struct{}{}
				// Note: nameIndex and entryCount already updated by ScheduleJob
				// (while holding runningMu) to prevent TOCTOU races
				c.hooks.callOnSchedule(newEntry.ID, newEntry.Job, newEntry.Next)
				c.logger.Info("added", "now", now, "entry", newEntry.ID, "next", newEntry.Next)

			case replyChan := <-c.snapshot:
				replyChan <- c.entrySnapshot()
				continue

			case req := <-c.entryLookup:
				// O(1) single-entry lookup using index map
				if entry, ok := c.entryIndex[req.id]; ok {
					req.reply <- *entry
				} else {
					req.reply <- Entry{}
				}
				continue

			case req := <-c.nameLookup:
				// O(1) entry lookup by name using nameIndex
				if entry, ok := c.nameIndex[req.name]; ok {
					req.reply <- *entry
				} else {
					req.reply <- Entry{}
				}
				continue

			case <-c.stop:
				timer.Stop()
				c.logger.Info("stop")
				return

			case req := <-c.remove:
				id := req.value
				timer.Stop()
				now = c.now()
				c.removeEntry(id)
				req.reply <- struct{}{}
				c.logger.Info("removed", "entry", id)

			case req := <-c.update:
				err := c.updateSchedule(&req.value)
				req.reply <- err
				if err != nil {
					continue
				}

				timer.Stop()
				now = c.now()
				c.logger.Info("updated", "entry", req.value.id)
			}

			break
		}
	}
}

// startJob runs the given job in a new goroutine with observability hooks.
// The originalJob is used for name extraction, wrappedJob is the actual job to run.
//
// If wrappedJob implements JobWithContext, RunWithContext is called with the entry's
// per-entry context, allowing the job to receive cancellation signals when the entry
// is removed, its job is replaced, or Stop() is called (which cancels baseCtx,
// cascading to all entry contexts).
func (c *Cron) startJob(entryCtx context.Context, entryID EntryID, originalJob, wrappedJob Job, scheduledTime time.Time) {
	c.jobWaiter.Add(1)
	go func() {
		defer c.jobWaiter.Done()

		c.hooks.callOnJobStart(entryID, originalJob, scheduledTime)

		start := c.clock.Now()
		var recovered any
		func() {
			defer func() {
				recovered = recover()
			}()
			// Check if the job supports context and call appropriate method
			if jc, ok := wrappedJob.(JobWithContext); ok {
				jc.RunWithContext(entryCtx)
			} else {
				wrappedJob.Run()
			}
		}()
		duration := c.clock.Now().Sub(start)

		c.hooks.callOnJobComplete(entryID, originalJob, duration, recovered)

		// Re-panic if the job panicked and wasn't handled by a wrapper
		if recovered != nil {
			panic(recovered)
		}
	}()
}

// updateSchedule updates the schedule (and optionally the job) of an existing
// entry. When req.job is non-nil the entry's Job and WrappedJob are replaced;
// WrappedJob is re-wrapped through the configured Chain so middleware is applied.
// When the scheduler is running, it also recalculates the entry's next
// activation time relative to the current clock and fixes the heap to maintain
// ordering. When the scheduler is stopped, only the schedule (and job, if
// provided) are updated; the next activation time will be recomputed when
// scheduling resumes. Returns ErrEntryNotFound if the entry ID is unknown.
//
// Concurrency: Must be called only from the scheduler's run loop (when
// c.running is true) which owns the data exclusively, or from UpdateSchedule /
// UpdateEntry when the scheduler is stopped (caller holds runningMu).
// Complexity: O(log n) while running due to heap.Fix, O(1) when stopped.
func (c *Cron) updateSchedule(req *updateScheduleRequest) error {
	entry, found := c.entryIndex[req.id]
	if !found {
		return ErrEntryNotFound
	}

	entry.Schedule = req.schedule
	if req.job != nil {
		// Cancel the old entry context so running jobs for the old closure
		// receive a cancellation signal, then create a fresh context for
		// the replacement job.
		if entry.cancelEntryCtx != nil {
			entry.cancelEntryCtx()
		}
		entry.entryCtx, entry.cancelEntryCtx = context.WithCancel(c.baseCtx)
		entry.Job = req.job
		entry.WrappedJob = c.chain.Then(req.job)
	}
	if c.running {
		c.scheduleEntryNext(entry, c.now())
		c.entries.Update(entry)
		c.hooks.callOnSchedule(entry.ID, entry.Job, entry.Next)
	}
	return nil
}

// UpdateJob updates the schedule of an existing entry identified by id,
// parsing the provided cron spec string using this Cron's configured parser.
//
// If the scheduler is running, the update is applied safely via the run loop
// and takes effect immediately for next-run computation. If stopped, the
// schedule is updated directly in place.
//
// Returns ErrEntryNotFound if the id does not correspond to an existing entry.
// Returns a parse error if spec is invalid for the configured parser.
func (c *Cron) UpdateJob(id EntryID, spec string) error {
	schedule, err := c.parser.Parse(spec)
	if err != nil {
		return err
	}

	return c.UpdateSchedule(id, schedule)
}

// UpdateSchedule updates the Schedule of an existing entry identified by id.
//
// Concurrency semantics:
//   - If the scheduler is running, the change is routed through the run loop
//     to avoid races, and the heap is adjusted atomically. The new schedule is
//     used to recompute the entry's next run immediately.
//   - If the scheduler is stopped, the schedule is updated directly.
//
// Returns ErrEntryNotFound if no entry with the given id exists.
func (c *Cron) UpdateSchedule(id EntryID, schedule Schedule) error {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	request := updateScheduleRequest{id: id, schedule: schedule}
	if c.running {
		req := makeReq[updateScheduleRequest, error](request)
		c.update <- req
		return <-req.reply
	}

	return c.updateSchedule(&request)
}

// UpdateScheduleByName updates the Schedule of an existing entry identified by
// its Name. Lookup is O(1) via the internal name index. If the scheduler is
// running, the actual update is delegated to UpdateSchedule which routes through
// the run loop safely.
//
// Returns ErrEntryNotFound if no entry with the given name exists.
func (c *Cron) UpdateScheduleByName(name string, schedule Schedule) error {
	e := c.EntryByName(name)
	if !e.Valid() {
		return ErrEntryNotFound
	}
	return c.UpdateSchedule(e.ID, schedule)
}

// UpdateJobByName updates the schedule of an existing entry identified by its
// Name, parsing the provided cron spec using this Cron's configured parser.
//
// Returns ErrEntryNotFound if the name does not correspond to an existing
// entry. Returns a parse error if the spec is invalid for the configured
// parser.
func (c *Cron) UpdateJobByName(name, spec string) error {
	schedule, err := c.parser.Parse(spec)
	if err != nil {
		return err
	}
	return c.UpdateScheduleByName(name, schedule)
}

// ErrNilJob is returned by UpdateEntry and UpdateEntryByName when a nil job
// is passed. Use UpdateSchedule to update only the schedule.
var ErrNilJob = errors.New("cron: job must not be nil; use UpdateSchedule to update only the schedule")

// UpdateEntry atomically replaces both the Schedule and the Job of an existing
// entry identified by id. The new job is re-wrapped through the configured
// Chain, so middleware (Recover, SkipIfStillRunning, etc.) is applied to the
// replacement job. The job parameter must not be nil; to update only the
// schedule, use UpdateSchedule instead.
//
// This is useful when rescheduling requires a new closure—for example, a fresh
// context.WithCancel per schedule change (the weaviate pattern).
//
// Concurrency semantics are the same as UpdateSchedule.
//
// Returns ErrEntryNotFound if no entry with the given id exists.
// Returns ErrNilJob if job is nil.
func (c *Cron) UpdateEntry(id EntryID, schedule Schedule, job Job) error {
	if job == nil {
		return ErrNilJob
	}
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	request := updateScheduleRequest{id: id, schedule: schedule, job: job}
	if c.running {
		req := makeReq[updateScheduleRequest, error](request)
		c.update <- req
		return <-req.reply
	}

	return c.updateSchedule(&request)
}

// UpdateEntryByName atomically replaces both the Schedule and the Job of an
// existing entry identified by its Name. Lookup is O(1) via the internal name
// index. Delegates to UpdateEntry for the actual update.
//
// Returns ErrEntryNotFound if no entry with the given name exists.
func (c *Cron) UpdateEntryByName(name string, schedule Schedule, job Job) error {
	e := c.EntryByName(name)
	if !e.Valid() {
		return ErrEntryNotFound
	}
	return c.UpdateEntry(e.ID, schedule, job)
}

// UpdateEntryJob parses spec with the Cron's configured parser, then atomically
// replaces both schedule and job. This eliminates the need for callers to
// construct their own parser matching the Cron's configuration.
//
// Returns a parse error if spec is invalid for the configured parser.
// Returns ErrEntryNotFound if the id does not correspond to an existing entry.
// Returns ErrNilJob if job is nil.
func (c *Cron) UpdateEntryJob(id EntryID, spec string, job Job) error {
	schedule, err := c.parser.Parse(spec)
	if err != nil {
		return err
	}
	return c.UpdateEntry(id, schedule, job)
}

// UpdateEntryJobByName is the name-based variant of UpdateEntryJob.
// It parses spec with the Cron's configured parser, then atomically replaces
// both schedule and job of the entry identified by name.
//
// Returns a parse error if spec is invalid for the configured parser.
// Returns ErrEntryNotFound if the name does not correspond to an existing entry.
// Returns ErrNilJob if job is nil.
func (c *Cron) UpdateEntryJobByName(name, spec string, job Job) error {
	schedule, err := c.parser.Parse(spec)
	if err != nil {
		return err
	}
	return c.UpdateEntryByName(name, schedule, job)
}

// now returns current time in c location.
// Uses the configured clock (defaults to RealClock).
func (c *Cron) now() time.Time {
	return c.clock.Now().In(c.location)
}

// Stop stops the cron scheduler if it is running; otherwise it does nothing.
// A context is returned so the caller can wait for running jobs to complete.
//
// When Stop is called, the base context is canceled, signaling all running jobs
// that implement JobWithContext to shut down gracefully. Jobs should check
// ctx.Done() and return promptly when canceled.
func (c *Cron) Stop() context.Context {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	if c.running {
		c.stop <- struct{}{}
		c.running = false
	}
	// Cancel the base context to signal running jobs to stop
	if c.cancelCtx != nil {
		c.cancelCtx()
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		c.jobWaiter.Wait()
		cancel()
	}()
	return ctx
}

// IsRunning returns true if the cron scheduler is currently running.
// This can be used for health checks, conditional starts, or debugging.
func (c *Cron) IsRunning() bool {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	return c.running
}

// StopAndWait stops the cron scheduler and blocks until all running jobs complete.
// This is a convenience method equivalent to:
//
//	ctx := c.Stop()
//	<-ctx.Done()
//
// For timeout-based shutdown, use StopWithTimeout() or use Stop() directly:
//
//	ctx := c.Stop()
//	select {
//	case <-ctx.Done():
//	    // All jobs completed
//	case <-time.After(5 * time.Second):
//	    // Timeout - some jobs may still be running
//	}
func (c *Cron) StopAndWait() {
	<-c.Stop().Done()
}

// StopWithTimeout stops the cron scheduler and waits for running jobs to complete
// with a timeout. Returns true if all jobs completed within the timeout,
// false if the timeout was reached and some jobs may still be running.
//
// When the timeout is reached, jobs that implement JobWithContext should already
// have received context cancellation and should be in the process of shutting down.
// Jobs that don't check their context may continue running in the background.
//
// A timeout of zero or negative waits indefinitely (equivalent to StopAndWait).
//
// Example:
//
//	if !c.StopWithTimeout(30 * time.Second) {
//	    log.Println("Warning: some jobs did not complete within 30s")
//	}
func (c *Cron) StopWithTimeout(timeout time.Duration) bool {
	ctx := c.Stop()
	if timeout <= 0 {
		<-ctx.Done()
		return true
	}
	select {
	case <-ctx.Done():
		return true
	case <-time.After(timeout):
		return false
	}
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

// sortEntriesByTime sorts entries in place by their Next scheduled execution time.
// Entries with zero time (not scheduled or schedule exhausted) are moved to the
// end of the slice to keep active entries at the front for efficient iteration.
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

// tryIncrementEntryCount atomically checks and increments the entry count.
// Returns true if the increment was successful (under limit or unlimited),
// false if the limit has been reached.
// This uses Compare-And-Swap to prevent race conditions where multiple
// concurrent ScheduleJob calls could exceed the maxEntries limit.
func (c *Cron) tryIncrementEntryCount() bool {
	if c.maxEntries <= 0 {
		return true // unlimited
	}
	for {
		current := atomic.LoadInt64(&c.entryCount)
		if int(current) >= c.maxEntries {
			return false
		}
		if atomic.CompareAndSwapInt64(&c.entryCount, current, current+1) {
			return true
		}
		// CAS failed, another goroutine modified count - retry
	}
}

// removeEntry removes the entry with the given ID from the scheduler.
// It removes the entry from the heap, both index maps, and decrements the entry count.
// If the entry has a name, it is also removed from the nameIndex.
// After removal, it may trigger index map compaction to reclaim memory.
// If the ID is not found, the function returns without error.
//
// IMPORTANT: This function must ONLY be called from:
//   - The run loop (when c.running is true) - owns the data exclusively
//   - Remove() when not running - caller already holds runningMu
//
// This avoids a deadlock where ScheduleJob holds runningMu while sending to c.add,
// and a concurrent run-once job completion tries to acquire runningMu here.
func (c *Cron) removeEntry(id EntryID) {
	entry, ok := c.entryIndex[id]
	if !ok {
		return
	}

	// Cancel the per-entry context to signal running jobs for this entry.
	if entry.cancelEntryCtx != nil {
		entry.cancelEntryCtx()
	}

	c.entries.RemoveAt(entry)
	delete(c.entryIndex, id)

	// Remove from nameIndex. No mutex needed:
	// - When running: run loop owns all data exclusively, no concurrent access
	// - When not running: caller (Remove) already holds runningMu
	if entry.Name != "" {
		delete(c.nameIndex, entry.Name)
	}
	atomic.AddInt64(&c.entryCount, -1)

	// Track deletions and compact maps when threshold is met.
	// Go maps don't release memory on delete, so we rebuild periodically.
	c.indexDeletions++
	c.maybeCompactIndexes()
}

// indexCompactionThreshold is the minimum number of deletions before considering compaction.
// This avoids compacting maps for low-churn use cases.
const indexCompactionThreshold = 1000

// maybeCompactIndexes rebuilds index maps if deletion count exceeds threshold
// and is proportional to current map size. This reclaims memory from Go's
// map implementation which doesn't shrink on delete.
//
// IMPORTANT: This function must ONLY be called from:
//   - The run loop (when c.running is true) - owns the data exclusively
//   - Remove() when not running - caller already holds runningMu
//
// This avoids a deadlock where ScheduleJob holds runningMu while sending to c.add,
// and a concurrent compaction tries to acquire runningMu here.
func (c *Cron) maybeCompactIndexes() {
	// Only compact if we've deleted enough entries AND the deletion count
	// is significant relative to remaining entries. This avoids rebuilding
	// huge maps for small numbers of deletions.
	if c.indexDeletions < indexCompactionThreshold {
		return
	}
	currentSize := len(c.entryIndex)
	if currentSize > 0 && c.indexDeletions <= currentSize {
		return
	}

	// Rebuild entryIndex
	newEntryIndex := make(map[EntryID]*Entry, currentSize)
	maps.Copy(newEntryIndex, c.entryIndex)
	c.entryIndex = newEntryIndex

	// Rebuild nameIndex. No mutex needed:
	// - When running: run loop owns all data exclusively, no concurrent access
	// - When not running: caller (Remove) already holds runningMu
	newNameIndex := make(map[string]*Entry, len(c.nameIndex))
	maps.Copy(newNameIndex, c.nameIndex)
	c.nameIndex = newNameIndex

	c.indexDeletions = 0
}

// EntryByName returns a snapshot of the entry with the given name,
// or an invalid Entry (Entry.Valid() == false) if not found.
//
// This operation is O(1) in all cases using the internal name index.
func (c *Cron) EntryByName(name string) Entry {
	c.runningMu.Lock()
	if c.running {
		c.runningMu.Unlock()
		// When running, use dedicated lookup channel for O(1) access
		replyChan := make(chan Entry, 1)
		c.nameLookup <- nameLookupRequest{name: name, reply: replyChan}
		return <-replyChan
	}
	// When not running, use direct map lookup (O(1))
	entry, ok := c.nameIndex[name]
	c.runningMu.Unlock()
	if ok {
		return *entry
	}
	return Entry{}
}

// EntriesByTag returns snapshots of all entries that have the given tag.
// Returns an empty slice if no entries match.
func (c *Cron) EntriesByTag(tag string) []Entry {
	var result []Entry
	for _, entry := range c.Entries() {
		if slices.Contains(entry.Tags, tag) {
			result = append(result, entry)
		}
	}
	return result
}

// RemoveByName removes the entry with the given name.
// Returns true if an entry was removed, false if no entry had that name.
func (c *Cron) RemoveByName(name string) bool {
	entry := c.EntryByName(name)
	if !entry.Valid() {
		return false
	}
	c.Remove(entry.ID)
	return true
}

// RemoveByTag removes all entries that have the given tag.
// Returns the number of entries removed.
func (c *Cron) RemoveByTag(tag string) int {
	entries := c.EntriesByTag(tag)
	for _, entry := range entries {
		c.Remove(entry.ID)
	}
	return len(entries)
}
