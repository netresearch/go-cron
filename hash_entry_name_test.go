package cron

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// hashMinuteOf returns the minute at which the schedule fires next.
func hashMinuteOf(t *testing.T, s Schedule) int {
	t.Helper()
	return s.Next(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)).Minute()
}

func hashParser() Parser {
	return NewParser(Minute | Hour | Dom | Month | Dow | Hash)
}

// plainParser is a ScheduleParser without ParseWithHashKey.
type plainParser struct{ p Parser }

func (p plainParser) Parse(spec string) (Schedule, error) { return p.p.Parse(spec) }

// tokyoParser embeds Parser, so it has ParseWithHashKey by promotion, and
// overrides Parse to set a zone.
type tokyoParser struct{ Parser }

func (p tokyoParser) Parse(spec string) (Schedule, error) {
	return p.Parser.Parse("TZ=Asia/Tokyo " + spec)
}

// TestEntryNameIsHashKey verifies that every way of adding a job with a spec
// uses the entry's name as the hash key (#470). job-a selects minute 54 and
// job-b minute 23 (see ExampleNewParser_hash).
func TestEntryNameIsHashKey(t *testing.T) {
	t.Parallel()
	noop := FuncJob(func() {})
	adders := map[string]func(c *Cron, name string) (EntryID, error){
		"AddFunc": func(c *Cron, name string) (EntryID, error) {
			return c.AddFunc("H * * * *", func() {}, WithName(name))
		},
		"AddJob": func(c *Cron, name string) (EntryID, error) {
			return c.AddJob("H * * * *", noop, WithName(name))
		},
		"AddOnceFunc": func(c *Cron, name string) (EntryID, error) {
			return c.AddOnceFunc("H * * * *", func() {}, WithName(name))
		},
		"AddOnceJob": func(c *Cron, name string) (EntryID, error) {
			return c.AddOnceJob("H * * * *", noop, WithName(name))
		},
		"UpsertJob": func(c *Cron, name string) (EntryID, error) {
			return c.UpsertJob("H * * * *", noop, WithName(name))
		},
		"DrainAndUpsertJob": func(c *Cron, name string) (EntryID, error) {
			return c.DrainAndUpsertJob("H * * * *", noop, WithName(name))
		},
	}
	want := map[string]int{"job-a": 54, "job-b": 23}

	for api, add := range adders {
		t.Run(api, func(t *testing.T) {
			t.Parallel()
			for _, c := range []*Cron{
				New(WithParser(hashParser())),                    // no parser key
				New(WithParser(hashParser().WithHashKey("svc"))), // the name wins
			} {
				for name, minute := range want {
					id, err := add(c, name)
					if err != nil {
						t.Fatalf("%s: %v", name, err)
					}
					if got := hashMinuteOf(t, c.Entry(id).Schedule); got != minute {
						t.Errorf("%s: minute %d, want %d", name, got, minute)
					}
				}
			}
		})
	}
}

// TestUpdateKeepsEntryNameAsHashKey verifies that an update with a spec keeps
// the entry's minute instead of falling back to the parser's key.
func TestUpdateKeepsEntryNameAsHashKey(t *testing.T) {
	t.Parallel()
	noop := FuncJob(func() {})
	updates := map[string]func(c *Cron, id EntryID) error{
		"UpdateJob": func(c *Cron, id EntryID) error {
			return c.UpdateJob(id, "H * * * *")
		},
		"UpdateJobByName": func(c *Cron, _ EntryID) error {
			return c.UpdateJobByName("job-a", "H * * * *")
		},
		"UpdateEntryJob": func(c *Cron, id EntryID) error {
			return c.UpdateEntryJob(id, "H * * * *", noop)
		},
		"UpdateEntryJobByName": func(c *Cron, _ EntryID) error {
			return c.UpdateEntryJobByName("job-a", "H * * * *", noop)
		},
		"UpsertJob": func(c *Cron, _ EntryID) error {
			_, err := c.UpsertJob("H * * * *", noop, WithName("job-a"))
			return err
		},
		"DrainAndUpsertJob": func(c *Cron, _ EntryID) error {
			_, err := c.DrainAndUpsertJob("H * * * *", noop, WithName("job-a"))
			return err
		},
	}

	for api, update := range updates {
		t.Run(api, func(t *testing.T) {
			t.Parallel()
			c := New(WithParser(hashParser().WithHashKey("svc")))
			id, err := c.AddFunc("0 * * * *", func() {}, WithName("job-a"))
			if err != nil {
				t.Fatal(err)
			}
			if err := update(c, id); err != nil {
				t.Fatal(err)
			}
			if got := hashMinuteOf(t, c.Entry(id).Schedule); got != 54 {
				t.Errorf("minute %d, want 54", got)
			}
		})
	}
}

