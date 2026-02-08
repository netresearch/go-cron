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
	result := SpecAnalysis{
		Fields: make(map[string]string),
	}

	// Handle empty spec early
	if len(spec) == 0 {
		result.Error = ErrEmptySpec
		return result
	}

	parser := getParserForOptions(options)

	// Parse the spec
	schedule, err := parser.Parse(spec)
	if err != nil {
		result.Error = err
		return result
	}

	result.Valid = true
	result.Schedule = schedule

	// Extract location and analyze the spec
	result.analyzeSpec(spec)

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
	result := SpecAnalysis{
		Fields: make(map[string]string),
	}

	if len(spec) == 0 {
		result.Error = ErrEmptySpec
		return result
	}

	parser := NewParser(options).WithHashKey(hashSeed)

	schedule, err := parser.Parse(spec)
	if err != nil {
		result.Error = err
		return result
	}

	result.Valid = true
	result.Schedule = schedule

	result.analyzeSpec(spec)
	result.checkDomDowWarning()
	result.NextRun = schedule.Next(time.Now())

	return result
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
func (r *SpecAnalysis) analyzeSpec(spec string) {
	// Extract timezone
	loc, remaining, _ := parseTimezone(spec)
	r.Location = loc

	// Check for descriptors
	if strings.HasPrefix(remaining, "@") {
		r.IsDescriptor = true
		r.analyzeDescriptor(remaining)
		return
	}

	// Parse fields for non-descriptor specs
	r.parseFields(remaining)
}

// analyzeDescriptor extracts information from descriptor-style specs.
func (r *SpecAnalysis) analyzeDescriptor(descriptor string) {
	const every = "@every "
	if strings.HasPrefix(descriptor, every) {
		durationStr := descriptor[len(every):]
		if duration, err := time.ParseDuration(durationStr); err == nil {
			r.Interval = duration
		}
	}
}

// parseFields extracts individual field values from a standard cron expression.
func (r *SpecAnalysis) parseFields(spec string) {
	fields := strings.Fields(spec)

	// Map fields based on count
	// 5 fields: minute hour dom month dow
	// 6 fields: second minute hour dom month dow (or minute hour dom month dow year)
	// 7 fields: second minute hour dom month dow year
	switch len(fields) {
	case 5:
		r.Fields["minute"] = fields[0]
		r.Fields["hour"] = fields[1]
		r.Fields["day_of_month"] = fields[2]
		r.Fields["month"] = fields[3]
		r.Fields["day_of_week"] = fields[4]
	case 6:
		// Could be either with seconds or with year (without seconds)
		// We detect year by checking if the last field looks like a year (4 digits, 1970-2099 range)
		if looksLikeYear(fields[5]) {
			r.Fields["minute"] = fields[0]
			r.Fields["hour"] = fields[1]
			r.Fields["day_of_month"] = fields[2]
			r.Fields["month"] = fields[3]
			r.Fields["day_of_week"] = fields[4]
			r.Fields["year"] = fields[5]
		} else {
			r.Fields["second"] = fields[0]
			r.Fields["minute"] = fields[1]
			r.Fields["hour"] = fields[2]
			r.Fields["day_of_month"] = fields[3]
			r.Fields["month"] = fields[4]
			r.Fields["day_of_week"] = fields[5]
		}
	case 7:
		r.Fields["second"] = fields[0]
		r.Fields["minute"] = fields[1]
		r.Fields["hour"] = fields[2]
		r.Fields["day_of_month"] = fields[3]
		r.Fields["month"] = fields[4]
		r.Fields["day_of_week"] = fields[5]
		r.Fields["year"] = fields[6]
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
	if len(options) == 0 {
		return standardParser
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

	return NewParser(opts)
}

// ErrEmptySpec is returned when an empty spec string is provided.
var ErrEmptySpec = &ValidationError{Message: "empty spec string"}

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
