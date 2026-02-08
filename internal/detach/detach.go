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

// Exec re-invokes the current process as a detached background child.
//
// It rewrites os.Args to replace --detach with --_detached, redirects
// stdout/stderr to a log file under .jj/forge/, and detaches the child
// into its own session. The caller should print the returned PID and exit.
//
// Single-instance enforcement: if a live process for the same name already
// exists (tracked via .jj/forge/<name>.pid), Exec returns an error.
func Exec(name, repoRoot string) (pid int, err error) {
	forgeDir := filepath.Join(repoRoot, ".jj", "forge")
	if err := os.MkdirAll(forgeDir, 0755); err != nil {
		return 0, fmt.Errorf("creating forge directory: %w", err)
	}
	pidPath := filepath.Join(forgeDir, name+".pid")
	// Single-instance check.
	if existingPID, alive := readLivePID(pidPath); alive {
		return 0, fmt.Errorf("jj-forge %s is already running (pid %d)", name, existingPID)
	}
	selfExe, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("finding executable: %w", err)
	}
	childArgs := rewriteArgs(os.Args[1:])
	logPath := filepath.Join(forgeDir, name+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return 0, fmt.Errorf("opening log file: %w", err)
	}
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		logFile.Close()
		return 0, fmt.Errorf("opening /dev/null: %w", err)
	}
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

// Cleanup removes the PID file for name. It should be deferred by the
// detached child process on exit.
func Cleanup(name, repoRoot string) {
	pidPath := filepath.Join(repoRoot, ".jj", "forge", name+".pid")
	os.Remove(pidPath)
}

// rewriteArgs copies args, replacing --detach with --_detached.
// If --detach is not found, --_detached is appended.
func rewriteArgs(args []string) []string {
	out := make([]string, len(args))
	replaced := false
	for i, arg := range args {
		if arg == "--detach" {
			out[i] = "--_detached"
			replaced = true
		} else {
			out[i] = arg
		}
	}
	if !replaced {
		out = append(out, "--_detached")
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