// TestUpdateUnnamedEntryUsesParserKey verifies that an unnamed entry keeps the
// parser's key through an update by id.
func TestUpdateUnnamedEntryUsesParserKey(t *testing.T) {
	t.Parallel()
	c := New(WithParser(hashParser().WithHashKey("job-b")))
	id, err := c.AddFunc("0 * * * *", func() {})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.UpdateJob(id, "H * * * *"); err != nil {
		t.Fatal(err)
	}
	if got := hashMinuteOf(t, c.Entry(id).Schedule); got != 23 {
		t.Errorf("minute %d, want 23", got)
	}
}

// TestUnnamedJobHashKey verifies the fallback for a job without a name: the
// parser's key, or the error for a missing key.
func TestUnnamedJobHashKey(t *testing.T) {
	t.Parallel()

	c := New(WithParser(hashParser().WithHashKey("job-a")))
	id, err := c.AddFunc("H * * * *", func() {})
	if err != nil {
		t.Fatal(err)
	}
	if got := hashMinuteOf(t, c.Entry(id).Schedule); got != 54 {
		t.Errorf("minute %d, want the parser key's 54", got)
	}

	c = New(WithParser(hashParser()))
	if _, err := c.AddFunc("H * * * *", func() {}); err == nil ||
		!strings.Contains(err.Error(), "require a hash key") {
		t.Errorf("unnamed job without a key: err = %v", err)
	}
}

// TestOtherParserUsesParse verifies that only the built-in Parser gets the name
// as the key. Any other ScheduleParser is called through Parse, also a wrapper
// that embeds Parser and so has ParseWithHashKey by promotion.
func TestOtherParserUsesParse(t *testing.T) {
	t.Parallel()

	c := New(WithParser(plainParser{hashParser().WithHashKey("job-b")}))
	id, err := c.AddFunc("H * * * *", func() {}, WithName("job-a"))
	if err != nil {
		t.Fatal(err)
	}
	if got := hashMinuteOf(t, c.Entry(id).Schedule); got != 23 {
		t.Errorf("plain parser: minute %d, want the parser key's 23", got)
	}

	c = New(WithParser(tokyoParser{hashParser().WithHashKey("job-b")}))
	for _, spec := range []string{"H 12 * * *", "0 12 * * *"} {
		id, err := c.AddFunc(spec, func() {}, WithName("job-a"+spec))
		if err != nil {
			t.Fatal(err)
		}
		s := c.Entry(id).Schedule.(*SpecSchedule)
		if got := s.Location.String(); got != "Asia/Tokyo" {
			t.Errorf("wrapper, %q: zone %s, want Asia/Tokyo from its Parse", spec, got)
		}
	}
}

// TestPointerParserUsesName verifies that a *Parser gets the name as the key.
func TestPointerParserUsesName(t *testing.T) {
	t.Parallel()
	p := hashParser()
	c := New(WithParser(&p))
	id, err := c.AddFunc("H * * * *", func() {}, WithName("job-a"))
	if err != nil {
		t.Fatal(err)
	}
	if got := hashMinuteOf(t, c.Entry(id).Schedule); got != 54 {
		t.Errorf("minute %d, want 54", got)
	}
}

