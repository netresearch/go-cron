// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package cron

import (
	"bytes"
	"fmt"
	"log"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
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

// infoRecorder is a Logger that is not a printfLogger and records its Info
// messages.
type infoRecorder struct {
	mu   sync.Mutex
	msgs []string
}

func (r *infoRecorder) Info(msg string, _ ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, msg)
}

func (r *infoRecorder) Error(error, string, ...any) {}

func (r *infoRecorder) has(msg string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Contains(r.msgs, msg)
}

// TestRunLoopInfoMessages checks that the "wake" and "run" messages, which
// the scheduler skips for a logger that drops Info, still reach a verbose
// printf logger and any other Logger, and that a non-verbose printf logger
// still prints nothing.
func TestRunLoopInfoMessages(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	runOnce := func(t *testing.T, logger Logger) {
		t.Helper()
		fc := NewFakeClock(start)
		c := New(WithSeconds(), WithLogger(logger), WithLocation(time.UTC), WithClock(fc))
		ran := make(chan struct{}, 1)
		if _, err := c.AddFunc("* * * * * *", func() {
			select {
			case ran <- struct{}{}:
			default:
			}
		}); err != nil {
			t.Fatal(err)
		}
		c.Start()
		fc.BlockUntil(1)
		fc.Advance(time.Second)
		select {
		case <-ran:
		case <-time.After(5 * time.Second):
			t.Fatal("job did not run")
		}
		<-c.Stop().Done() // the run loop logs "run" before it takes the stop request
	}

	t.Run("verbose printf logger", func(t *testing.T) {
		var sw syncWriter
		runOnce(t, VerbosePrintfLogger(log.New(&sw, "", 0)))
		out := sw.String()
		for _, msg := range []string{"wake, now=", "run, now="} {
			if !strings.Contains(out, msg) {
				t.Errorf("output lacks %q:\n%s", msg, out)
			}
		}
	})

	t.Run("printf logger without Info", func(t *testing.T) {
		var sw syncWriter
		runOnce(t, PrintfLogger(log.New(&sw, "", 0)))
		if out := sw.String(); out != "" {
			t.Errorf("non-verbose logger printed:\n%s", out)
		}
	})

	t.Run("other logger", func(t *testing.T) {
		rec := &infoRecorder{}
		runOnce(t, rec)
		for _, msg := range []string{"wake", "run"} {
			if !rec.has(msg) {
				t.Errorf("Info(%q) not called; got %v", msg, rec.msgs)
			}
		}
	})
}
