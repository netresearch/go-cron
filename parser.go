package cron

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// ParseOption represents configuration options for creating a parser.
// Most options specify which fields should be included, while others enable features.
// If a field is not included the parser will assume a default value.
// These options do not change the order fields are parsed in.
type ParseOption int

// ParseOption constants define which fields are included in parsing.
const (
	Second         ParseOption = 1 << iota // Seconds field, default 0
	SecondOptional                         // Optional seconds field, default 0
	Minute                                 // Minutes field, default 0
	Hour                                   // Hours field, default 0
	Dom                                    // Day of month field, default *
	Month                                  // Month field, default *
	Dow                                    // Day of week field, default *
	DowOptional                            // Optional day of week field, default *
	Descriptor                             // Allow descriptors such as @monthly, @weekly, etc.
)

var places = []ParseOption{
	Second,
	Minute,
	Hour,
	Dom,
	Month,
	Dow,
}

var defaults = []string{
	"0",
	"0",
	"0",
	"*",
	"*",
	"*",
}

// Parser is a custom cron expression parser that can be configured.
type Parser struct {
	options          ParseOption
	minEveryInterval time.Duration
	maxSearchYears   int
}

// ErrNoFields is returned when no fields or Descriptor are configured.
var ErrNoFields = fmt.Errorf("at least one field or Descriptor must be configured")

// ErrMultipleOptionals is returned when more than one optional field is configured.
var ErrMultipleOptionals = fmt.Errorf("multiple optionals may not be configured")

// TryNewParser creates a Parser with custom options, returning an error if the
// configuration is invalid. This is the safe alternative to NewParser for cases
// where parser options come from runtime configuration rather than hardcoded values.
//
// Use TryNewParser when:
//   - Parser options come from config files, environment variables, or user input
//   - You want to handle configuration errors gracefully
//
// Use NewParser when:
//   - Parser options are hardcoded constants (invalid config = bug)
//   - You want to fail fast during initialization
//
// Returns ErrNoFields if no fields or Descriptor are configured.
// Returns ErrMultipleOptionals if more than one optional field is configured.
//
// Example:
//
//	// Safe parsing from config
//	opts := loadParserOptionsFromConfig()
//	parser, err := TryNewParser(opts)
//	if err != nil {
//	    return fmt.Errorf("invalid parser config: %w", err)
//	}
func TryNewParser(options ParseOption) (Parser, error) {
	// Count how many regular fields are configured
	fields := 0
	for _, place := range places {
		if options&place > 0 {
			fields++
		}
	}
	if fields == 0 && options&Descriptor == 0 {
		return Parser{}, ErrNoFields
	}

	optionals := 0
	if options&DowOptional > 0 {
		optionals++
	}
	if options&SecondOptional > 0 {
		optionals++
	}
	if optionals > 1 {
		return Parser{}, ErrMultipleOptionals
	}
	return Parser{
		options:          options,
		minEveryInterval: time.Second, // default minimum interval for @every
	}, nil
}

// NewParser creates a Parser with custom options.
//
// It panics if more than one Optional is given, since it would be impossible to
// correctly infer which optional is provided or missing in general.
//
// For runtime configuration where errors should be handled gracefully,
// use TryNewParser instead.
//
// Examples
//
//	// Standard parser without descriptors
//	specParser := NewParser(Minute | Hour | Dom | Month | Dow)
//	sched, err := specParser.Parse("0 0 15 */3 *")
//
//	// Same as above, just excludes time fields
//	specParser := NewParser(Dom | Month | Dow)
//	sched, err := specParser.Parse("15 */3 *")
//
//	// Same as above, just makes Dow optional
//	specParser := NewParser(Dom | Month | DowOptional)
//	sched, err := specParser.Parse("15 */3")
func NewParser(options ParseOption) Parser {
	p, err := TryNewParser(options)
	if err != nil {
		panic(err)
	}
	return p
}

