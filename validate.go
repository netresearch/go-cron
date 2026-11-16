// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT.

package cron

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// SpecAnalysis contains detailed information about a parsed cron specification.
// It provides insight into the schedule without requiring job registration.
type SpecAnalysis struct {
	// Valid indicates whether the spec was successfully parsed.
	Valid bool

	// Error contains the parsing error if Valid is false.
	Error error

	// NextRun is the next scheduled execution time from now.
	// Zero if the spec is invalid or represents a one-time past event.
	NextRun time.Time

	// Location is the timezone for the schedule.
	// Defaults to time.Local unless TZ= or CRON_TZ= is specified.
	Location *time.Location

	// Fields contains the original field values from the spec.
	// Keys: "second" (if applicable), "minute", "hour", "day_of_month", "month", "day_of_week"
	// For descriptors, this will be empty.
	Fields map[string]string

	// IsDescriptor indicates if the spec uses a descriptor (@hourly, @every, etc.)
	IsDescriptor bool

	// Interval is the duration for @every expressions.
	// Zero for non-@every specs.
	Interval time.Duration

	// Schedule is the parsed schedule, available for further introspection.
	// Nil if the spec is invalid.
	Schedule Schedule

	// Description is the spec in English, as DescribeSpec gives it.
	// Empty if the spec is invalid.
	Description string

	// DescriptionNote spells out the values the hash key picks, as
	// DescribeSpecNote gives it, and the windows in which a written zone is
	// off the shift the description states. Empty unless a field has a
	// hashed step or a hashed day of the month, or the zone has a window.
	DescriptionNote string

	// Warnings contains non-fatal warnings about the schedule.
	// These don't prevent parsing but may indicate unexpected behavior.
	// Example: "DOM and DOW both restricted - using AND logic (use DowOrDom for OR)"
	Warnings []string
}

// ValidateSpec validates a cron expression without scheduling a job.
// It returns nil if the spec is valid, or an error describing the problem.
//
// By default, it uses the standard parser (5-field cron + descriptors).
// Pass a ParseOption to customize validation (e.g., to require seconds field).
//
// Example:
//
//	// Validate user input
//	if err := cron.ValidateSpec(userInput); err != nil {
//	    return fmt.Errorf("invalid cron expression: %w", err)
//	}
//
//	// Validate with seconds field
//	if err := cron.ValidateSpec(userInput, cron.Second|cron.Minute|cron.Hour|cron.Dom|cron.Month|cron.Dow); err != nil {
//	    // Handle error
//	}
func ValidateSpec(spec string, options ...ParseOption) error {
	parser := getParserForOptions(options)
	_, err := parser.Parse(spec)
	return err
}

// ValidateSpecWith validates a cron expression using any ScheduleParser implementation.
// This is useful when you have a custom parser or a pre-configured Parser instance.
//
// Example:
//
//	// Validate with a custom parser instance
//	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Hash).
//		WithHashKey("my-job")
//	if err := cron.ValidateSpecWith("H * * * * *", parser); err != nil {
//	    // Handle error
//	}
func ValidateSpecWith(spec string, parser ScheduleParser) error {
	if parser == nil {
		return errors.New("cron: parser is nil")
	}
	_, err := parser.Parse(spec)
	return err
}

// ValidateSpecs validates multiple cron expressions at once.
// It returns a map of index to error for any invalid specs.
// If all specs are valid, returns an empty map (not nil).
//
// This is useful for:
//   - Validating configuration files before deployment
//   - Bulk validation with detailed error reporting
//   - Pre-flight checks before registering multiple jobs
//
// Example:
//
//	specs := []string{"* * * * *", "invalid", "0 9 * * MON-FRI", "bad"}
//	errors := cron.ValidateSpecs(specs)
//	if len(errors) > 0 {
//	    for idx, err := range errors {
//	        log.Printf("Spec %d is invalid: %v", idx, err)
//	    }
//	}
//
//	// For all-or-nothing validation:
//	if len(errors) > 0 {
//	    return fmt.Errorf("invalid specs: %v", errors)
//	}
//	// Now safe to add all specs
//	for _, spec := range specs {
//	    c.AddFunc(spec, handler)
//	}
func ValidateSpecs(specs []string, options ...ParseOption) map[int]error {
	errs := make(map[int]error)
	parser := getParserForOptions(options)

	for i, spec := range specs {
		if _, err := parser.Parse(spec); err != nil {
			errs[i] = err
		}
	}

	return errs
}

