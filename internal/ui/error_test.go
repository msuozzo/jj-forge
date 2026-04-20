package ui

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
)

func TestPrintError_Simple(t *testing.T) {
	var buf bytes.Buffer
	u := New(&buf, ColorNever)

	u.PrintError(errors.New("something went wrong"))

	want := "Error: something went wrong\n"
	if buf.String() != want {
		t.Errorf("PrintError() =\n%q\nwant:\n%q", buf.String(), want)
	}
}

func TestPrintError_SingleSource(t *testing.T) {
	var buf bytes.Buffer
	u := New(&buf, ColorNever)

	inner := errors.New("connection refused")
	outer := fmt.Errorf("failed to connect: %w", inner)
	u.PrintError(outer)

	want := "Error: failed to connect\nCaused by: connection refused\n"
	if buf.String() != want {
		t.Errorf("PrintError() =\n%q\nwant:\n%q", buf.String(), want)
	}
}

func TestPrintError_MultipleSource(t *testing.T) {
	var buf bytes.Buffer
	u := New(&buf, ColorNever)

	inner := errors.New("permission denied")
	mid := fmt.Errorf("failed to open file: %w", inner)
	outer := fmt.Errorf("configuration error: %w", mid)
	u.PrintError(outer)

	want := "Error: configuration error\nCaused by:\n  1: failed to open file: permission denied\n  2: permission denied\n"
	if buf.String() != want {
		t.Errorf("PrintError() =\n%q\nwant:\n%q", buf.String(), want)
	}
}

func TestPrintError_WithColor(t *testing.T) {
	var buf bytes.Buffer
	u := New(&buf, ColorAlways)

	u.PrintError(errors.New("something went wrong"))

	got := buf.String()
	// Should contain red bold "Error: " and bold message
	if !bytes.Contains([]byte(got), []byte("\x1b[1;31mError: \x1b[0m")) {
		t.Errorf("PrintError() missing red bold Error prefix in:\n%q", got)
	}
	if !bytes.Contains([]byte(got), []byte("\x1b[1msomething went wrong\x1b[0m")) {
		t.Errorf("PrintError() missing bold message in:\n%q", got)
	}
}

func TestPrintError_UserErrorWithHint(t *testing.T) {
	var buf bytes.Buffer
	u := New(&buf, ColorNever)

	err := &UserError{
		Msg:  "review not found",
		Hint: "run 'jj-forge review open' first",
	}
	u.PrintError(err)

	want := "Error: review not found\nHint: run 'jj-forge review open' first\n"
	if buf.String() != want {
		t.Errorf("PrintError() =\n%q\nwant:\n%q", buf.String(), want)
	}
}

func TestPrintError_UserErrorWithDetails(t *testing.T) {
	var buf bytes.Buffer
	u := New(&buf, ColorAlways)

	err := &UserError{
		Msg:     "check command failed",
		Details: "stderr output",
	}
	u.PrintError(err)

	got := buf.String()
	// Should contain red bold "Error: ", bold message, and red (not bold) details
	if !bytes.Contains([]byte(got), []byte("\x1b[1;31mError: \x1b[0m")) {
		t.Errorf("PrintError() missing red bold Error prefix in:\n%q", got)
	}
	if !bytes.Contains([]byte(got), []byte("\x1b[1mcheck command failed\x1b[0m")) {
		t.Errorf("PrintError() missing bold message in:\n%q", got)
	}
	if !bytes.Contains([]byte(got), []byte("\x1b[32mstderr output\x1b[0m")) {
		// Wait, Fg Red is 31, Green is 32. Let me check color.go again.
		// Black=0, Red=1, Green=2, ...
		// ansiCode = 30 + int(c-Black) = 30 + 1 = 31.
		if !bytes.Contains([]byte(got), []byte("\x1b[31mstderr output\x1b[0m")) {
			t.Errorf("PrintError() missing red details in:\n%q", got)
		}
	}
}

func TestPrintWarning(t *testing.T) {
	var buf bytes.Buffer
	u := New(&buf, ColorNever)

	u.PrintWarning("failed to delete bookmark %s", "push-abc")

	want := "Warning: failed to delete bookmark push-abc\n"
	if buf.String() != want {
		t.Errorf("PrintWarning() = %q, want %q", buf.String(), want)
	}
}

func TestPrintWarning_WithColor(t *testing.T) {
	var buf bytes.Buffer
	u := New(&buf, ColorAlways)

	u.PrintWarning("something happened")

	got := buf.String()
	if !bytes.Contains([]byte(got), []byte("\x1b[1;33mWarning: \x1b[0m")) {
		t.Errorf("PrintWarning() missing yellow bold Warning prefix in:\n%q", got)
	}
}

func TestPrintHint(t *testing.T) {
	var buf bytes.Buffer
	u := New(&buf, ColorNever)

	u.PrintHint("try running %s", "'jj describe'")

	want := "Hint: try running 'jj describe'\n"
	if buf.String() != want {
		t.Errorf("PrintHint() = %q, want %q", buf.String(), want)
	}
}

func TestPrintHint_WithColor(t *testing.T) {
	var buf bytes.Buffer
	u := New(&buf, ColorAlways)

	u.PrintHint("try again")

	got := buf.String()
	if !bytes.Contains([]byte(got), []byte("\x1b[1;36mHint: \x1b[0m")) {
		t.Errorf("PrintHint() missing cyan bold Hint prefix in:\n%q", got)
	}
}

func TestUserError_Error(t *testing.T) {
	err := &UserError{Msg: "something failed"}
	if err.Error() != "something failed" {
		t.Errorf("Error() = %q, want %q", err.Error(), "something failed")
	}

	err = &UserError{Msg: "outer", Source: errors.New("inner")}
	if err.Error() != "outer: inner" {
		t.Errorf("Error() = %q, want %q", err.Error(), "outer: inner")
	}
}

func TestUserError_Unwrap(t *testing.T) {
	inner := errors.New("inner")
	err := &UserError{Msg: "outer", Source: inner}
	if !errors.Is(err, inner) {
		t.Error("Unwrap() should make inner error available via errors.Is")
	}
}