// MustNewParser is like TryNewParser but panics if the options are invalid.
// This follows the Go convention of Must* functions for cases where failure
// indicates a programming error rather than a runtime condition.
//
// Use MustNewParser when:
//   - Parser options are hardcoded constants
//   - Invalid configuration is a bug that should fail fast
//
// Use TryNewParser when:
//   - Parser options come from config files, environment, or user input
//   - You want to handle configuration errors gracefully
//
// Note: In v2.0, NewParser will return (Parser, error) and MustNewParser
// will be the only panicking variant. Using MustNewParser now ensures
// forward compatibility with v2.0.
//
// Example:
//
//	// Panics if options are invalid (hardcoded, so invalid = bug)
//	var parser = cron.MustNewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
func MustNewParser(options ParseOption) Parser {
	p, err := TryNewParser(options)
	if err != nil {
		panic(err)
	}
	return p
}

// WithMinEveryInterval returns a new Parser with the specified minimum interval
// for @every expressions. This allows overriding the default 1-second minimum.
//
// Use 0 or negative values to disable the minimum check entirely.
// Use values larger than 1 second to enforce longer minimum intervals.
//
// Example:
//
//	// Allow sub-second intervals (for testing)
//	p := NewParser(Minute | Hour | Dom | Month | Dow | Descriptor).
//	    WithMinEveryInterval(100 * time.Millisecond)
//
//	// Enforce minimum 1-minute intervals (for rate limiting)
//	p := NewParser(Minute | Hour | Dom | Month | Dow | Descriptor).
//	    WithMinEveryInterval(time.Minute)
func (p Parser) WithMinEveryInterval(d time.Duration) Parser {
	p.minEveryInterval = d
	return p
}

// WithMaxSearchYears returns a new Parser with the specified maximum search years
// for finding the next schedule time. This limits how far into the future the
// Next() method will search before giving up and returning zero time.
//
// The default is 5 years. Values <= 0 will use the default.
//
// Use cases:
//   - Shorter limits for faster failure detection on invalid schedules
//   - Longer limits for rare schedules (e.g., "Friday the 13th in February")
//   - Testing scenarios that need predictable behavior
//
// Example:
//
//	// Allow searching up to 10 years for rare schedules
//	p := NewParser(Minute | Hour | Dom | Month | Dow | Descriptor).
//	    WithMaxSearchYears(10)
//
//	// Fail faster on invalid schedules (1 year max)
//	p := NewParser(Minute | Hour | Dom | Month | Dow | Descriptor).
//	    WithMaxSearchYears(1)
func (p Parser) WithMaxSearchYears(years int) Parser {
	p.maxSearchYears = years
	return p
}

// MaxSpecLength is the maximum allowed length for a cron spec string.
// This limit prevents potential resource exhaustion from extremely long inputs.
const MaxSpecLength = 1024

// parseTimezone extracts and validates the timezone from a spec string.
// Returns the location, remaining spec string, and any error.
func parseTimezone(spec string) (*time.Location, string, error) {
	if !strings.HasPrefix(spec, "TZ=") && !strings.HasPrefix(spec, "CRON_TZ=") {
		return time.Local, spec, nil
	}

	i := strings.Index(spec, " ")
	if i == -1 {
		return nil, "", fmt.Errorf("missing fields after timezone in spec %q", spec)
	}

	eq := strings.Index(spec, "=")
	tzName := spec[eq+1 : i]

	if err := validateTimezone(tzName); err != nil {
		return nil, "", fmt.Errorf("invalid timezone %q: %w", tzName, err)
	}

	loc, err := time.LoadLocation(tzName)
	if err != nil {
		return nil, "", fmt.Errorf("unknown time zone %q: %w", tzName, err)
	}

	remaining := strings.TrimSpace(spec[i:])
	if len(remaining) == 0 {
		return nil, "", fmt.Errorf("missing fields after timezone %q", tzName)
	}

	return loc, remaining, nil
}

