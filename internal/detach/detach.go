package detach

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// ArgTransform transforms args for the child process re-invocation.
// Required by New to force callers to explicitly decide how the child
// process will identify itself as the detached instance.
type ArgTransform func(args []string) []string

// FlagReplace returns an ArgTransform that replaces oldFlag with newFlag
// in the args. If oldFlag is not found, newFlag is appended.
func FlagReplace(oldFlag, newFlag string) ArgTransform {
	return func(args []string) []string {
		return rewriteArgs(args, oldFlag, newFlag)
	}
}

// NoTransform returns args unchanged. Use when the child process identifies
// itself through some other mechanism.
func NoTransform() ArgTransform {
	return func(args []string) []string {
		return args
	}
}

// Cmd holds the shared state for a detached process lifecycle.
type Cmd struct {
	name      string
	dir       string
	transform ArgTransform
}

// New creates a Cmd that manages a detached process named name under dir.
// The transform controls how os.Args are rewritten for the child invocation.
func New(name, dir string, transform ArgTransform) *Cmd {
	return &Cmd{name: name, dir: dir, transform: transform}
}

// Start re-invokes the current executable as a detached background child.
// args should be os.Args; the package resolves the executable via
// os.Executable() and applies the configured transform to args[1:].
// It redirects stdout/stderr to a log file under dir and detaches the child
// into its own session.
//
// Single-instance enforcement: if a live process for the same name already
// exists (tracked via <dir>/<name>.pid), Start returns an error.
func (c *Cmd) Start(args []string) (pid int, err error) {
	pidPath := filepath.Join(c.dir, c.name+".pid")
	// Single-instance check.
	if existingPID, alive := readLivePID(pidPath); alive {
		return 0, fmt.Errorf("jj-forge %s is already running (pid %d)", c.name, existingPID)
	}
	selfExe, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("finding executable: %w", err)
	}
	logPath := c.LogPath()
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return 0, fmt.Errorf("opening log file: %w", err)
	}
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		logFile.Close()
		return 0, fmt.Errorf("opening /dev/null: %w", err)
	}
	childArgs := c.transform(args[1:])
	child := exec.Command(selfExe, childArgs...)
	child.Stdin = devNull
	child.Stdout = logFile
	child.Stderr = logFile
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		devNull.Close()
		logFile.Close()
		return 0, fmt.Errorf("starting background process: %w", err)
	}
	childPID := child.Process.Pid
	// Write PID file.
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(childPID)+"\n"), 0644); err != nil {
		// Best effort: kill the child if we can't track it.
		child.Process.Kill()
		child.Process.Release()
		devNull.Close()
		logFile.Close()
		return 0, fmt.Errorf("writing PID file: %w", err)
	}
	// Detach from the child — we don't wait for it.
	child.Process.Release()
	devNull.Close()
	logFile.Close()
	return childPID, nil
}

// Cleanup removes the PID file. It should be deferred by the detached child
// process on exit.
func (c *Cmd) Cleanup() {
	os.Remove(filepath.Join(c.dir, c.name+".pid"))
}

// LogPath returns the path to the log file for this detached process.
func (c *Cmd) LogPath() string {
	return filepath.Join(c.dir, c.name+".log")
}

// rewriteArgs copies args, replacing oldFlag with newFlag.
// If oldFlag is not found, newFlag is appended.
func rewriteArgs(args []string, oldFlag, newFlag string) []string {
	out := make([]string, len(args))
	replaced := false
	for i, arg := range args {
		if arg == oldFlag {
			out[i] = newFlag
			replaced = true
		} else {
			out[i] = arg
		}
	}
	if !replaced {
		out = append(out, newFlag)
	}
	return out
}

// readLivePID reads a PID file and checks whether the process is still alive.
// Returns the PID and true if the process exists and is running.
func readLivePID(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false
	}
	if !isProcessAlive(pid) {
		// Stale PID file — clean it up.
		os.Remove(path)
		return pid, false
	}
	return pid, true
}

// isProcessAlive checks whether a process with the given PID is running.
func isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return errors.Is(proc.Signal(syscall.Signal(0)), nil)
}
