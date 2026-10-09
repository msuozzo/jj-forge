// Package filelock provides cross-process locks on files.
//
// A lock is a file created with O_EXCL that records its holder's PID and
// start time. A lock whose holder is no longer running, or whose record can't
// be read, is stale and is taken over.
package filelock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Lock is a held lock. Unlock releases it.
type Lock struct {
	path string
}

// HeldError is returned by TryLock when another holder has the lock. PID and
// Since describe the holder when its record could be read, and are zero
// otherwise.
type HeldError struct {
	Path  string
	PID   int
	Since time.Time
}

func (e *HeldError) Error() string {
	if e.PID == 0 {
		return fmt.Sprintf("%s is locked by another process", e.Path)
	}
	return fmt.Sprintf("%s is locked by pid %d since %s", e.Path, e.PID, e.Since.Format(time.TimeOnly))
}

// TryLock takes the lock on path without waiting, creating the file if needed.
// It returns a *HeldError when another holder has the lock.
func TryLock(path string) (*Lock, error) {
	return tryLock(path, false)
}

func tryLock(path string, isRetry bool) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err == nil {
		content := fmt.Sprintf("%d\n%d\n", os.Getpid(), time.Now().Unix())
		if _, writeErr := f.WriteString(content); writeErr != nil {
			f.Close()
			os.Remove(path)
			return nil, fmt.Errorf("failed to write lock file: %w", writeErr)
		}
		f.Close()
		return &Lock{path: path}, nil
	}
	if !os.IsExist(err) {
		return nil, fmt.Errorf("failed to create lock file: %w", err)
	}
	// Lock file exists — check if stale.
	if isRetry {
		return nil, &HeldError{Path: path}
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		// Corrupt/unreadable — remove and retry.
		os.Remove(path)
		return tryLock(path, true)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		os.Remove(path)
		return tryLock(path, true)
	}
	pid, pidErr := strconv.Atoi(lines[0])
	ts, tsErr := strconv.ParseInt(lines[1], 10, 64)
	if pidErr != nil || tsErr != nil {
		os.Remove(path)
		return tryLock(path, true)
	}
	// Check if owning process is still alive.
	proc, procErr := os.FindProcess(pid)
	if procErr != nil || proc.Signal(syscall.Signal(0)) != nil {
		// Process is dead — stale lock.
		os.Remove(path)
		return tryLock(path, true)
	}
	// Process alive — real contention.
	return nil, &HeldError{Path: path, PID: pid, Since: time.Unix(ts, 0)}
}

// Acquire takes the lock on path, retrying every interval while another holder
// has it. onHeld, when non-nil, is called with each *HeldError seen while
// waiting. It returns ctx's error if ctx ends first.
func Acquire(ctx context.Context, path string, interval time.Duration, onHeld func(*HeldError)) (*Lock, error) {
	for {
		l, err := TryLock(path)
		var held *HeldError
		if !errors.As(err, &held) {
			return l, err
		}
		if onHeld != nil {
			onHeld(held)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// Unlock releases the lock. It is safe to call on a nil Lock.
func (l *Lock) Unlock() error {
	if l == nil {
		return nil
	}
	return os.Remove(l.path)
}
