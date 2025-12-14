package cron

import (
	"strings"
	"testing"
	"time"
)

// TestValidateSpec tests the ValidateSpec function for basic validation.
func TestValidateSpec(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		options []ParseOption
		wantErr bool
		errMsg  string
	}{
		// Valid standard expressions
		{
			name:    "valid standard 5-field",
			spec:    "0 * * * *",
			wantErr: false,
		},
		{
			name:    "valid with ranges",
			spec:    "0 9-17 * * MON-FRI",
			wantErr: false,
		},
		{
			name:    "valid with step",
			spec:    "*/15 * * * *",
			wantErr: false,
		},
		{
			name:    "valid descriptor",
			spec:    "@hourly",
			wantErr: false,
		},
		{
			name:    "valid every descriptor",
			spec:    "@every 1h30m",
			wantErr: false,
		},
		{
			name:    "valid with timezone",
			spec:    "TZ=America/New_York 0 9 * * *",
			wantErr: false,
		},

		// Invalid expressions
		{
			name:    "empty spec",
			spec:    "",
			wantErr: true,
			errMsg:  "empty",
		},
		{
			name:    "too few fields",
			spec:    "* * *",
			wantErr: true,
			errMsg:  "expected",
		},
		{
			name:    "too many fields",
			spec:    "* * * * * * *",
			wantErr: true,
			errMsg:  "expected",
		},
		{
			name:    "invalid minute value",
			spec:    "60 * * * *",
			wantErr: true,
			errMsg:  "above maximum",
		},
		{
			name:    "invalid hour value",
			spec:    "0 25 * * *",
			wantErr: true,
			errMsg:  "above maximum",
		},
		{
			name:    "invalid day of month",
			spec:    "0 0 32 * *",
			wantErr: true,
			errMsg:  "above maximum",
		},
		{
			name:    "invalid month",
			spec:    "0 0 1 13 *",
			wantErr: true,
			errMsg:  "above maximum",
		},
		{
			name:    "invalid day of week",
			spec:    "0 0 * * 8",
			wantErr: true,
			errMsg:  "above maximum",
		},
		{
			name:    "invalid range",
			spec:    "5-3 * * * *",
			wantErr: true,
			errMsg:  "beyond end of range",
		},
		{
			name:    "invalid timezone",
			spec:    "TZ=Invalid/Zone 0 * * * *",
			wantErr: true,
			errMsg:  "time zone",
		},
		{
			name:    "invalid descriptor",
			spec:    "@invalid",
			wantErr: true,
			errMsg:  "unrecognized descriptor",
		},
		{
			name:    "invalid every duration",
			spec:    "@every invalid",
			wantErr: true,
			errMsg:  "parse duration",
		},
		{
			name:    "negative value",
			spec:    "-1 * * * *",
			wantErr: true,
			errMsg:  "failed to parse",
		},

		// With seconds option
		{
			name:    "valid 6-field with seconds",
			spec:    "30 0 * * * *",
			options: []ParseOption{Second | Minute | Hour | Dom | Month | Dow},
			wantErr: false,
		},
		{
			name:    "invalid seconds value",
			spec:    "60 0 * * * *",
			options: []ParseOption{Second | Minute | Hour | Dom | Month | Dow},
			wantErr: true,
			errMsg:  "above maximum",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if len(tt.options) > 0 {
				err = ValidateSpec(tt.spec, tt.options[0])
			} else {
				err = ValidateSpec(tt.spec)
			}

			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateSpec(%q) expected error containing %q, got nil", tt.spec, tt.errMsg)
				} else if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidateSpec(%q) error = %v, want error containing %q", tt.spec, err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateSpec(%q) unexpected error: %v", tt.spec, err)
				}
			}
		})
	}
}

// TestValidateSpecWithParser tests validation with custom parser configurations.
func TestValidateSpecWithParser(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		options ParseOption
		wantErr bool
	}{
		{
			name:    "seconds parser accepts 6 fields",
			spec:    "0 0 * * * *",
			options: Second | Minute | Hour | Dom | Month | Dow,
			wantErr: false,
		},
		{
			name:    "standard parser rejects 6 fields",
			spec:    "0 0 * * * *",
			options: Minute | Hour | Dom | Month | Dow,
			wantErr: true,
		},
		{
			name:    "descriptor disabled rejects @hourly",
			spec:    "@hourly",
			options: Minute | Hour | Dom | Month | Dow,
			wantErr: true,
		},
		{
			name:    "descriptor enabled accepts @hourly",
			spec:    "@hourly",
			options: Minute | Hour | Dom | Month | Dow | Descriptor,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSpec(tt.spec, tt.options)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateSpec(%q) expected error, got nil", tt.spec)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateSpec(%q) unexpected error: %v", tt.spec, err)
			}
		})
	}
}

