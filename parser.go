package cron

import (
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"strconv"
	"strings"
	"sync"
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
	Year                                   // Year field, default * (any year)
	YearOptional                           // Optional year field, auto-detected by value >= 100
	Hash                                   // Allow Jenkins-style 'H' hash expressions for load distribution
	DowNth                                 // Allow #n syntax in DOW (e.g., FRI#3 for 3rd Friday)
	DowLast                                // Allow #L syntax in DOW (e.g., FRI#L for last Friday)
	DomL                                   // Allow L syntax in DOM (e.g., L for last day, L-3 for 3rd last day)
	DomW                                   // Allow W syntax in DOM (e.g., 15W for nearest weekday, LW for last weekday)
	DowOrDom                               // Use legacy OR logic for DOW/DOM (default: AND)
)

// Extended is a convenience flag that enables all extended cron syntax options:
// DowNth, DowLast, DomL, and DomW. This provides Quartz/Jenkins-style extensions.
//
// Example:
//
//	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor | cron.Extended)
//	// Now supports: FRI#3 (3rd Friday), MON#L (last Monday), L (last day), 15W (nearest weekday)
const Extended = DowNth | DowLast | DomL | DomW

var places = []ParseOption{
	Second,
	Minute,
	Hour,
	Dom,
	Month,
	Dow,
	// Note: Year is NOT in places/defaults because it's handled separately
	// in the parse() function with special offset-based bitmask logic.
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
	cache            *sync.Map // optional cache: spec string -> cacheEntry
	hashKey          string    // key used for H (hash) expressions
}

// cacheEntry holds a cached parse result.
type cacheEntry struct {
	schedule Schedule
	err      error
}

// ErrNoFields is returned when no fields or Descriptor are configured.
var ErrNoFields = errors.New("at least one field or Descriptor must be configured")

// ErrMultipleOptionals is returned when more than one optional field is configured.
var ErrMultipleOptionals = errors.New("multiple optionals may not be configured")

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
// Deprecated: NewParser will change to return (Parser, error) in v2.0.
// Use [MustNewParser] for panic-on-error behavior (forward compatible),
// or [TryNewParser] for explicit error handling.
//
// It panics if more than one Optional is given, since it would be impossible to
// correctly infer which optional is provided or missing in general.
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

// WithCache returns a new Parser with caching enabled for parsed schedules.
// When caching is enabled, repeated calls to Parse with the same spec string
// will return the cached result instead of re-parsing.
//
// Caching is particularly beneficial when:
//   - The same cron expressions are parsed repeatedly
//   - Multiple cron instances share the same parser
//   - Configuration is reloaded frequently
//
// The cache is thread-safe and grows unbounded. For applications with many
// unique spec strings, consider using a single shared parser instance.
//
// Example:
//
//	// Create a caching parser for improved performance
//	p := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor).
//	    WithCache()
//
//	// Subsequent parses of the same spec return cached results
//	sched1, _ := p.Parse("0 * * * *") // parsed
//	sched2, _ := p.Parse("0 * * * *") // cached (same reference)
func (p Parser) WithCache() Parser {
	p.cache = &sync.Map{}
	return p
}

// WithSecondOptional returns a new Parser configured to accept an optional seconds
// field as the first field. This allows the parser to accept both 5-field (standard)
// and 6-field (with seconds) expressions.
//
// When 5 fields are provided, the seconds field defaults to 0.
// When 6 fields are provided, the first field is interpreted as seconds.
//
// This method enables composable parser configuration when you need both
// SecondOptional and other parser customizations (like WithMinEveryInterval or
// WithMaxSearchYears).
//
// Example:
//
//	// Parser accepting optional seconds with custom minimum @every interval
//	p := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor).
//	    WithSecondOptional().
//	    WithMinEveryInterval(100 * time.Millisecond)
//
//	// Both expressions are valid:
//	sched1, _ := p.Parse("* * * * *")       // 5 fields, seconds=0
//	sched2, _ := p.Parse("30 * * * * *")    // 6 fields, seconds=30
func (p Parser) WithSecondOptional() Parser {
	p.options |= SecondOptional | Minute | Hour | Dom | Month | Dow
	return p
}