// AnalyzeSpec provides detailed analysis of a cron expression.
// It returns a SpecAnalysis struct containing validation status,
// next run time, parsed fields, and other metadata.
//
// This is useful for:
//   - Configuration validation with detailed feedback
//   - UI previews showing when a job will run
//   - Debugging cron expressions
//   - Import/migration validation
//
// Example:
//
//	result := cron.AnalyzeSpec("0 9 * * MON-FRI")
//	if !result.Valid {
//	    log.Printf("Invalid: %v", result.Error)
//	} else {
//	    log.Printf("Next run: %v", result.NextRun)
//	    log.Printf("Fields: %v", result.Fields)
//	}
func AnalyzeSpec(spec string, options ...ParseOption) SpecAnalysis {
	if spec == "" { // before the options, which may be invalid
		return standardParser.AnalyzeSpec(spec)
	}
	return getParserForOptions(options).AnalyzeSpec(spec)
}

// AnalyzeSpec analyzes spec as p parses it, with p's own settings, as its
// hash key; see the package function AnalyzeSpec. WithSchedulerLocation
// names the zone that Description gives a written zone's shift from; callers
// in a running scheduler pass c.Location(). Without it Description gives the
// shift from UTC only.
// WithParseOptions is ignored.
func (p Parser) AnalyzeSpec(spec string, opts ...DescribeOption) SpecAnalysis {
	result := SpecAnalysis{
		Fields: make(map[string]string),
	}

	// Handle empty spec early
	if len(spec) == 0 {
		result.Error = ErrEmptySpec
		return result
	}

	// Parse the spec
	schedule, err := p.Parse(spec)
	if err != nil {
		result.Error = err
		return result
	}

	result.Valid = true
	result.Schedule = schedule
	result.Description, result.DescriptionNote, _ = p.describe(spec, newDescribeConfig(opts).sched)

	// Extract location and analyze the spec
	result.analyzeSpec(spec, p)

	// Check for DOM/DOW AND logic warning
	result.checkDomDowWarning()

	// Calculate next run time
	result.NextRun = schedule.Next(time.Now())

	return result
}

// AnalyzeSpecWithHash analyzes a cron expression containing H hash expressions.
// This is like AnalyzeSpec but takes a hash seed for resolving H expressions.
// The seed should be a unique identifier (like a job name) that produces
// deterministic, distributed scheduling times.
//
// Example:
//
//	result := AnalyzeSpecWithHash("H H * * *", Minute|Hour|Dom|Month|Dow|Hash, "my-job")
//	if result.Valid {
//	    log.Printf("Next run: %v", result.NextRun)
//	}
func AnalyzeSpecWithHash(spec string, options ParseOption, hashSeed string) SpecAnalysis {
	if spec == "" { // before the options, which may be invalid
		return standardParser.AnalyzeSpec(spec)
	}
	return NewParser(options).WithHashKey(hashSeed).AnalyzeSpec(spec)
}

// checkDomDowWarning adds a warning if both DOM and DOW are restricted.
// This helps users understand that AND logic is used (both must match).
func (r *SpecAnalysis) checkDomDowWarning() {
	if r.Schedule == nil {
		return
	}

	spec, ok := r.Schedule.(*SpecSchedule)
	if !ok {
		return
	}

	// Check if both DOM and DOW are restricted (neither has starBit)
	// and DowOrDom is not enabled (meaning AND logic is in effect)
	if spec.Dom&starBit == 0 && spec.Dow&starBit == 0 && !spec.DowOrDom {
		r.Warnings = append(r.Warnings,
			"both day-of-month and day-of-week are restricted - using AND logic "+
				"(both must match); use DowOrDom parser option for legacy OR behavior")
	}
}