// TestAnalyzeSpec tests the AnalyzeSpec function for detailed analysis.
func TestAnalyzeSpec(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name           string
		spec           string
		options        []ParseOption
		wantValid      bool
		wantErrMsg     string
		checkNextRun   bool
		nextRunAfter   time.Time
		nextRunBefore  time.Time
		checkFields    bool
		expectedFields map[string]string
	}{
		{
			name:         "valid hourly",
			spec:         "0 * * * *",
			wantValid:    true,
			checkNextRun: true,
			nextRunAfter: now,
			// Next run should be within the next hour
			nextRunBefore: now.Add(61 * time.Minute),
		},
		{
			name:         "valid daily at 9am",
			spec:         "0 9 * * *",
			wantValid:    true,
			checkNextRun: true,
			nextRunAfter: now,
			// Next 9am could be up to 24 hours away
			nextRunBefore: now.Add(25 * time.Hour),
		},
		{
			name:       "invalid empty",
			spec:       "",
			wantValid:  false,
			wantErrMsg: "empty",
		},
		{
			name:       "invalid syntax",
			spec:       "invalid cron",
			wantValid:  false,
			wantErrMsg: "expected",
		},
		{
			name:       "invalid range",
			spec:       "0 25 * * *",
			wantValid:  false,
			wantErrMsg: "above maximum",
		},
		{
			name:        "check fields for standard expression",
			spec:        "30 9 15 6 1",
			wantValid:   true,
			checkFields: true,
			expectedFields: map[string]string{
				"minute":       "30",
				"hour":         "9",
				"day_of_month": "15",
				"month":        "6",
				"day_of_week":  "1",
			},
		},
		{
			name:        "check fields with wildcards",
			spec:        "*/15 * * * *",
			wantValid:   true,
			checkFields: true,
			expectedFields: map[string]string{
				"minute":       "*/15",
				"hour":         "*",
				"day_of_month": "*",
				"month":        "*",
				"day_of_week":  "*",
			},
		},
		{
			name:      "descriptor @hourly",
			spec:      "@hourly",
			wantValid: true,
		},
		{
			name:      "descriptor @every",
			spec:      "@every 5m",
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result SpecAnalysis
			if len(tt.options) > 0 {
				result = AnalyzeSpec(tt.spec, tt.options[0])
			} else {
				result = AnalyzeSpec(tt.spec)
			}

			if result.Valid != tt.wantValid {
				t.Errorf("AnalyzeSpec(%q).Valid = %v, want %v", tt.spec, result.Valid, tt.wantValid)
			}

			if !tt.wantValid {
				if result.Error == nil {
					t.Errorf("AnalyzeSpec(%q) expected error, got nil", tt.spec)
				} else if tt.wantErrMsg != "" && !strings.Contains(result.Error.Error(), tt.wantErrMsg) {
					t.Errorf("AnalyzeSpec(%q).Error = %v, want containing %q", tt.spec, result.Error, tt.wantErrMsg)
				}
			}

			if tt.checkNextRun && tt.wantValid {
				if result.NextRun.IsZero() {
					t.Errorf("AnalyzeSpec(%q).NextRun is zero, expected a time", tt.spec)
				} else {
					if result.NextRun.Before(tt.nextRunAfter) {
						t.Errorf("AnalyzeSpec(%q).NextRun = %v, want after %v", tt.spec, result.NextRun, tt.nextRunAfter)
					}
					if result.NextRun.After(tt.nextRunBefore) {
						t.Errorf("AnalyzeSpec(%q).NextRun = %v, want before %v", tt.spec, result.NextRun, tt.nextRunBefore)
					}
				}
			}

			if tt.checkFields && tt.wantValid {
				for field, expectedVal := range tt.expectedFields {
					if actual, ok := result.Fields[field]; !ok {
						t.Errorf("AnalyzeSpec(%q).Fields missing key %q", tt.spec, field)
					} else if actual != expectedVal {
						t.Errorf("AnalyzeSpec(%q).Fields[%q] = %q, want %q", tt.spec, field, actual, expectedVal)
					}
				}
			}
		})
	}
}

// TestAnalyzeSpecTimezone tests timezone handling in analysis.
func TestAnalyzeSpecTimezone(t *testing.T) {
	result := AnalyzeSpec("TZ=America/New_York 0 9 * * *")
	if !result.Valid {
		t.Fatalf("AnalyzeSpec with timezone failed: %v", result.Error)
	}

	if result.Location == nil {
		t.Error("AnalyzeSpec should populate Location for timezone specs")
	} else if result.Location.String() != "America/New_York" {
		t.Errorf("AnalyzeSpec location = %q, want %q", result.Location.String(), "America/New_York")
	}
}

