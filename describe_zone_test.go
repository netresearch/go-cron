// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cron

import (
	"errors"
	"slices"
	"testing"
	"time"
)

// goldenZone loads a scheduler zone the golden tables pass. A host without
// it gets nil, so the goldens that pass it fail and the other tests run.
func goldenZone(name string) *time.Location {
	loc, _ := time.LoadLocation(name)
	return loc
}

// The scheduler zones the zoned goldens pass; a golden that passes none
// gives the shift from UTC only. schedLocal is a zone named "Local", as time.Local is.
var (
	schedUTC   = WithSchedulerLocation(time.UTC)
	schedNY    = WithSchedulerLocation(goldenZone("America/New_York"))
	schedLA    = WithSchedulerLocation(goldenZone("America/Los_Angeles"))
	schedLon   = WithSchedulerLocation(goldenZone("Europe/London"))
	schedKol   = WithSchedulerLocation(goldenZone("Asia/Kolkata"))
	schedLocal = WithSchedulerLocation(time.FixedZone("Local", -8*3600))
)

func TestWithSchedulerLocation(t *testing.T) {
	la := loadLocation(t, "America/Los_Angeles")
	if c := newDescribeConfig([]DescribeOption{WithSchedulerLocation(la)}); c.sched != la {
		t.Errorf("sched = %v, want Los Angeles", c.sched)
	}
	if c := newDescribeConfig([]DescribeOption{WithSchedulerLocation(nil)}); c.sched != nil {
		t.Errorf("nil: sched = %v, want nil (none)", c.sched)
	}
	if c := newDescribeConfig(nil); c.parse != nil || c.sched != nil {
		t.Errorf("zero config changed: %+v", c)
	}
}

func TestZoneName(t *testing.T) {
	for _, tt := range []struct {
		loc     *time.Location
		want    string
		generic bool
	}{
		{loadLocation(t, "America/New_York"), "New York", false},
		{loadLocation(t, "Europe/Berlin"), "Berlin", false},
		{loadLocation(t, "America/Argentina/Buenos_Aires"), "Buenos Aires", false},
		{time.UTC, "UTC", false},
		{loadLocation(t, "Etc/UTC"), "Etc/UTC", false},
		{loadLocation(t, "Etc/GMT+5"), "Etc/GMT+5", false},
		{loadLocation(t, "EST5EDT"), "EST5EDT", false},
		// A name outside the tz areas is often one of several alike.
		{loadLocation(t, "US/Eastern"), "US/Eastern", false},
		{loadLocation(t, "Canada/Central"), "Canada/Central", false},
		{loadLocation(t, "Australia/Sydney"), "Sydney", false},
		{loadLocation(t, "Pacific/Auckland"), "Auckland", false},
		// time.Local is named "Local" unless the TZ variable names it.
		{time.FixedZone("Local", 0), "the scheduler's", true},
		{time.FixedZone("", 3600), "the scheduler's", true},
	} {
		if got, generic := zoneName(tt.loc); got != tt.want || generic != tt.generic {
			t.Errorf("zoneName(%v) = %q, %v; want %q, %v", tt.loc, got, generic, tt.want, tt.generic)
		}
	}
}

