package detach

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
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

// logLimit is the size past which Start begins the log afresh rather than
// appending to it.
const logLimit = 1 << 20

// Start re-invokes the current executable as a detached background child.
// args should be os.Args; the package resolves the executable via
// os.Executable() and applies the configured transform to args[1:].
// It appends stdout/stderr to a log file under dir, after a line marking the
// start of the run, and detaches the child into its own session.
//
// Runs may overlap. Anything they must not do at once, like running checks,
// needs its own lock.
func (c *Cmd) Start(args []string) (pid int, err error) {
	selfExe, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("finding executable: %w", err)
	}
	logPath := c.LogPath()
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if info, err := os.Stat(logPath); err == nil && info.Size() > logLimit {
		flags |= os.O_TRUNC
	}
	logFile, err := os.OpenFile(logPath, flags, 0644)
	if err != nil {
		return 0, fmt.Errorf("opening log file: %w", err)
	}
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		logFile.Close()
		return 0, fmt.Errorf("opening /dev/null: %w", err)
	}
	childArgs := c.transform(args[1:])
	fmt.Fprintf(logFile, "=== %s jj-forge %s\n", time.Now().Format(time.DateTime), strings.Join(args[1:], " "))
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
	// Detach from the child — we don't wait for it.
	child.Process.Release()
	devNull.Close()
	logFile.Close()
	return childPID, nil
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