// analyzeSpec extracts metadata from the spec string.
func (r *SpecAnalysis) analyzeSpec(spec string, p Parser) {
	// Extract timezone
	loc, remaining, _ := parseTimezone(spec)
	r.Location = loc

	// Check for descriptors
	if strings.HasPrefix(remaining, "@") {
		r.IsDescriptor = true
		r.analyzeDescriptor()
		return
	}

	// Parse fields for non-descriptor specs
	r.parseFields(remaining, p)
}

// analyzeDescriptor extracts information from descriptor-style specs. The
// interval comes from the parsed schedule, so it is the rounded value the
// scheduler uses (90.4s reports as 1m30s).
func (r *SpecAnalysis) analyzeDescriptor() {
	switch s := r.Schedule.(type) {
	case AlignedSchedule:
		r.Interval = s.Interval
	case ConstantDelaySchedule:
		r.Interval = s.Delay
	}
}

// parseFields names the fields of spec as p parses them: the year as p's
// options split it off (handleYearExtraction), then six fields from the
// second or five from the minute.
func (r *SpecAnalysis) parseFields(spec string, p Parser) {
	fields, year, _ := p.handleYearExtraction(strings.Fields(spec))
	if len(fields) == 6 {
		r.Fields["second"] = fields[0]
		fields = fields[1:]
	}
	if len(fields) != 5 {
		return
	}
	r.Fields["minute"] = fields[0]
	r.Fields["hour"] = fields[1]
	r.Fields["day_of_month"] = fields[2]
	r.Fields["month"] = fields[3]
	r.Fields["day_of_week"] = fields[4]
	if year != "" {
		r.Fields["year"] = year
	}
}

// looksLikeYear returns true if the field appears to be a year value or year expression.
// A field "looks like a year" if it contains a number >= 100, which distinguishes it
// from seconds (0-59), minutes (0-59), hours (0-23), days (1-31), months (1-12), dow (0-7).
func looksLikeYear(field string) bool {
	// Wildcard is ambiguous - could be any field
	if field == "*" || field == "?" {
		return false
	}
	// Check if any numeric part is >= 100 (clearly a year, not another cron field)
	for _, part := range strings.FieldsFunc(field, func(r rune) bool {
		return r == ',' || r == '-' || r == '/'
	}) {
		if year, err := strconv.Atoi(part); err == nil && year >= 100 {
			return true
		}
	}
	return false
}

// getParserForOptions returns a parser configured with the given options.
// If no options provided, returns the standard parser with descriptors enabled.
func getParserForOptions(options []ParseOption) Parser {
	p, err := tryParserForOptions(options)
	if err != nil {
		panic(err)
	}
	return p
}

// tryParserForOptions is getParserForOptions with invalid options returned
// as an error.
func tryParserForOptions(options []ParseOption) (Parser, error) {
	if len(options) == 0 {
		return standardParser, nil
	}

	// Use the provided options, ensuring Descriptor is included for common use
	opts := options[0]
	if opts&Descriptor == 0 {
		// Check if caller explicitly configured fields without Descriptor
		// If so, respect their choice. Otherwise, add it for convenience.
		hasFields := opts&(Second|Minute|Hour|Dom|Month|Dow) != 0
		if !hasFields {
			opts |= Descriptor
		}
	}

	return TryNewParser(opts)
}

// emptySpecMessage is the package-shared error text used when a spec string
// is empty. It backs both ErrEmptySpec here and the parser.parse() error.
const emptySpecMessage = "empty spec string"

// ErrEmptySpec is returned when an empty spec string is provided.
var ErrEmptySpec = &ValidationError{Message: emptySpecMessage}

// ValidationError represents a cron expression validation error.
type ValidationError struct {
	Message string
	Field   string // Optional: which field caused the error
	Value   string // Optional: the invalid value
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return e.Message + " in " + e.Field + ": " + e.Value
	}
	return e.Message
}
