package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestIsInteractive_NonTerminal(t *testing.T) {
	var buf bytes.Buffer
	u := New(&buf, ColorNever)
	if u.IsInteractive() {
		t.Error("IsInteractive() = true for bytes.Buffer, want false")
	}
}

func TestTaskTracker_NonInteractive_Done(t *testing.T) {
	var buf bytes.Buffer
	u := New(&buf, ColorNever)
	tracker := NewTaskTracker(u, []string{"task-a", "task-b"})
	tracker.Start() // no-op for non-interactive

	tracker.SetStatus(0, TaskRunning)
	tracker.SetStatus(0, TaskDone)

	tracker.SetStatus(1, TaskRunning)
	tracker.SetStatus(1, TaskDone)

	tracker.Finish()

	got := buf.String()
	if !strings.Contains(got, "✓ task-a") {
		t.Errorf("expected checkmark for task-a, got %q", got)
	}
	if !strings.Contains(got, "✓ task-b") {
		t.Errorf("expected checkmark for task-b, got %q", got)
	}
}

func TestTaskTracker_NonInteractive_Failed(t *testing.T) {
	var buf bytes.Buffer
	u := New(&buf, ColorNever)
	tracker := NewTaskTracker(u, []string{"task-x"})
	tracker.Start()

	tracker.SetStatus(0, TaskRunning)
	tracker.SetStatus(0, TaskFailed)

	tracker.Finish()

	got := buf.String()
	if !strings.Contains(got, "✗ task-x") {
		t.Errorf("expected cross for task-x, got %q", got)
	}
}

func TestTaskTracker_NonInteractive_PendingNoOutput(t *testing.T) {
	var buf bytes.Buffer
	u := New(&buf, ColorNever)
	tracker := NewTaskTracker(u, []string{"task-p"})
	tracker.Start()

	// Only set to running, never to terminal state.
	tracker.SetStatus(0, TaskRunning)

	tracker.Finish()

	if buf.Len() != 0 {
		t.Errorf("expected no output for non-terminal task, got %q", buf.String())
	}
}

func TestTaskTracker_NonInteractive_Mixed(t *testing.T) {
	var buf bytes.Buffer
	u := New(&buf, ColorNever)
	tracker := NewTaskTracker(u, []string{"pass-task", "fail-task"})
	tracker.Start()

	tracker.SetStatus(0, TaskRunning)
	tracker.SetStatus(0, TaskDone)
	tracker.SetStatus(1, TaskRunning)
	tracker.SetStatus(1, TaskFailed)

	tracker.Finish()

	got := buf.String()
	if !strings.Contains(got, "✓ pass-task") {
		t.Errorf("expected checkmark for pass-task, got %q", got)
	}
	if !strings.Contains(got, "✗ fail-task") {
		t.Errorf("expected cross for fail-task, got %q", got)
	}
}