// Parse returns a new crontab schedule representing the given spec.
// It returns a descriptive error if the spec is not valid.
// It accepts crontab specs and features configured by NewParser.
func (p Parser) Parse(spec string) (Schedule, error) {
	if len(spec) == 0 {
		return nil, fmt.Errorf("empty spec string")
	}
	if len(spec) > MaxSpecLength {
		return nil, fmt.Errorf("spec too long: %d > %d", len(spec), MaxSpecLength)
	}

	loc, spec, err := parseTimezone(spec)
	if err != nil {
		return nil, err
	}

	// Handle named schedules (descriptors), if configured
	if strings.HasPrefix(spec, "@") {
		if p.options&Descriptor == 0 {
			return nil, fmt.Errorf("parser does not accept descriptors: %q", spec)
		}
		return parseDescriptor(spec, loc, p.minEveryInterval, p.maxSearchYears)
	}

	// Split on whitespace.
	fields := strings.Fields(spec)

	// Validate & fill in any omitted or optional fields
	fields, err = normalizeFields(fields, p.options)
	if err != nil {
		return nil, err
	}

	field := func(field string, r bounds) uint64 {
		if err != nil {
			return 0
		}
		var bits uint64
		bits, err = getField(field, r)
		return bits
	}

	var (
		second     = field(fields[0], seconds)
		minute     = field(fields[1], minutes)
		hour       = field(fields[2], hours)
		dayofmonth = field(fields[3], dom)
		month      = field(fields[4], months)
		dayofweek  = field(fields[5], dow)
	)
	if err != nil {
		return nil, err
	}

	return &SpecSchedule{
		Second:         second,
		Minute:         minute,
		Hour:           hour,
		Dom:            dayofmonth,
		Month:          month,
		Dow:            dayofweek,
		Location:       loc,
		MaxSearchYears: p.maxSearchYears,
	}, nil
}

// normalizeFields takes a subset set of the time fields and returns the full set
// with defaults (zeroes) populated for unset fields.
//
// As part of performing this function, it also validates that the provided
// fields are compatible with the configured options.

// processOptionalFlags validates and processes optional field flags.
// Returns updated options with optional fields enabled, count of optionals, and any error.
func processOptionalFlags(options ParseOption) (ParseOption, int, error) {
	optionals := 0
	if options&SecondOptional > 0 {
		options |= Second
		optionals++
	}
	if options&DowOptional > 0 {
		options |= Dow
		optionals++
	}
	if optionals > 1 {
		return 0, 0, fmt.Errorf("multiple optionals may not be configured")
	}
	return options, optionals, nil
}

// countConfiguredFields returns the number of fields configured in options.
func countConfiguredFields(options ParseOption) int {
	count := 0
	for _, place := range places {
		if options&place > 0 {
			count++
		}
	}
	return count
}

func normalizeFields(fields []string, options ParseOption) ([]string, error) {
	options, optionals, err := processOptionalFlags(options)
	if err != nil {
		return nil, err
	}

	maxFields := countConfiguredFields(options)
	minFields := maxFields - optionals

	// Validate number of fields
	if count := len(fields); count < minFields || count > maxFields {
		if minFields == maxFields {
			return nil, fmt.Errorf("expected exactly %d fields, found %d: %s", minFields, count, fields)
		}
		return nil, fmt.Errorf("expected %d to %d fields, found %d: %s", minFields, maxFields, count, fields)
	}

	// Populate the optional field if not provided
	if minFields < maxFields && len(fields) == minFields {
		switch {
		case options&DowOptional > 0:
			fields = append(fields, defaults[5])
		case options&SecondOptional > 0:
			fields = append([]string{defaults[0]}, fields...)
		default:
			return nil, fmt.Errorf("unknown optional field")
		}
	}

	// Populate all fields not part of options with their defaults
	n := 0
	expandedFields := make([]string, len(places))
	copy(expandedFields, defaults)
	for i, place := range places {
		if options&place > 0 {
			expandedFields[i] = fields[n]
			n++
		}
	}
	return expandedFields, nil
}

var standardParser = NewParser(
	Minute | Hour | Dom | Month | Dow | Descriptor,
)

// StandardParser returns a copy of the standard parser used by ParseStandard.
// This can be used as a base for creating custom parsers with modified settings.
//
// Example:
//
//	// Create parser allowing sub-second @every intervals
//	p := StandardParser().WithMinEveryInterval(0)
//	c := cron.New(cron.WithParser(p))
func StandardParser() Parser {
	return standardParser
}