// TestAnalyzeSpecDescriptor tests descriptor analysis.
func TestAnalyzeSpecDescriptor(t *testing.T) {
	tests := []struct {
		name           string
		spec           string
		wantDescriptor bool
		wantInterval   time.Duration
	}{
		{
			name:           "@hourly",
			spec:           "@hourly",
			wantDescriptor: true,
		},
		{
			name:           "@daily",
			spec:           "@daily",
			wantDescriptor: true,
		},
		{
			name:           "@every 30m",
			spec:           "@every 30m",
			wantDescriptor: true,
			wantInterval:   30 * time.Minute,
		},
		{
			name:           "@every 2h15m",
			spec:           "@every 2h15m",
			wantDescriptor: true,
			wantInterval:   2*time.Hour + 15*time.Minute,
		},
		{
			name:           "standard expression",
			spec:           "0 * * * *",
			wantDescriptor: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AnalyzeSpec(tt.spec)
			if !result.Valid {
				t.Fatalf("AnalyzeSpec(%q) failed: %v", tt.spec, result.Error)
			}

			if result.IsDescriptor != tt.wantDescriptor {
				t.Errorf("AnalyzeSpec(%q).IsDescriptor = %v, want %v", tt.spec, result.IsDescriptor, tt.wantDescriptor)
			}

			if tt.wantInterval > 0 && result.Interval != tt.wantInterval {
				t.Errorf("AnalyzeSpec(%q).Interval = %v, want %v", tt.spec, result.Interval, tt.wantInterval)
			}
		})
	}
}

// TestValidateSpecPerformance ensures validation is fast.
func TestValidateSpecPerformance(t *testing.T) {
	specs := []string{
		"* * * * *",
		"0 9-17 * * MON-FRI",
		"*/15 * * * *",
		"@hourly",
		"@every 5m",
		"TZ=UTC 0 0 * * *",
	}

	start := time.Now()
	iterations := 1000

	for i := 0; i < iterations; i++ {
		for _, spec := range specs {
			_ = ValidateSpec(spec)
		}
	}

	elapsed := time.Since(start)
	avgPerValidation := elapsed / time.Duration(iterations*len(specs))

	// Validation should be very fast (under 100µs per call)
	if avgPerValidation > 100*time.Microsecond {
		t.Errorf("Validation too slow: %v per call, want < 100µs", avgPerValidation)
	}
}

// TestValidateSpecEdgeCases tests edge cases and boundary conditions.
func TestValidateSpecEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		wantErr bool
	}{
		// Boundary values
		{"minute 0", "0 * * * *", false},
		{"minute 59", "59 * * * *", false},
		{"hour 0", "0 0 * * *", false},
		{"hour 23", "0 23 * * *", false},
		{"dom 1", "0 0 1 * *", false},
		{"dom 31", "0 0 31 * *", false},
		{"month 1", "0 0 1 1 *", false},
		{"month 12", "0 0 1 12 *", false},
		{"dow 0 (Sunday)", "0 0 * * 0", false},
		{"dow 6 (Saturday)", "0 0 * * 6", false},
		{"dow 7 (Sunday alt)", "0 0 * * 7", false},

		// Named values
		{"month JAN", "0 0 1 JAN *", false},
		{"month DEC", "0 0 1 DEC *", false},
		{"dow SUN", "0 0 * * SUN", false},
		{"dow SAT", "0 0 * * SAT", false},
		{"dow MON-FRI range", "0 9 * * MON-FRI", false},

		// Question mark (any)
		{"question mark dom", "0 0 ? * *", false},
		{"question mark dow", "0 0 * * ?", false},

		// Complex expressions
		{"multiple ranges", "0 9-12,14-18 * * *", false},
		{"multiple steps", "0,15,30,45 * * * *", false},
		{"step with range", "0-30/10 * * * *", false},

		// Whitespace handling
		{"extra spaces", "0   *   *   *   *", false}, // strings.Fields handles multiple spaces
		{"tabs", "0\t*\t*\t*\t*", false},             // Tabs are valid separators

		// Long spec (should be rejected)
		{"spec too long", string(make([]byte, MaxSpecLength+1)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSpec(tt.spec)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateSpec(%q) expected error, got nil", tt.spec)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateSpec(%q) unexpected error: %v", tt.spec, err)
			}
		})
	}
}

