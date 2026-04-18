package ui

import (
	"fmt"
	"sync"
	"time"
)

// TaskStatus represents the state of a tracked task.
type TaskStatus int

const (
	TaskPending TaskStatus = iota
	TaskRunning
	TaskDone
	TaskFailed
	TaskSkipped
)

var spinnerFrames = []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}

type taskEntry struct {
	name    string
	status  TaskStatus
	message string
}

// TaskTracker displays animated progress for a set of named tasks.
//
// In interactive terminals it redraws task lines in-place with spinner
// animation. In non-interactive contexts it prints a single line per task
// when it reaches a terminal state.
type TaskTracker struct {
	ui          *UI
	entries     []*taskEntry
	nameToIndex map[string]int
	warnings    []string
	mu          sync.Mutex
	stopCh      chan struct{}
	frame       int
	started     bool
}

// NewTaskTracker creates a tracker for the given task names.
func NewTaskTracker(u *UI, taskNames []string) *TaskTracker {
	entries := make([]*taskEntry, len(taskNames))
	nameToIndex := make(map[string]int, len(taskNames))
	for i, name := range taskNames {
		entries[i] = &taskEntry{name: name, status: TaskPending}
		nameToIndex[name] = i
	}
	return &TaskTracker{
		ui:          u,
		entries:     entries,
		nameToIndex: nameToIndex,
		stopCh:      make(chan struct{}),
	}
}

// Start begins the animation loop for interactive terminals. For
// non-interactive writers this is a no-op.
func (t *TaskTracker) Start() {
	if !t.ui.IsInteractive() {
		return
	}
	t.started = true
	t.render()
	go t.loop()
}

func (t *TaskTracker) loop() {
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			t.mu.Lock()
			t.frame++
			t.mu.Unlock()
			t.render()
		}
	}
}

// SetStatus updates the status of task at index. It is safe to call from
// multiple goroutines. In non-interactive mode, a line is printed when the
// task reaches a terminal state (TaskDone, TaskFailed, or TaskSkipped).
func (t *TaskTracker) SetStatus(index int, status TaskStatus) {
	t.mu.Lock()
	t.entries[index].status = status
	if status == TaskDone || status == TaskFailed || status == TaskSkipped {
		t.entries[index].message = ""
	}
	entry := t.entries[index]
	interactive := t.ui.IsInteractive()
	t.mu.Unlock()

	if !interactive && (status == TaskDone || status == TaskFailed || status == TaskSkipped) {
		t.printTerminalLine(entry)
	}
}

// SetStatusByName updates the status of task by name.
func (t *TaskTracker) SetStatusByName(name string, status TaskStatus) {
	t.mu.Lock()
	index, ok := t.nameToIndex[name]
	t.mu.Unlock()
	if ok {
		t.SetStatus(index, status)
	}
}

// SetMessage updates the message of task at index. It is safe to call from
// multiple goroutines.
func (t *TaskTracker) SetMessage(index int, message string) {
	t.mu.Lock()
	t.entries[index].message = message
	t.mu.Unlock()
}

// SetMessageByName updates the message of task by name.
func (t *TaskTracker) SetMessageByName(name string, message string) {
	t.mu.Lock()
	index, ok := t.nameToIndex[name]
	t.mu.Unlock()
	if ok {
		t.SetMessage(index, message)
	}
}

// Len returns the number of tracked tasks.
func (t *TaskTracker) Len() int {
	return len(t.entries)
}

// IsInteractive reports whether the output is an interactive terminal.
func (t *TaskTracker) IsInteractive() bool {
	return t.ui.IsInteractive()
}

// Warning buffers a warning message to be displayed after the tracker
// finishes. It is safe to call from multiple goroutines.
func (t *TaskTracker) Warning(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	t.mu.Lock()
	t.warnings = append(t.warnings, msg)
	t.mu.Unlock()
}

// Finish stops the animation loop, renders the final state, and flushes
// any buffered warnings below the tracker output.
func (t *TaskTracker) Finish() {
	if t.started {
		close(t.stopCh)
		t.render()
	}
	t.mu.Lock()
	warnings := t.warnings
	t.warnings = nil
	t.mu.Unlock()
	for _, msg := range warnings {
		t.ui.PrintWarning("%s", msg)
	}
}

func (t *TaskTracker) printTerminalLine(e *taskEntry) {
	switch e.status {
	case TaskDone:
		fmt.Fprintf(t.ui, "  %s %s\n", t.ui.Styled("task_pass", "✓"), e.name)
	case TaskFailed:
		fmt.Fprintf(t.ui, "  %s %s\n", t.ui.Styled("task_fail", "✗"), e.name)
	case TaskSkipped:
		fmt.Fprintf(t.ui, "  %s %s\n", t.ui.Styled("task_skipped", "◌"), t.ui.Styled("task_skipped", e.name))
	}
}

// render redraws all task lines in-place (interactive mode only).
func (t *TaskTracker) render() {
	t.mu.Lock()
	defer t.mu.Unlock()

	n := len(t.entries)

	// Move cursor up to overwrite previous output (skip on first render).
	if t.frame > 0 {
		fmt.Fprintf(t.ui, "\x1b[%dA", n)
	}

	spinner := spinnerFrames[t.frame%len(spinnerFrames)]

	for _, e := range t.entries {
		// Clear the current line.
		fmt.Fprint(t.ui, "\x1b[2K")

		msg := ""
		if e.message != "" {
			msg = "  " + t.ui.Styled("task_pending", e.message)
		}

		switch e.status {
		case TaskPending:
			fmt.Fprintf(t.ui, "  %s %s%s\n", t.ui.Styled("task_pending", "·"), t.ui.Styled("task_pending", e.name), msg)
		case TaskRunning:
			fmt.Fprintf(t.ui, "  %s %s%s\n", t.ui.Styled("task_running", string(spinner)), e.name, msg)
		case TaskDone:
			fmt.Fprintf(t.ui, "  %s %s\n", t.ui.Styled("task_pass", "✓"), e.name)
		case TaskFailed:
			fmt.Fprintf(t.ui, "  %s %s\n", t.ui.Styled("task_fail", "✗"), e.name)
		case TaskSkipped:
			fmt.Fprintf(t.ui, "  %s %s\n", t.ui.Styled("task_skipped", "◌"), t.ui.Styled("task_skipped", e.name))
		}
	}
}