// ParseStandard returns a new crontab schedule representing the given
// standardSpec (https://en.wikipedia.org/wiki/Cron). It requires 5 entries
// representing: minute, hour, day of month, month and day of week, in that
// order. It returns a descriptive error if the spec is not valid.
//
// It accepts
//   - Standard crontab specs, e.g. "* * * * ?"
//   - Descriptors, e.g. "@midnight", "@every 1h30m"
func ParseStandard(standardSpec string) (Schedule, error) {
	return standardParser.Parse(standardSpec)
}

// getField returns an Int with the bits set representing all of the times that
// the field represents or error parsing field value.  A "field" is a comma-separated
// list of "ranges".
func getField(field string, r bounds) (uint64, error) {
	var bits uint64
	ranges := strings.FieldsFunc(field, func(r rune) bool { return r == ',' })
	for _, expr := range ranges {
		bit, err := getRange(expr, r)
		if err != nil {
			return bits, err
		}
		bits |= bit
	}
	return bits, nil
}

// getRange returns the bits indicated by the given expression:
//
//	number | number "-" number [ "/" number ]
//
// or error parsing range.

// parseRangeBounds parses the start/end bounds from a cron range expression.
// Returns start, end values, extra bits (starBit if wildcard), and any error.
func parseRangeBounds(lowAndHigh []string, r bounds) (start, end uint, extra uint64, err error) {
	if lowAndHigh[0] == "*" || lowAndHigh[0] == "?" {
		return r.min, r.max, starBit, nil
	}

	start, err = parseIntOrName(lowAndHigh[0], r.names)
	if err != nil {
		return 0, 0, 0, err
	}

	switch len(lowAndHigh) {
	case 1:
		return start, start, 0, nil
	case 2:
		end, err = parseIntOrName(lowAndHigh[1], r.names)
		if err != nil {
			return 0, 0, 0, err
		}
		return start, end, 0, nil
	default:
		return 0, 0, 0, fmt.Errorf("too many hyphens: %q", strings.Join(lowAndHigh, "-"))
	}
}

// validateRangeParams validates the parsed range parameters.
func validateRangeParams(start, end, step uint, r bounds, expr string) error {
	if start < r.min {
		return fmt.Errorf("beginning of range (%d) below minimum (%d): %q", start, r.min, expr)
	}
	if end > r.max {
		return fmt.Errorf("end of range (%d) above maximum (%d): %q", end, r.max, expr)
	}
	if start > end {
		return fmt.Errorf("beginning of range (%d) beyond end of range (%d): %q", start, end, expr)
	}
	if step == 0 {
		return fmt.Errorf("step of range must be a positive number: %q", expr)
	}
	if step > 1 && step >= end-start+1 {
		return fmt.Errorf("step (%d) must be less than range size (%d): %q", step, end-start+1, expr)
	}
	return nil
}

func getRange(expr string, r bounds) (uint64, error) {
	rangeAndStep := strings.Split(expr, "/")
	lowAndHigh := strings.Split(rangeAndStep[0], "-")
	singleDigit := len(lowAndHigh) == 1

	start, end, extra, err := parseRangeBounds(lowAndHigh, r)
	if err != nil {
		return 0, err
	}

	var step uint
	switch len(rangeAndStep) {
	case 1:
		step = 1
	case 2:
		step, err = mustParseInt(rangeAndStep[1])
		if err != nil {
			return 0, err
		}
		// Special handling: "N/step" means "N-max/step".
		if singleDigit {
			end = r.max
		}
		if step > 1 {
			extra = 0
		}
	default:
		return 0, fmt.Errorf("too many slashes: %q", expr)
	}

	if err := validateRangeParams(start, end, step, r, expr); err != nil {
		return 0, err
	}

	return getBits(start, end, step) | extra, nil
}

// parseIntOrName returns the (possibly-named) integer contained in expr.
func parseIntOrName(expr string, names map[string]uint) (uint, error) {
	if names != nil {
		if namedInt, ok := names[strings.ToLower(expr)]; ok {
			return namedInt, nil
		}
	}
	return mustParseInt(expr)
}

// mustParseInt parses the given expression as an int or returns an error.

// validateTimezone checks if the timezone string is safe to pass to time.LoadLocation.
// It enforces length limits and character restrictions to prevent DoS attacks via
// crafted timezone strings.