// TestValidateSpecConcurrent tests thread safety of validation.
func TestValidateSpecConcurrent(t *testing.T) {
	specs := []string{
		"* * * * *",
		"0 9 * * *",
		"@hourly",
		"@every 5m",
		"TZ=UTC 0 0 * * *",
	}

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				for _, spec := range specs {
					_ = ValidateSpec(spec)
					_ = AnalyzeSpec(spec)
				}
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestValidateSpecs tests bulk validation of cron expressions.
func TestValidateSpecs(t *testing.T) {
	tests := []struct {
		name           string
		specs          []string
		options        []ParseOption
		wantErrIndices []int
	}{
		{
			name:           "all valid",
			specs:          []string{"* * * * *", "@hourly", "0 9 * * MON-FRI"},
			wantErrIndices: nil,
		},
		{
			name:           "all invalid",
			specs:          []string{"invalid", "bad", "wrong"},
			wantErrIndices: []int{0, 1, 2},
		},
		{
			name:           "mixed valid and invalid",
			specs:          []string{"* * * * *", "invalid", "0 9 * * MON-FRI", "bad"},
			wantErrIndices: []int{1, 3},
		},
		{
			name:           "empty slice",
			specs:          []string{},
			wantErrIndices: nil,
		},
		{
			name:           "single valid",
			specs:          []string{"@every 1h"},
			wantErrIndices: nil,
		},
		{
			name:           "single invalid",
			specs:          []string{"not-a-cron"},
			wantErrIndices: []int{0},
		},
		{
			name:           "empty spec in list",
			specs:          []string{"* * * * *", "", "0 0 * * *"},
			wantErrIndices: []int{1},
		},
		{
			name:           "invalid values",
			specs:          []string{"60 * * * *", "0 25 * * *", "0 0 32 * *"},
			wantErrIndices: []int{0, 1, 2},
		},
		{
			name:           "with seconds option - valid",
			specs:          []string{"0 * * * * *", "30 0 9 * * *"},
			options:        []ParseOption{Second | Minute | Hour | Dom | Month | Dow},
			wantErrIndices: nil,
		},
		{
			name:           "with seconds option - mixed",
			specs:          []string{"0 * * * * *", "* * * * *", "invalid"},
			options:        []ParseOption{Second | Minute | Hour | Dom | Month | Dow},
			wantErrIndices: []int{1, 2}, // 5-field fails when seconds required
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := ValidateSpecs(tt.specs, tt.options...)

			// Check we got errors at expected indices
			if len(tt.wantErrIndices) == 0 {
				if len(errors) != 0 {
					t.Errorf("expected no errors, got %d: %v", len(errors), errors)
				}
				return
			}

			if len(errors) != len(tt.wantErrIndices) {
				t.Errorf("expected %d errors, got %d: %v", len(tt.wantErrIndices), len(errors), errors)
				return
			}

			for _, idx := range tt.wantErrIndices {
				if _, ok := errors[idx]; !ok {
					t.Errorf("expected error at index %d, but not found", idx)
				}
			}

			// Verify no unexpected errors
			for idx := range errors {
				found := false
				for _, wantIdx := range tt.wantErrIndices {
					if idx == wantIdx {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("unexpected error at index %d: %v", idx, errors[idx])
				}
			}
		})
	}
}

// TestValidateSpecsReturnsEmptyMapNotNil verifies the function returns empty map, not nil.
func TestValidateSpecsReturnsEmptyMapNotNil(t *testing.T) {
	errors := ValidateSpecs([]string{"* * * * *", "@hourly"})
	if errors == nil {
		t.Error("expected empty map, got nil")
	}
	if len(errors) != 0 {
		t.Errorf("expected empty map, got %d errors", len(errors))
	}
}

// TestValidateSpecsErrorMessages verifies error messages are meaningful.
func TestValidateSpecsErrorMessages(t *testing.T) {
	specs := []string{
		"60 * * * *", // invalid minute
		"",           // empty
		"not-a-cron", // invalid format
	}

	errors := ValidateSpecs(specs)

	if len(errors) != 3 {
		t.Fatalf("expected 3 errors, got %d", len(errors))
	}

	// Check error at index 0 mentions the value issue
	if err, ok := errors[0]; ok {
		if !strings.Contains(err.Error(), "above maximum") {
			t.Errorf("expected 'above maximum' in error, got: %v", err)
		}
	}

	// Check error at index 1 mentions empty
	if err, ok := errors[1]; ok {
		if !strings.Contains(err.Error(), "empty") {
			t.Errorf("expected 'empty' in error, got: %v", err)
		}
	}
}

// TestValidateSpecsConcurrent tests concurrent use of ValidateSpecs.
func TestValidateSpecsConcurrent(t *testing.T) {
	specs := []string{"* * * * *", "invalid", "@hourly", "bad", "0 9 * * MON-FRI"}
	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				errors := ValidateSpecs(specs)
				// Should always have exactly 2 errors (indices 1 and 3)
				if len(errors) != 2 {
					t.Errorf("expected 2 errors, got %d", len(errors))
				}
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}