func TestOffsetWords(t *testing.T) {
	for d, want := range map[time.Duration]string{
		5 * time.Hour:                   "5 hours",
		time.Hour:                       "1 hour",
		10*time.Hour + 30*time.Minute:   "10 hours 30 minutes",
		30 * time.Minute:                "30 minutes",
		5*time.Hour + 30*time.Minute:    "5 hours 30 minutes",
		-(5*time.Hour + 45*time.Minute): "5 hours 45 minutes",
	} {
		if got := offsetWords(d); got != want {
			t.Errorf("offsetWords(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestShiftWords(t *testing.T) {
	for _, tt := range []struct {
		d           time.Duration
		other, want string
	}{
		{-5 * time.Hour, "UTC", "5 hours behind UTC"},
		{time.Hour, "New York time", "1 hour ahead of New York time"},
		{0, "UTC", "the same as UTC"},
	} {
		if got := shiftWords(tt.d, tt.other); got != tt.want {
			t.Errorf("shiftWords(%v, %q) = %q, want %q", tt.d, tt.other, got, tt.want)
		}
	}
}

func TestSecondWords(t *testing.T) {
	for _, tt := range []struct {
		first, second time.Duration
		other, want   string
	}{
		{-5 * time.Hour, -4 * time.Hour, "UTC", "4 hours"},
		{0, time.Hour, "UTC", "1 hour ahead"},
		{0, -time.Hour, "London time", "1 hour behind"},
		{time.Hour, 0, "UTC", "the same as UTC"},
	} {
		if got := secondWords(tt.first, tt.second, tt.other); got != tt.want {
			t.Errorf("secondWords(%v, %v, %q) = %q, want %q", tt.first, tt.second, tt.other, got, tt.want)
		}
	}
}

func TestZoneYearOf(t *testing.T) {
	utc := func(m time.Month, d, h, minute int) time.Time {
		return time.Date(2027, m, d, h, minute, 0, 0, time.UTC)
	}
	for _, tt := range []struct {
		zone     string
		base     time.Duration
		shifts   []zoneShift
		seasonal bool
	}{
		{"America/New_York", -5 * time.Hour, []zoneShift{{utc(3, 14, 7, 0), -4 * time.Hour}, {utc(11, 7, 6, 0), -5 * time.Hour}}, true},
		{"Asia/Kolkata", 5*time.Hour + 30*time.Minute, nil, false},
		{"Australia/Lord_Howe", 11 * time.Hour, []zoneShift{{utc(4, 3, 15, 0), 10*time.Hour + 30*time.Minute}, {utc(10, 2, 15, 30), 11 * time.Hour}}, true},
	} {
		z := zoneYearOf(loadLocation(t, tt.zone), 2027)
		if z.base != tt.base || len(z.shifts) != len(tt.shifts) || z.seasonal() != tt.seasonal {
			t.Errorf("%s: base %v, %d shifts, seasonal %v; want %v, %d, %v", tt.zone, z.base, len(z.shifts), z.seasonal(), tt.base, len(tt.shifts), tt.seasonal)
			continue
		}
		for i, s := range tt.shifts {
			if got := z.shifts[i]; !got.at.Equal(s.at) || got.off != s.off {
				t.Errorf("%s: shift %d = %v %v, want %v %v", tt.zone, i, got.at.UTC(), got.off, s.at, s.off)
			}
		}
	}
}

// TestZoneYearOfEveryYear walks zones that change with the season in every
// year through 2050. Go can report a year-end bound that is not after the
// instant asked about (TestNextAcrossYearEnds); the walk must step past it
// rather than stop with no shifts.
func TestZoneYearOfEveryYear(t *testing.T) {
	for _, zone := range []string{"Europe/Berlin", "Australia/Lord_Howe", "Australia/Adelaide", "America/New_York"} {
		loc := loadLocation(t, zone)
		for y := 2026; y <= 2050; y++ {
			if z := zoneYearOf(loc, y); !z.seasonal() {
				t.Errorf("%s %d: %d shifts, not seasonal", zone, y, len(z.shifts))
			}
		}
	}
}

func TestUTCShift(t *testing.T) {
	for zone, want := range map[string]string{
		"America/New_York":    "5 hours behind UTC, 4 hours from March to November",
		"Europe/Berlin":       "1 hour ahead of UTC, 2 hours from March to October",
		"Europe/London":       "the same as UTC, 1 hour ahead from March to October",
		"Australia/Lord_Howe": "11 hours ahead of UTC, 10 hours 30 minutes from April to October",
		"Asia/Kolkata":        "5 hours 30 minutes ahead of UTC",
		"Etc/UTC":             "the same as UTC",
	} {
		if got, note := utcShift(zoneYearOf(loadLocation(t, zone), 2027), "X"); got != want || note != "" {
			t.Errorf("%s: utcShift = %q, note %q; want %q and no note", zone, got, note, want)
		}
	}
}

func TestJoinNote(t *testing.T) {
	for _, tt := range [][3]string{{"", "", ""}, {"a", "", "a"}, {"", "b", "b"}, {"a", "b", "a; b"}} {
		if got := joinNote(tt[0], tt[1]); got != tt[2] {
			t.Errorf("joinNote(%q, %q) = %q, want %q", tt[0], tt[1], got, tt[2])
		}
	}
}

func TestZoneSpans(t *testing.T) {
	berlin := loadLocation(t, "Europe/Berlin")
	spans := zoneSpans(zoneYearOf(berlin, 2027), zoneYearOf(loadLocation(t, "America/New_York"), 2027))
	var offs []time.Duration
	for _, s := range spans {
		offs = append(offs, s.off)
	}
	h := time.Hour
	if want := []time.Duration{6 * h, 5 * h, 6 * h, 5 * h, 6 * h}; !slices.Equal(offs, want) {
		t.Fatalf("offsets %v, want %v", offs, want)
	}
	at := func(m time.Month, d, hour int) time.Time { return time.Date(2027, m, d, hour, 0, 0, 0, berlin) }
	for _, tt := range []struct {
		i        int
		from, to time.Time
	}{
		{0, at(1, 1, 0), at(3, 14, 8)},
		{1, at(3, 14, 8), at(3, 28, 3)},
		{3, time.Date(2027, 10, 31, 1, 0, 0, 0, time.UTC), at(11, 7, 7)}, // 2 am in Berlin, the second time
		{4, at(11, 7, 7), at(1, 1, 0).AddDate(1, 0, 0)},
	} {
		if s := spans[tt.i]; !s.from.Equal(tt.from) || !s.to.Equal(tt.to) {
			t.Errorf("span %d: %v to %v, want %v to %v", tt.i, s.from.In(berlin), s.to.In(berlin), tt.from, tt.to)
		}
	}
}

// TestZoneSpansJoinsEqualOffsets: two zones that change at the same instant
// keep one span, and so read as one value all year.
func TestZoneSpansJoinsEqualOffsets(t *testing.T) {
	spans := zoneSpans(zoneYearOf(loadLocation(t, "Europe/Berlin"), 2027), zoneYearOf(loadLocation(t, "Europe/London"), 2027))
	if len(spans) != 1 || spans[0].off != time.Hour {
		t.Errorf("spans %v, want one of 1h", spans)
	}
}

func TestSteadyOf(t *testing.T) {
	year := func(name string) zoneYear { return zoneYearOf(loadLocation(t, name), 2027) }
	if got := steadyOf(zoneSpans(year("Europe/Berlin"), year("America/New_York")), true); got != 6*time.Hour {
		t.Errorf("Berlin on New York: %v, want 6h", got)
	}
	if got := steadyOf(zoneSpans(year("UTC"), year("America/Los_Angeles")), false); got != 8*time.Hour {
		t.Errorf("UTC on Los Angeles: %v, want 8h", got)
	}
	tie := []zoneSpan{{off: time.Hour, to: time.Unix(10, 0)}, {off: 2 * time.Hour, from: time.Unix(10, 0), to: time.Unix(20, 0)}}
	if got := steadyOf(tie, true); got != time.Hour {
		t.Errorf("tie: %v, want the first, 1h", got)
	}
}

// TestSchedulerDefaultIsNone checks that with no scheduler zone given a
// description states the shift from UTC only, on every host, for the package
// functions, AnalyzeSpec and the Parser methods alike.
func TestSchedulerDefaultIsNone(t *testing.T) {
	pinDescribeYear(t)
	const spec = "CRON_TZ=Europe/Berlin 0 9 * * *"
	const want = "at 9 am every day Berlin time, 1 hour ahead of UTC, 2 hours from March to October"
	p := NewParser(Minute | Hour | Dom | Month | Dow)
	for name, opts := range map[string][]DescribeOption{"default": nil, "nil": {WithSchedulerLocation(nil)}, "UTC": {schedUTC}} {
		if got, _ := DescribeSpec(spec, opts...); got != want {
			t.Errorf("%s: DescribeSpec: %q, want %q", name, got, want)
		}
		if got, _ := p.DescribeSpec(spec, opts...); got != want {
			t.Errorf("%s: Parser.DescribeSpec: %q, want %q", name, got, want)
		}
		if got := p.AnalyzeSpec(spec, opts...).Description; got != want {
			t.Errorf("%s: Parser.AnalyzeSpec: %q, want %q", name, got, want)
		}
	}
	if got := AnalyzeSpec(spec).Description; got != want {
		t.Errorf("AnalyzeSpec: %q, want %q", got, want)
	}
	const la = want + ", and 9 hours ahead of Los Angeles time"
	if got, _ := p.DescribeSpec(spec, schedLA); len(got) < len(la) || got[:len(la)] != la {
		t.Errorf("scheduler in Los Angeles: %q, want it to start %q", got, la)
	}
}

func TestWindowNote(t *testing.T) {
	year := func(name string) zoneYear { return zoneYearOf(loadLocation(t, name), 2027) }
	berlin, h := loadLocation(t, "Europe/Berlin"), time.Hour
	at := func(m time.Month, d, hour int) time.Time { return time.Date(2027, m, d, hour, 0, 0, 0, berlin) }
	const tail = ", between the two zones' clock changes"
	for _, tt := range []struct {
		name            string
		subject, object string
		spans           []zoneSpan
		steady          time.Duration
		loc             *time.Location
		tail            bool
		want            string
	}{
		{
			"one zone against UTC", "New York", "UTC", zoneSpans(year("America/New_York"), year("UTC")), -5 * h, loadLocation(t, "America/New_York"), false,
			"New York is 4 hours behind UTC from March 14 to November 7",
		},
		{
			"two windows of one value", "Berlin", "New York", zoneSpans(year("Europe/Berlin"), year("America/New_York")), 6 * h, berlin, true,
			"Berlin is 5 hours ahead of New York from March 14 to 28 and from October 31 to November 7" + tail,
		},
		{"windows under a day", "New York", "Los Angeles", zoneSpans(year("America/New_York"), year("America/Los_Angeles")), 3 * h, loadLocation(t, "America/New_York"), true, ""},
		{
			"two values", "X", "the scheduler's time",
			[]zoneSpan{
				{at(1, 1, 0), at(3, 14, 8), 6 * h},
				{at(3, 14, 8), at(3, 28, 3), 5 * h},
				{at(3, 28, 3), at(10, 31, 3), 6 * h},
				{at(10, 31, 3), at(11, 7, 7), 7 * h},
				{at(11, 7, 7), at(12, 31, 23), 6 * h},
			},
			6 * h, berlin, true,
			"X is 5 hours ahead of the scheduler's time from March 14 to 28; X is 7 hours ahead of the scheduler's time from October 31 to November 7" + tail,
		},
		{
			"three windows of one value", "X", "UTC",
			[]zoneSpan{
				{at(1, 1, 0), at(2, 15, 3), h},
				{at(2, 15, 3), at(3, 22, 3), 0},
				{at(3, 22, 3), at(3, 29, 3), h},
				{at(3, 29, 3), at(9, 20, 3), 0},
				{at(9, 20, 3), at(10, 25, 3), h},
				{at(10, 25, 3), at(12, 31, 23), 0},
			},
			h, berlin, false,
			"X is the same as UTC from February 15 to March 22, from March 29 to September 20, and from October 25 to December 31",
		},
	} {
		if got := windowNote(tt.subject, tt.object, tt.spans, tt.steady, tt.loc, tt.tail); got != tt.want {
			t.Errorf("%s:\n got %q\nwant %q", tt.name, got, tt.want)
		}
	}
}

func TestWindowWords(t *testing.T) {
	at := func(m time.Month, d, hour int) time.Time { return time.Date(2027, m, d, hour, 0, 0, 0, time.UTC) }
	for _, tt := range []struct {
		from, last time.Time
		want       string
	}{
		{at(3, 14, 2), at(3, 28, 1), "from March 14 to 28"},
		{at(10, 31, 1), at(11, 7, 1), "from October 31 to November 7"},
		{at(9, 30, 0), at(9, 30, 23), "on September 30"},
	} {
		if got := windowWords(tt.from, tt.last); got != tt.want {
			t.Errorf("windowWords(%v, %v) = %q, want %q", tt.from, tt.last, got, tt.want)
		}
	}
}

func TestBetweenChanges(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2027, 1, d, 0, 0, 0, 0, time.UTC) }
	shifts := func(days ...int) zoneYear {
		var z zoneYear
		for _, d := range days {
			z.shifts = append(z.shifts, zoneShift{at: day(d)})
		}
		return z
	}
	spec, sched := shifts(5, 20), shifts(10)
	for _, tt := range []struct {
		name  string
		spans []zoneSpan
		want  bool
	}{
		{"one zone's change to the other's", []zoneSpan{{day(5), day(10), time.Hour}}, true},
		{"the other way round", []zoneSpan{{day(10), day(20), time.Hour}}, true},
		{"two changes of one zone", []zoneSpan{{day(5), day(20), time.Hour}}, false},
		{"from the start of the year", []zoneSpan{{day(1), day(10), time.Hour}}, false},
		{"a steady span is not named", []zoneSpan{{day(1), day(5), 0}, {day(5), day(10), time.Hour}}, true},
		{"a span under a day is not named", []zoneSpan{{day(5), day(5).Add(time.Hour), time.Hour}}, true},
	} {
		if got := betweenChanges(tt.spans, 0, spec, sched); got != tt.want {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestUTCShiftNote: a zone whose year is not seasonal states its January
// offset, and the note names the rest of the year. Pyongyang moved from 8
// hours 30 minutes to 9 hours ahead of UTC on May 5, 2018, and stayed.
func TestUTCShiftNote(t *testing.T) {
	clause, note := utcShift(zoneYearOf(loadLocation(t, "Asia/Pyongyang"), 2018), "Pyongyang")
	if want := "8 hours 30 minutes ahead of UTC"; clause != want {
		t.Errorf("clause %q, want %q", clause, want)
	}
	if want := "Pyongyang is 9 hours ahead of UTC from May 5 to December 31"; note != want {
		t.Errorf("note %q, want %q", note, want)
	}
	if !describeNoteGrammar().MatchString(note) {
		t.Errorf("note %q is outside the note grammar", note)
	}
}

// TestZoneOffsetsBehindGoldens asserts, against Zone() directly, every
// month, date and offset the zoned goldens name (G1). A host with other zone
// data fails here, with the zone and date named.
func TestZoneOffsetsBehindGoldens(t *testing.T) {
	const h, m = time.Hour, time.Minute
	type day struct {
		month time.Month
		day   int
		want  time.Duration
	}
	for zone, days := range map[string][]day{
		"America/New_York":               {{3, 13, -5 * h}, {3, 14, -4 * h}, {11, 6, -4 * h}, {11, 7, -5 * h}},
		"America/Los_Angeles":            {{3, 13, -8 * h}, {3, 14, -7 * h}, {11, 7, -8 * h}},
		"America/Detroit":                {{3, 14, -4 * h}, {11, 7, -5 * h}},
		"Europe/Berlin":                  {{3, 27, h}, {3, 28, 2 * h}, {10, 30, 2 * h}, {10, 31, h}},
		"Europe/London":                  {{3, 27, 0}, {3, 28, h}, {10, 31, 0}},
		"Australia/Lord_Howe":            {{4, 3, 11 * h}, {4, 4, 10*h + 30*m}, {10, 2, 10*h + 30*m}, {10, 3, 11 * h}},
		"Asia/Kolkata":                   {{1, 1, 5*h + 30*m}, {7, 1, 5*h + 30*m}},
		"Australia/Sydney":               {{4, 3, 11 * h}, {4, 4, 10 * h}, {10, 2, 10 * h}, {10, 3, 11 * h}},
		"America/Argentina/Buenos_Aires": {{1, 1, -3 * h}, {7, 1, -3 * h}},
	} {
		for _, d := range days {
			noon := time.Date(2027, d.month, d.day, 12, 0, 0, 0, loadLocation(t, zone))
			if got := offsetOf(noon); got != d.want {
				t.Errorf("%s at noon on %s %d, 2027: offset %v, want %v", zone, d.month, d.day, got, d.want)
			}
		}
	}
}

func TestParserAnalyzeSpec(t *testing.T) {
	pinDescribeYear(t)
	const spec = "CRON_TZ=Europe/Berlin 0 0 * * *"
	got := standardParser.AnalyzeSpec(spec, schedNY)
	wantDesc := "at midnight every day Berlin time, 1 hour ahead of UTC, 2 hours from March to October, and 6 hours ahead of New York time"
	wantNote := "Berlin is 5 hours ahead of New York from March 14 to 28 and from October 31 to November 7, between the two zones' clock changes"
	if !got.Valid || got.Description != wantDesc || got.DescriptionNote != wantNote {
		t.Errorf("scheduler in New York: valid %v\n got %q, %q\nwant %q, %q", got.Valid, got.Description, got.DescriptionNote, wantDesc, wantNote)
	}
	if got.Location == nil || got.Location.String() != "Europe/Berlin" || got.NextRun.IsZero() {
		t.Errorf("location %v, next run %v", got.Location, got.NextRun)
	}
	// The parser's own settings apply, as its hash key does.
	const hashed = "H H * * *"
	opts := Minute | Hour | Dom | Month | Dow | Hash
	keyed, want := NewParser(opts).WithHashKey("job").AnalyzeSpec(hashed), AnalyzeSpecWithHash(hashed, opts, "job")
	if !keyed.Valid || keyed.Description != want.Description || !keyed.NextRun.Equal(want.NextRun) {
		t.Errorf("hash key: got %q at %v, want %q at %v", keyed.Description, keyed.NextRun, want.Description, want.NextRun)
	}
	if got := standardParser.AnalyzeSpec(""); got.Valid || !errors.Is(got.Error, ErrEmptySpec) {
		t.Errorf("empty spec: %+v", got)
	}
	if got := standardParser.AnalyzeSpec("not a spec"); got.Valid || got.Error == nil {
		t.Errorf("bad spec: %+v", got)
	}
}