// isValidTimezoneChar returns true if r is a valid character in a timezone name.
// Valid chars: letters, digits, slash, underscore, hyphen, plus, colon.
func isValidTimezoneChar(r rune) bool {
	// Valid chars: A-Z, a-z, 0-9, /, _, -, +, :
	if r >= 'A' && r <= 'Z' {
		return true
	}
	if r >= 'a' && r <= 'z' {
		return true
	}
	if r >= '0' && r <= '9' {
		return true
	}
	return r == '/' || r == '_' || r == '-' || r == '+' || r == ':'
}

func validateTimezone(tz string) error {
	const maxTimezoneLen = 64 // IANA timezone names are well under this limit
	if len(tz) == 0 {
		return fmt.Errorf("empty timezone string")
	}
	if len(tz) > maxTimezoneLen {
		return fmt.Errorf("timezone string too long (max %d chars)", maxTimezoneLen)
	}
	for i, r := range tz {
		if !isValidTimezoneChar(r) {
			return fmt.Errorf("invalid character %q at position %d in timezone", r, i)
		}
	}
	return nil
}

func mustParseInt(expr string) (uint, error) {
	num, err := strconv.Atoi(expr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse int from %q: %w", expr, err)
	}
	if num < 0 {
		return 0, fmt.Errorf("negative number (%d) not allowed: %q", num, expr)
	}

	return uint(num), nil
}

// getBits sets all bits in the range [low, high], modulo the given step size.
func getBits(low, high, step uint) uint64 {
	var bits uint64

	// If step is 1, use shifts.
	if step == 1 {
		return ^(math.MaxUint64 << (high + 1)) & (math.MaxUint64 << low)
	}

	// Else, use a simple loop.
	for i := low; i <= high; i += step {
		bits |= 1 << i
	}
	return bits
}

// all returns all bits within the given bounds.  (plus the star bit)
func all(r bounds) uint64 {
	return getBits(r.min, r.max, 1) | starBit
}

// parseDescriptor returns a predefined schedule for the expression, or error if none matches.

// newDescriptorSchedule creates a SpecSchedule for descriptor-based schedules.
// Second and Minute are always set to first value (0). Hour, Dom, Month, Dow vary.
func newDescriptorSchedule(hour, dom, month, dow uint64, loc *time.Location, maxSearchYears int) *SpecSchedule {
	return &SpecSchedule{
		Second:         1 << seconds.min,
		Minute:         1 << minutes.min,
		Hour:           hour,
		Dom:            dom,
		Month:          month,
		Dow:            dow,
		Location:       loc,
		MaxSearchYears: maxSearchYears,
	}
}

func parseDescriptor(descriptor string, loc *time.Location, minEveryInterval time.Duration, maxSearchYears int) (Schedule, error) {
	switch descriptor {
	case "@yearly", "@annually":
		return newDescriptorSchedule(1<<hours.min, 1<<dom.min, 1<<months.min, all(dow), loc, maxSearchYears), nil
	case "@monthly":
		return newDescriptorSchedule(1<<hours.min, 1<<dom.min, all(months), all(dow), loc, maxSearchYears), nil
	case "@weekly":
		return newDescriptorSchedule(1<<hours.min, all(dom), all(months), 1<<dow.min, loc, maxSearchYears), nil
	case "@daily", "@midnight":
		return newDescriptorSchedule(1<<hours.min, all(dom), all(months), all(dow), loc, maxSearchYears), nil
	case "@hourly":
		return newDescriptorSchedule(all(hours), all(dom), all(months), all(dow), loc, maxSearchYears), nil
	}

	const every = "@every "
	if strings.HasPrefix(descriptor, every) {
		duration, err := time.ParseDuration(descriptor[len(every):])
		if err != nil {
			return nil, fmt.Errorf("failed to parse duration %q: %w", descriptor, err)
		}
		if minEveryInterval > 0 && duration < minEveryInterval {
			return nil, fmt.Errorf("@every duration must be at least %v: %q", minEveryInterval, descriptor)
		}
		return EveryWithMin(duration, minEveryInterval), nil
	}

	return nil, fmt.Errorf("unrecognized descriptor: %q", descriptor)
}
