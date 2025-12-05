package cron

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestSlogLoggerInfo(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	sl := NewSlogLogger(logger)

	sl.Info("test message", "key", "value")

	output := buf.String()
	if !strings.Contains(output, "test message") {
		t.Errorf("expected output to contain 'test message', got: %s", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("expected output to contain 'key=value', got: %s", output)
	}
}

func TestSlogLoggerError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))
	sl := NewSlogLogger(logger)

	testErr := &testError{msg: "test error"}
	sl.Error(testErr, "error message", "key", "value")

	output := buf.String()
	if !strings.Contains(output, "error message") {
		t.Errorf("expected output to contain 'error message', got: %s", output)
	}
	if !strings.Contains(output, "error=") {
		t.Errorf("expected output to contain 'error=', got: %s", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("expected output to contain 'key=value', got: %s", output)
	}
}

func TestSlogLoggerNilDefault(t *testing.T) {
	// Should not panic when nil is passed and should use slog.Default()
	sl := NewSlogLogger(nil)
	// Verify it works by calling Info (would panic if logger was nil)
	sl.Info("test with nil logger")
}

func TestSlogLoggerImplementsInterface(t *testing.T) {
	var _ Logger = (*SlogLogger)(nil)
}

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

// captureLogger captures log output for testing
type captureLogger struct {
	output string
}

func (cl *captureLogger) Printf(format string, args ...any) {
	cl.output = fmt.Sprintf(format, args...)
}

// TestFormatStringBoundary tests the boundary condition at logger.go:65
// where `numKeysAndValues > 0` could be mutated to `>= 0`.
// This would incorrectly add ", " separator when there are no key-value pairs.
func TestFormatStringBoundary(t *testing.T) {
	t.Run("no key-value pairs - no separator", func(t *testing.T) {
		capture := &captureLogger{}
		logger := VerbosePrintfLogger(capture)
		logger.Info("message only")

		// With 0 key-value pairs, output should be just "message only"
		// NOT "message only, " (which would happen if mutation passes)
		if strings.Contains(capture.output, ", ") {
			t.Errorf("output should NOT contain ', ' separator with no key-value pairs, got: %q", capture.output)
		}
		if capture.output != "message only" {
			t.Errorf("expected 'message only', got: %q", capture.output)
		}
	})

	t.Run("with key-value pairs - has separator", func(t *testing.T) {
		capture := &captureLogger{}
		logger := VerbosePrintfLogger(capture)
		logger.Info("message", "key", "value")

		// With key-value pairs, output SHOULD contain ", " separator
		if !strings.Contains(capture.output, ", ") {
			t.Errorf("output SHOULD contain ', ' separator with key-value pairs, got: %q", capture.output)
		}
		expected := "message, key=value"
		if capture.output != expected {
			t.Errorf("expected %q, got: %q", expected, capture.output)
		}
	})

	t.Run("boundary at exactly zero vs one", func(t *testing.T) {
		// This is the critical boundary: 0 vs 1
		capture0 := &captureLogger{}
		logger0 := VerbosePrintfLogger(capture0)
		logger0.Info("msg")

		capture2 := &captureLogger{}
		logger2 := VerbosePrintfLogger(capture2)
		logger2.Info("msg", "k", "v")

		// Zero key-values: no ", "
		if strings.HasSuffix(capture0.output, ", ") || strings.Contains(capture0.output, ", ") {
			t.Errorf("zero key-values should have no separator, got: %q", capture0.output)
		}

		// Two key-values (1 pair): has ", "
		if !strings.Contains(capture2.output, ", ") {
			t.Errorf("with key-values should have separator, got: %q", capture2.output)
		}
	})
}

// TestPrintfLoggerNonVerboseDoesNotLog verifies that non-verbose logger
// does not log Info messages (only Error).
func TestPrintfLoggerNonVerboseDoesNotLog(t *testing.T) {
	capture := &captureLogger{}
	logger := PrintfLogger(capture)
	logger.Info("should not appear", "key", "value")

	if capture.output != "" {
		t.Errorf("non-verbose logger should not log Info, got: %q", capture.output)
	}
}

// TestPrintfLoggerError tests the Error method with boundary cases.
func TestPrintfLoggerError(t *testing.T) {
	t.Run("error with no extra key-values", func(t *testing.T) {
		capture := &captureLogger{}
		logger := PrintfLogger(capture)
		err := &testError{msg: "test error"}
		logger.Error(err, "error occurred")

		// Error always adds "error" key, so there will be key-values
		// Format: message, error=<err>
		if !strings.Contains(capture.output, "error occurred") {
			t.Errorf("should contain message, got: %q", capture.output)
		}
		if !strings.Contains(capture.output, "error=test error") {
			t.Errorf("should contain error, got: %q", capture.output)
		}
	})

	t.Run("error with additional key-values", func(t *testing.T) {
		capture := &captureLogger{}
		logger := PrintfLogger(capture)
		err := &testError{msg: "test error"}
		logger.Error(err, "error occurred", "key", "value")

		if !strings.Contains(capture.output, "error occurred") {
			t.Errorf("should contain message, got: %q", capture.output)
		}
		if !strings.Contains(capture.output, "error=test error") {
			t.Errorf("should contain error, got: %q", capture.output)
		}
		if !strings.Contains(capture.output, "key=value") {
			t.Errorf("should contain key=value, got: %q", capture.output)
		}
	})
}