// TestNilJobOption verifies that a nil option is skipped when an entry is
// created. On a running Cron that happens in the run loop, where a panic ends
// the process.
func TestNilJobOption(t *testing.T) {
	t.Parallel()
	c := New()
	c.Start()
	defer c.Stop()

	noop := FuncJob(func() {})
	if _, err := c.UpsertJob("* * * * *", noop, WithName("upsert"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.DrainAndUpsertJob("* * * * *", noop, WithName("drain"), nil); err != nil {
		t.Fatal(err)
	}
	id, err := c.AddFunc("* * * * *", func() {}, nil, WithName("add"))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Entry(id).Name; got != "add" {
		t.Errorf("name %q, want the option after the nil one applied", got)
	}
	for _, name := range []string{"upsert", "drain"} {
		if !c.EntryByName(name).Valid() {
			t.Errorf("entry %q is missing", name)
		}
	}
}

// TestJobOptionReadsScheduleWithHash verifies that an option that reads what
// scheduling fills in works with an H spec. The name lookup runs before the
// entry exists, so the option meets an Entry without a Schedule there.
func TestJobOptionReadsScheduleWithHash(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	readsSchedule := func(e *Entry) { e.Prev = e.Schedule.Next(from) }
	noop := FuncJob(func() {})

	add := func(c *Cron) (EntryID, error) {
		return c.AddJob("H * * * *", noop, readsSchedule, WithName("job-a"))
	}
	upsert := func(c *Cron) (EntryID, error) {
		return c.UpsertJob("H * * * *", noop, readsSchedule, WithName("job-a"))
	}
	for name, run := range map[string]func(*Cron) (EntryID, error){"AddJob": add, "UpsertJob": upsert} {
		c := New(WithParser(hashParser().WithHashKey("job-b")))
		id, err := run(c)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		e := c.Entry(id)
		if got := hashMinuteOf(t, e.Schedule); got != 54 {
			t.Errorf("%s: minute %d, want the name's 54", name, got)
		}
		if e.Prev.IsZero() {
			t.Errorf("%s: the option did not run on the real entry", name)
		}
	}
}

// TestJobOptionsWithoutHash verifies that a spec without an H expression does
// not evaluate the job options before ScheduleJob, so an option may read what
// scheduling fills in, and that a nil option does not hide a parse error.
func TestJobOptionsWithoutHash(t *testing.T) {
	t.Parallel()
	c := New()
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	readsSchedule := func(e *Entry) { e.Prev = e.Schedule.Next(from) }
	if _, err := c.AddFunc("* * * * *", func() {}, readsSchedule); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddFunc("bogus", func() {}, nil); err == nil ||
		!strings.Contains(err.Error(), "expected exactly 5 fields") {
		t.Errorf("invalid spec with a nil option: err = %v", err)
	}
	if _, err := c.AddFunc("H bogus", func() {}, nil); err == nil {
		t.Error("invalid H spec with a nil option was accepted")
	}
}

// TestAddWorkflowHashKey verifies that workflow steps, which are always named,
// pass validation without a parser key and spread by step name.
func TestAddWorkflowHashKey(t *testing.T) {
	t.Parallel()
	c := New(WithParser(hashParser()))
	w := NewWorkflow("wf")
	w.StepFunc("job-a", "H * * * *", func() {})
	w.StepFunc("job-b", "H * * * *", func() {})
	if err := c.AddWorkflow(w); err != nil {
		t.Fatal(err)
	}
	for name, minute := range map[string]int{"job-a": 54, "job-b": 23} {
		if got := hashMinuteOf(t, c.EntryByName(name).Schedule); got != minute {
			t.Errorf("%s: minute %d, want %d", name, got, minute)
		}
	}
}

// TestCronValidateSpecHash verifies that Cron.ValidateSpec accepts an H spec
// without a parser key, because a named job can run it, and still rejects
// invalid H syntax and a parser without the Hash option.
func TestCronValidateSpecHash(t *testing.T) {
	t.Parallel()
	c := New(WithParser(hashParser()))
	if err := c.ValidateSpec("H * * * *"); err != nil {
		t.Errorf("valid H spec: %v", err)
	}
	if err := c.ValidateSpec("H(70-80) * * * *"); err == nil {
		t.Error("H range out of bounds was accepted")
	}
	c = New(WithParser(NewParser(Minute | Hour | Dom | Month | Dow)))
	if err := c.ValidateSpec("H * * * *"); err == nil {
		t.Error("parser without the Hash option accepted H")
	}
}

// TestCronValidateSpecUsesParserKey verifies that a parser with its own key is
// validated with that key, as an unnamed job would run: with StrictDays the
// result depends on the value the key selects.
func TestCronValidateSpecUsesParserKey(t *testing.T) {
	t.Parallel()
	strict := hashParser()
	strict.options |= StrictDays
	for _, key := range []string{"a", "b", "c", "d", "e", "f"} {
		p := strict.WithHashKey(key)
		c := New(WithParser(p))
		for _, spec := range []string{"0 0 H(29-31) 2 *", "0 0 31 H *"} {
			_, want := p.Parse(spec)
			if got := c.ValidateSpec(spec); (got == nil) != (want == nil) {
				t.Errorf("key %s, %q: ValidateSpec = %v, Parse = %v", key, spec, got, want)
			}
		}
	}
}

// TestUpdateByIDNotFoundWithHashSpec verifies that an unknown id reports
// ErrEntryNotFound and not the missing hash key.
func TestUpdateByIDNotFoundWithHashSpec(t *testing.T) {
	t.Parallel()
	c := New(WithParser(hashParser()))
	if err := c.UpdateJob(999, "H * * * *"); !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("UpdateJob: err = %v", err)
	}
	if err := c.UpdateEntryJob(999, "H * * * *", FuncJob(func() {})); !errors.Is(err, ErrEntryNotFound) {
		t.Errorf("UpdateEntryJob: err = %v", err)
	}
}
