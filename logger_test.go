package cron

import (
	"bytes"
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