// WithHashKey returns a new Parser configured with a default hash key for
// Jenkins-style 'H' expressions. The hash key is used to deterministically
// distribute execution times across the allowed range.
//
// When a hash key is set, the Parse method can handle H expressions without
// requiring ParseWithHashKey to be called explicitly.
//
// Example:
//
//	// Parser with default hash key for all H expressions
//	p := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Hash).
//	    WithHashKey("my-service")
//
//	// H resolves based on "my-service" hash
//	sched, _ := p.Parse("H * * * *")
func (p Parser) WithHashKey(key string) Parser {
	p.hashKey = key
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
//
// If caching is enabled via WithCache(), repeated calls with the same spec
// will return the cached result.
func (p Parser) Parse(spec string) (Schedule, error) {
	// Check cache first if enabled
	if p.cache != nil {
		if cached, ok := p.cache.Load(spec); ok {
			if entry, ok := cached.(cacheEntry); ok {
				return entry.schedule, entry.err
			}
		}
	}

	schedule, err := p.parse(spec)

	// Store in cache if enabled
	if p.cache != nil {
		p.cache.Store(spec, cacheEntry{schedule: schedule, err: err})
	}

	return schedule, err
}

// ParseWithHashKey returns a new crontab schedule using the specified hash key
// for Jenkins-style 'H' expressions. The hash key is used to deterministically
// compute the offset for H fields, allowing different jobs to be distributed
// across the time range.
//
// This method must be used when the spec contains 'H' expressions and no
// default hash key was set via WithHashKey().
//
// Example:
//
//	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Hash)
//	// Each job runs at a different minute based on its name
//	sched1, _ := parser.ParseWithHashKey("H * * * *", "job-a")
//	sched2, _ := parser.ParseWithHashKey("H * * * *", "job-b")
func (p Parser) ParseWithHashKey(spec, hashKey string) (Schedule, error) {
	// Create a copy with the hash key set
	p.hashKey = hashKey
	return p.Parse(spec)
}

// parse is the internal parsing logic, called by Parse.
func (p Parser) parse(spec string) (Schedule, error) {
	if len(spec) == 0 {
		return nil, errors.New("empty spec string")
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

	// Extract year field if Year or YearOptional option is enabled (it's always the last field)
	fields, yearField, err := p.handleYearExtraction(fields)
	if err != nil {
		return nil, err
	}

	// Validate & fill in any omitted or optional fields (excluding Year)
	// Use options without Year/YearOptional flags for normalizeFields since Year is handled separately
	normalizeOptions := p.options &^ Year &^ YearOptional
	fields, err = normalizeFields(fields, normalizeOptions)
	if err != nil {
		return nil, err
	}

	// Check if any field contains H expression
	hashEnabled := p.options&Hash != 0
	hasHashExpr := false
	for _, f := range fields {
		if strings.Contains(f, "H") {
			hasHashExpr = true
			break
		}
	}

	// Validate hash requirements
	if hasHashExpr {
		if !hashEnabled {
			return nil, errors.New("h expressions require hash option to be enabled")
		}
		if p.hashKey == "" {
			return nil, errors.New("h expressions require a hash key: use ParseWithHashKey or WithHashKey")
		}
	}

	field := func(fieldExpr string, r bounds) uint64 {
		if err != nil {
			return 0
		}
		var bits uint64
		bits, err = getFieldWithHash(fieldExpr, r, p.hashKey, hashEnabled)
		return bits
	}

	var (
		second = field(fields[0], seconds)
		minute = field(fields[1], minutes)
		hour   = field(fields[2], hours)
		month  = field(fields[4], months)
	)

	// Check for errors from basic field parsing before proceeding
	if err != nil {
		return nil, err
	}

	// Parse DOM field with potential L/W constraints
	dayofmonth, domConstraints, domErr := p.parseDomField(fields[3], hashEnabled)
	if domErr != nil {
		return nil, domErr
	}

	// Parse DOW field with potential #n/#L constraints
	dayofweek, dowConstraints, dowErr := p.parseDowField(fields[5], hashEnabled)
	if dowErr != nil {
		return nil, dowErr
	}

	// Parse year field if Year or YearOptional option is enabled
	var yearSet map[int]struct{} // nil = wildcard (any year)
	if (p.options&Year > 0 || p.options&YearOptional > 0) && yearField != "" {
		var yearErr error
		yearSet, yearErr = getYearField(yearField)
		if yearErr != nil {
			err = yearErr
		}
	}
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
		Year:           yearSet,
		Location:       loc,
		MaxSearchYears: p.maxSearchYears,
		DomConstraints: domConstraints,
		DowConstraints: dowConstraints,
		DowOrDom:       p.options&DowOrDom > 0,
	}, nil
}

// parseDomField parses the day-of-month field, handling extended L/W syntax if enabled.
func (p Parser) parseDomField(fieldStr string, hashEnabled bool) (uint64, []DomConstraint, error) {
	allowL := p.options&DomL != 0
	allowW := p.options&DomW != 0
	fieldUpper := strings.ToUpper(fieldStr)
	hasSpecial := strings.Contains(fieldUpper, "L") || strings.Contains(fieldUpper, "W")

	switch {
	case (allowL || allowW) && hasSpecial:
		return getDomFieldWithConstraints(fieldStr, dom, allowL, allowW, p.hashKey, hashEnabled)
	case hasSpecial:
		return 0, nil, errors.New("extended day-of-month syntax requires DomL option (for L, L-n) or DomW option (for nW, LW) to be enabled")
	default:
		bits, err := getFieldWithHash(fieldStr, dom, p.hashKey, hashEnabled)
		return bits, nil, err
	}
}

// parseDowField parses the day-of-week field, handling extended #n/#L syntax if enabled.
func (p Parser) parseDowField(fieldStr string, hashEnabled bool) (uint64, []DowConstraint, error) {
	allowNth := p.options&DowNth != 0
	allowLast := p.options&DowLast != 0
	hasHash := strings.Contains(fieldStr, "#")

	switch {
	case (allowNth || allowLast) && hasHash:
		bits, constraints, err := getDowFieldWithConstraints(fieldStr, dow, allowNth, allowLast, p.hashKey, hashEnabled)
		return NormalizeDOW(bits), constraints, err
	case hasHash:
		return 0, nil, errors.New("#n/#L syntax requires DowNth or DowLast option to be enabled")
	default:
		bits, err := getFieldWithHash(fieldStr, dow, p.hashKey, hashEnabled)
		return NormalizeDOW(bits), nil, err
	}
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
		return 0, 0, errors.New("multiple optionals may not be configured")
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
			return nil, errors.New("unknown optional field")
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

// extractYearField extracts the year field from the fields slice when Year option is enabled.
// Returns the remaining fields (without year), the year field string, and any error.
// Handles SecondOptional + Year ambiguity by preferring seconds over year constraint.
func extractYearField(fields []string, options ParseOption) (remainingFields []string, yearField string, err error) {
	nonYearOptions := options &^ Year

	// Process optional flags to convert SecondOptional->Second, DowOptional->Dow
	// This ensures countConfiguredFields counts them correctly
	processedOptions, optionals, err := processOptionalFlags(nonYearOptions)
	if err != nil {
		return nil, "", err
	}
	maxNonYearFields := countConfiguredFields(processedOptions)
	minNonYearFields := maxNonYearFields - optionals

	// Year field is required when Year option is set
	// Total fields = non-year fields + 1 (for year)
	if len(fields) < minNonYearFields+1 || len(fields) > maxNonYearFields+1 {
		return nil, "", fmt.Errorf("expected %d to %d fields with year, found %d: %s",
			minNonYearFields+1, maxNonYearFields+1, len(fields), fields)
	}

	// Handle SecondOptional + Year ambiguity
	if nonYearOptions&SecondOptional > 0 && len(fields) == minNonYearFields+1 {
		return extractYearFieldAmbiguous(fields)
	}

	// Unambiguous case: extract year (last field)
	yearField = fields[len(fields)-1]
	return fields[:len(fields)-1], yearField, nil
}

// extractYearFieldAmbiguous handles the case where SecondOptional + Year are both enabled
// and we have an ambiguous field count. Strategy: prefer seconds over year constraint.
// If last field looks like a year (>= 100), treat it as year; otherwise treat as seconds.
func extractYearFieldAmbiguous(fields []string) (remainingFields []string, yearField string, err error) {
	lastField := fields[len(fields)-1]
	if !looksLikeYear(lastField) {
		// Treat as [sec min hour dom month dow] with no year constraint
		return fields, "*", nil // wildcard year, keep all fields
	}
	// Last field looks like a year, extract it
	return fields[:len(fields)-1], lastField, nil
}

// handleYearExtraction routes to the appropriate year extraction logic based on parser options.
func (p Parser) handleYearExtraction(fields []string) (remainingFields []string, yearField string, err error) {
	if p.options&Year > 0 {
		return extractYearField(fields, p.options)
	}
	if p.options&YearOptional > 0 {
		return extractOptionalYearField(fields, p.options)
	}
	return fields, "", nil
}

// extractOptionalYearField handles the YearOptional case where the year field is auto-detected.
// The year is only extracted if the last field looks like a year (contains a value >= 100).
// This allows both standard 5-field cron and 6-field cron with year to be parsed.
func extractOptionalYearField(fields []string, options ParseOption) (remainingFields []string, yearField string, err error) {
	// Remove YearOptional from options for field counting
	nonYearOptions := options &^ YearOptional

	// Process optional flags to get field count range
	processedOptions, optionals, err := processOptionalFlags(nonYearOptions)
	if err != nil {
		return nil, "", err
	}
	maxNonYearFields := countConfiguredFields(processedOptions)
	minNonYearFields := maxNonYearFields - optionals

	// Accept expressions with or without year field
	// minNonYearFields to maxNonYearFields+1 (the +1 is for optional year)
	if len(fields) < minNonYearFields || len(fields) > maxNonYearFields+1 {
		return nil, "", fmt.Errorf("expected %d to %d fields, found %d: %s",
			minNonYearFields, maxNonYearFields+1, len(fields), fields)
	}

	// If we have more fields than max non-year fields, the last one must be year
	if len(fields) > maxNonYearFields {
		lastField := fields[len(fields)-1]
		if !looksLikeYear(lastField) {
			return nil, "", fmt.Errorf("last field %q does not appear to be a year (expected value >= 100)", lastField)
		}
		return fields[:len(fields)-1], lastField, nil
	}

	// We have minNonYearFields to maxNonYearFields
	// Check if last field looks like a year
	if len(fields) > 0 {
		lastField := fields[len(fields)-1]
		if looksLikeYear(lastField) && len(fields) > minNonYearFields {
			// Last field looks like year and we have enough fields to spare one
			return fields[:len(fields)-1], lastField, nil
		}
	}

	// No year field detected, use wildcard
	return fields, "*", nil
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

// fullParser is a pre-configured parser that accepts all cron syntax variants.
var fullParser = NewParser(
	SecondOptional | Minute | Hour | Dom | Month | Dow |
		YearOptional | Descriptor | Extended | Hash,
)

// FullParser returns a parser that accepts all cron syntax variants including
// optional seconds, year field, descriptors, hash expressions, and extended
// day-of-month/day-of-week syntax.
//
// This parser supports:
//   - Standard 5-field cron: minute, hour, day-of-month, month, day-of-week
//   - Optional seconds prefix (6 fields): second, minute, hour, dom, month, dow
//   - Optional year suffix (7 fields with seconds, 6 without)
//   - Descriptors: @yearly, @monthly, @weekly, @daily, @hourly, @every <duration>
//   - Hash expressions: H for load-distributed scheduling
//   - Extended syntax: FRI#3 (3rd Friday), MON#L (last Monday), L (last day), 15W (nearest weekday)
//
// Example:
//
//	c := cron.New(cron.WithParser(cron.FullParser()))
//	c.AddFunc("0 30 14 25 12 2025", myFunc) // Run at 14:30 on Dec 25, 2025
//	c.AddFunc("0 0 0 1 1 * 2030", myFunc)   // Run at midnight on Jan 1, 2030
func FullParser() Parser {
	return fullParser
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

// getYearField parses a year field and returns a set of valid years.
// Returns nil for wildcards (* or ?), meaning any year is valid.
// Supports single years, ranges (2024-2030), steps (2020-2030/2), and lists (2025,2030,2050).
func getYearField(field string) (map[int]struct{}, error) {
	if field == "*" || field == "?" {
		return nil, nil // nil = wildcard (any year)
	}

	yearSet := make(map[int]struct{})
	ranges := strings.FieldsFunc(field, func(r rune) bool { return r == ',' })
	for _, expr := range ranges {
		years, err := getYearRange(expr)
		if err != nil {
			return nil, err
		}
		for _, y := range years {
			yearSet[y] = struct{}{}
		}
	}
	return yearSet, nil
}

// computeHash returns a deterministic hash value from a key.
// Uses FNV-1a which provides good distribution for string keys.
func computeHash(key string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key)) // hash.Hash.Write never returns an error
	return h.Sum64()
}

// getFieldWithHash returns an Int with the bits set representing all of the times that
// the field represents or error parsing field value. It handles H (hash) expressions
// when hashKey is provided.
func getFieldWithHash(field string, r bounds, hashKey string, hashEnabled bool) (uint64, error) {
	var bits uint64
	ranges := strings.FieldsFunc(field, func(r rune) bool { return r == ',' })
	for _, expr := range ranges {
		bit, err := getRangeWithHash(expr, r, hashKey, hashEnabled)
		if err != nil {
			return bits, err
		}
		bits |= bit
	}
	return bits, nil
}

// getYearRange parses a single year range expression and returns a slice of years.
// Supports single years, ranges (2024-2030), and steps (2020-2030/2).
func getYearRange(expr string) ([]int, error) {
	rangeAndStep := strings.Split(expr, "/")
	lowAndHigh := strings.Split(rangeAndStep[0], "-")

	start, err := mustParseInt(lowAndHigh[0])
	if err != nil {
		return nil, err
	}

	var end uint
	switch len(lowAndHigh) {
	case 1:
		end = start
	case 2:
		end, err = mustParseInt(lowAndHigh[1])
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("too many hyphens: %q", expr)
	}

	var step uint = 1
	if len(rangeAndStep) == 2 {
		step, err = mustParseInt(rangeAndStep[1])
		if err != nil {
			return nil, err
		}
	} else if len(rangeAndStep) > 2 {
		return nil, fmt.Errorf("too many slashes: %q", expr)
	}

	// Validate year bounds
	if start < years.min {
		return nil, fmt.Errorf("year (%d) below minimum (%d): %q", start, years.min, expr)
	}
	if end > years.max {
		return nil, fmt.Errorf("year (%d) above maximum (%d): %q", end, years.max, expr)
	}
	if start > end {
		return nil, fmt.Errorf("beginning of range (%d) beyond end of range (%d): %q", start, end, expr)
	}
	if step == 0 {
		return nil, fmt.Errorf("step of range must be a positive number: %q", expr)
	}

	// Generate list of years.
	// Conversion is safe: we validated end <= years.max (MaxInt32) above.
	var result []int
	for y := start; y <= end; y += step {
		result = append(result, int(y)) // #nosec G115 -- bounds checked against MaxInt32
	}
	return result, nil
}

// getDowFieldWithConstraints parses a DOW field that may contain #n/#L expressions.
// It splits the field by comma and for each part:
//   - If it contains #, parses it as a DowConstraint (nth occurrence or last)
//   - Otherwise, parses it as a normal bitmask expression
//
// Returns the combined bitmask for normal expressions, the list of constraints,
// and any parsing error.
//
// Examples:
//   - "FRI#3" → constraint for 3rd Friday
//   - "FRI#L" → constraint for last Friday
//   - "MON,FRI#3,SUN#L" → Monday bitmask + 3rd Friday constraint + last Sunday constraint
func getDowFieldWithConstraints(field string, r bounds, allowNth, allowLast bool, hashKey string, hashEnabled bool) (uint64, []DowConstraint, error) {
	var bits uint64
	var constraints []DowConstraint

	ranges := strings.FieldsFunc(field, func(r rune) bool { return r == ',' })
	for _, expr := range ranges {
		if idx := strings.Index(expr, "#"); idx != -1 {
			// Parse as #n or #L constraint
			constraint, err := parseDowConstraint(expr, idx, r, allowNth, allowLast)
			if err != nil {
				return 0, nil, err
			}
			constraints = append(constraints, constraint)
		} else {
			// Parse as normal bitmask expression
			bit, err := getRangeWithHash(expr, r, hashKey, hashEnabled)
			if err != nil {
				return 0, nil, err
			}
			bits |= bit
		}
	}

	return bits, constraints, nil
}

// parseDowConstraint parses a single #n or #L expression (e.g., "FRI#3" or "5#L").
// idx is the position of the '#' character in expr.
func parseDowConstraint(expr string, idx int, r bounds, allowNth, allowLast bool) (DowConstraint, error) {
	weekdayPart := expr[:idx]
	nthPart := expr[idx+1:]

	if len(weekdayPart) == 0 {
		return DowConstraint{}, fmt.Errorf("missing weekday before '#' in %q", expr)
	}
	if len(nthPart) == 0 {
		return DowConstraint{}, fmt.Errorf("missing occurrence after '#' in %q", expr)
	}

	// Parse weekday (either numeric 0-6 or name like FRI)
	weekday, err := parseIntOrName(weekdayPart, r.names)
	if err != nil {
		return DowConstraint{}, fmt.Errorf("invalid weekday in %q: %w", expr, err)
	}
	if weekday < r.min || weekday > r.max {
		return DowConstraint{}, fmt.Errorf("weekday %d out of range [%d,%d] in %q", weekday, r.min, r.max, expr)
	}

	// Parse occurrence (L for last, or 1-5 for nth)
	var n int
	switch {
	case strings.EqualFold(nthPart, "L"):
		if !allowLast {
			return DowConstraint{}, fmt.Errorf("#L syntax requires DowLast option: %q", expr)
		}
		n = -1 // -1 indicates "last occurrence"
	case !allowNth:
		return DowConstraint{}, fmt.Errorf("#n syntax requires DowNth option: %q", expr)
	default:
		nth, err := mustParseInt(nthPart)
		if err != nil {
			return DowConstraint{}, fmt.Errorf("invalid occurrence number in %q: %w", expr, err)
		}
		if nth < 1 || nth > 5 {
			return DowConstraint{}, fmt.Errorf("occurrence must be 1-5, got %d in %q", nth, expr)
		}
		n = int(nth)
	}

	// Normalize weekday 7 (Sunday as 7) to 0 (Sunday as 0) for consistency with time.Weekday()
	if weekday == 7 {
		weekday = 0
	}
	// #nosec G115 -- weekday is validated to be in range [0,7] and normalized to [0,6]
	return DowConstraint{Weekday: int(weekday), N: n}, nil
}

// getDomFieldWithConstraints parses a DOM field that may contain L, L-n, nW, or LW expressions.
// It splits the field by comma and for each part:
//   - If it's "L" or "L-n", parses it as a DomConstraint (last day or offset from last)
//   - If it's "LW", parses it as a DomConstraint (last weekday of month)
//   - If it's "nW", parses it as a DomConstraint (nearest weekday to nth)
//   - Otherwise, parses it as a normal bitmask expression
//
// Returns the combined bitmask for normal expressions, the list of constraints,
// and any parsing error.
//
// Examples:
//   - "L" → constraint for last day of month
//   - "L-3" → constraint for 3rd from last day
//   - "15W" → constraint for nearest weekday to 15th
//   - "LW" → constraint for last weekday of month
//   - "1,15,L" → 1st and 15th bitmask + last day constraint
func getDomFieldWithConstraints(field string, r bounds, allowL, allowW bool, hashKey string, hashEnabled bool) (uint64, []DomConstraint, error) {
	var bits uint64
	var constraints []DomConstraint

	ranges := strings.FieldsFunc(field, func(r rune) bool { return r == ',' })
	for _, expr := range ranges {
		exprUpper := strings.ToUpper(expr)

		// Route expression to appropriate handler
		switch {
		case strings.HasPrefix(exprUpper, "L"):
			// L expressions: L, L-n, LW
			if !allowL && !allowW {
				return 0, nil, fmt.Errorf("extended syntax L requires DomL or DomW option: %q", expr)
			}
			constraint, err := parseDomLConstraint(expr, exprUpper, allowL, allowW)
			if err != nil {
				return 0, nil, err
			}
			constraints = append(constraints, constraint)
		case allowW && strings.HasSuffix(exprUpper, "W"):
			// nW expression (e.g., 15W)
			constraint, err := parseDomWConstraint(expr, exprUpper)
			if err != nil {
				return 0, nil, err
			}
			constraints = append(constraints, constraint)
		case strings.Contains(exprUpper, "W"):
			// Field contains W but not in a valid position or option not enabled
			return 0, nil, fmt.Errorf("extended syntax W requires DomW option: %q", expr)
		default:
			// Parse as normal bitmask expression
			bit, err := getRangeWithHash(expr, r, hashKey, hashEnabled)
			if err != nil {
				return 0, nil, err
			}
			bits |= bit
		}
	}

	return bits, constraints, nil
}

// parseDomLConstraint parses L, L-n, or LW expressions.
func parseDomLConstraint(expr, exprUpper string, allowL, allowW bool) (DomConstraint, error) {
	switch {
	case exprUpper == "L":
		if !allowL {
			return DomConstraint{}, fmt.Errorf("syntax L requires DomL option: %q", expr)
		}
		return DomConstraint{Type: DomLast}, nil
	case exprUpper == "LW":
		if !allowW {
			return DomConstraint{}, fmt.Errorf("syntax LW requires DomW option: %q", expr)
		}
		return DomConstraint{Type: DomLastWeekday}, nil
	case strings.HasPrefix(exprUpper, "L-"):
		if !allowL {
			return DomConstraint{}, fmt.Errorf("syntax L-n requires DomL option: %q", expr)
		}
		offsetStr := expr[2:] // Skip "L-"
		offset, err := mustParseInt(offsetStr)
		if err != nil {
			return DomConstraint{}, fmt.Errorf("invalid offset in %q: %w", expr, err)
		}
		if offset < 1 || offset > 30 {
			return DomConstraint{}, fmt.Errorf("offset must be 1-30, got %d in %q", offset, expr)
		}
		return DomConstraint{Type: DomLastOffset, N: int(offset)}, nil
	default:
		return DomConstraint{}, fmt.Errorf("invalid L expression: %q", expr)
	}
}

// parseDomWConstraint parses nW expression (nearest weekday to nth day).
func parseDomWConstraint(expr, exprUpper string) (DomConstraint, error) {
	// Remove trailing W
	dayStr := exprUpper[:len(exprUpper)-1]
	day, err := mustParseInt(dayStr)
	if err != nil {
		return DomConstraint{}, fmt.Errorf("invalid day in %q: %w", expr, err)
	}
	if day < 1 || day > 31 {
		return DomConstraint{}, fmt.Errorf("day must be 1-31, got %d in %q", day, expr)
	}
	return DomConstraint{Type: DomNearestWeekday, N: int(day)}, nil
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
// Uses r.wraparound to determine if start > end is allowed for cyclic fields.
func validateRangeParams(start, end, step uint, r bounds, expr string) error {
	if start < r.min {
		return fmt.Errorf("beginning of range (%d) below minimum (%d): %q", start, r.min, expr)
	}
	if end > r.max {
		return fmt.Errorf("end of range (%d) above maximum (%d): %q", end, r.max, expr)
	}
	if start > end && !r.wraparound {
		return fmt.Errorf("beginning of range (%d) beyond end of range (%d): %q", start, end, expr)
	}
	if step == 0 {
		return fmt.Errorf("step of range must be a positive number: %q", expr)
	}
	// Calculate effective range size (accounting for wraparound)
	var rangeSize uint
	if start > end {
		// Wraparound: [start..max] + [min..end]
		rangeSize = (r.max - start + 1) + (end - r.min + 1)
	} else {
		rangeSize = end - start + 1
	}
	if step > 1 && step >= rangeSize {
		return fmt.Errorf("step (%d) must be less than range size (%d): %q", step, rangeSize, expr)
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

	// Handle wraparound ranges (e.g., 22-2 for hours)
	if start > end && r.wraparound {
		return getWraparoundBits(start, end, step, r) | extra, nil
	}

	return getBits(start, end, step) | extra, nil
}

// getRangeWithHash handles H (hash) expressions in addition to standard range expressions.
// H expressions use a deterministic hash of the hashKey to select a value within the range.
//
// Supported formats:
//   - H: hash within field bounds
//   - H/N: every N steps starting from hash offset
//   - H(min-max): hash within explicit range
//   - H(min-max)/N: every N steps within explicit range starting from hash offset
func getRangeWithHash(expr string, r bounds, hashKey string, hashEnabled bool) (uint64, error) {
	// Check for H expression
	if !strings.HasPrefix(expr, "H") {
		return getRange(expr, r)
	}

	if !hashEnabled {
		return 0, errors.New("h expressions require hash option to be enabled")
	}

	// Parse H expression: H, H/N, H(min-max), H(min-max)/N
	rangeAndStep := strings.Split(expr, "/")
	hashPart := rangeAndStep[0]

	// Parse range bounds from H or H(min-max)
	var start, end uint
	switch {
	case hashPart == "H":
		// Use field bounds
		start, end = r.min, r.max
	case strings.HasPrefix(hashPart, "H(") && strings.HasSuffix(hashPart, ")"):
		// Parse explicit range H(min-max)
		rangeSpec := hashPart[2 : len(hashPart)-1]
		rangeParts := strings.Split(rangeSpec, "-")
		if len(rangeParts) != 2 {
			return 0, fmt.Errorf("invalid H range syntax: %q", expr)
		}
		var err error
		start, err = mustParseInt(rangeParts[0])
		if err != nil {
			return 0, fmt.Errorf("invalid range start in %q: %w", expr, err)
		}
		end, err = mustParseInt(rangeParts[1])
		if err != nil {
			return 0, fmt.Errorf("invalid range end in %q: %w", expr, err)
		}
	default:
		return 0, fmt.Errorf("invalid H expression syntax: %q", expr)
	}

	// Validate bounds
	if start < r.min {
		return 0, fmt.Errorf("range start (%d) below minimum (%d): %q", start, r.min, expr)
	}
	if end > r.max {
		return 0, fmt.Errorf("range end (%d) above maximum (%d): %q", end, r.max, expr)
	}
	if start > end && !r.wraparound {
		return 0, fmt.Errorf("range start (%d) beyond end (%d): %q", start, end, expr)
	}

	// Parse step if present
	var step uint = 1
	if len(rangeAndStep) == 2 {
		var err error
		step, err = mustParseInt(rangeAndStep[1])
		if err != nil {
			return 0, fmt.Errorf("invalid step in %q: %w", expr, err)
		}
		if step == 0 {
			return 0, fmt.Errorf("step must be positive: %q", expr)
		}
	} else if len(rangeAndStep) > 2 {
		return 0, fmt.Errorf("too many slashes: %q", expr)
	}

	// Calculate effective range size (accounting for wraparound)
	var rangeSize uint
	if start > end {
		// Wraparound: [start..max] + [min..end]
		rangeSize = (r.max - start + 1) + (end - r.min + 1)
	} else {
		rangeSize = end - start + 1
	}

	// Compute hash-based offset
	hashValue := computeHash(hashKey)

	if step > 1 {
		return getHashStepBits(start, end, step, r, hashValue)
	}

	// Simple H: hash selects a single value within the range
	return getHashSingleBit(start, end, rangeSize, r, hashValue)
}

// getHashStepBits generates bits for H/step expression with optional wraparound.
func getHashStepBits(start, end, step uint, r bounds, hashValue uint64) (uint64, error) {
	stepOffset := uint(hashValue % uint64(step))
	firstValue := start + stepOffset

	// Handle wraparound case
	if start > end {
		var bits uint64
		fieldSize := r.max - r.min + 1
		extendedEnd := end + fieldSize
		for v := firstValue; v <= extendedEnd; v += step {
			val := v
			if val > r.max {
				val = (val-r.min)%fieldSize + r.min
			}
			bits |= 1 << val
		}
		return bits, nil
	}

	// Normal case: generate values from firstValue to end with step
	var bits uint64
	for v := firstValue; v <= end; v += step {
		bits |= 1 << v
	}
	return bits, nil
}

// getHashSingleBit generates a single bit for simple H expression with optional wraparound.
func getHashSingleBit(start, end, rangeSize uint, r bounds, hashValue uint64) (uint64, error) {
	hashOffset := uint(hashValue % uint64(rangeSize))

	// Handle wraparound case
	if start > end {
		fieldSize := r.max - r.min + 1
		val := start + hashOffset
		if val > r.max {
			val = (val-r.min)%fieldSize + r.min
		}
		return 1 << val, nil
	}

	return 1 << (start + hashOffset), nil
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
		return errors.New("empty timezone string")
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

// getWraparoundBits generates bits for a wraparound range where start > end.
// For example, 22-2 with bounds 0-23 generates bits for [22,23,0,1,2].
//
// Algorithm (based on Quartz scheduler):
//  1. Extend end by rangeSize to create virtual contiguous range
//  2. Iterate from start to extendedEnd with step
//  3. Normalize values back to valid range using modulo
func getWraparoundBits(start, end, step uint, r bounds) uint64 {
	var bits uint64
	rangeSize := r.max - r.min + 1

	// Extend end to create virtual contiguous range
	extendedEnd := end + rangeSize

	for i := start; i <= extendedEnd; i += step {
		val := i
		if val > r.max {
			// Normalize: (val - min) % rangeSize + min
			val = (val-r.min)%rangeSize + r.min
		}
		bits |= 1 << val
	}
	return bits
}

// all returns all bits within the given bounds (plus the star bit).
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
		Year:           nil, // nil = any year (wildcard)
		Location:       loc,
		MaxSearchYears: maxSearchYears,
	}
}

func parseDescriptor(descriptor string, loc *time.Location, minEveryInterval time.Duration, maxSearchYears int) (Schedule, error) {
	// Normalize DOW bits so that both Sunday=0 and Sunday=7 are handled consistently
	allDow := NormalizeDOW(all(dow))
	switch descriptor {
	case "@yearly", "@annually":
		return newDescriptorSchedule(1<<hours.min, 1<<dom.min, 1<<months.min, allDow, loc, maxSearchYears), nil
	case "@monthly":
		return newDescriptorSchedule(1<<hours.min, 1<<dom.min, all(months), allDow, loc, maxSearchYears), nil
	case "@weekly":
		return newDescriptorSchedule(1<<hours.min, all(dom), all(months), 1<<dow.min, loc, maxSearchYears), nil
	case "@daily", "@midnight":
		return newDescriptorSchedule(1<<hours.min, all(dom), all(months), allDow, loc, maxSearchYears), nil
	case "@hourly":
		return newDescriptorSchedule(all(hours), all(dom), all(months), allDow, loc, maxSearchYears), nil
	case "@triggered", "@manual", "@none":
		return TriggeredSchedule{}, nil
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
