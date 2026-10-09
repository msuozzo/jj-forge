// Package filelock provides cross-process locks backed by flock(2).
//
// The kernel releases a lock when its holder exits, so there is no stale-lock
// recovery to get wrong. Lock files are never removed: unlinking a locked file
// would let the next contender lock a fresh file while the holder still holds
// the old one.
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
	f *os.File
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
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, readHolder(path)
		}
		return nil, fmt.Errorf("locking %s: %w", path, err)
	}
	recordHolder(f)
	return &Lock{f: f}, nil
}

// recordHolder writes this process as the holder for contenders' messages.
// The record is informational, so a failure to write it doesn't fail the lock.
func recordHolder(f *os.File) {
	if err := f.Truncate(0); err == nil {
		f.WriteAt(fmt.Appendf(nil, "%d\n%d\n", os.Getpid(), time.Now().Unix()), 0)
	}
}

// Acquire takes the lock on path, retrying every interval while another holder
// has it. onHeld, when non-nil, is called with each *HeldError seen while
// waiting. It returns ctx's error if ctx ends first.
//
// It polls rather than blocking in flock(2): on macOS a blocked flock can
// miss the release while other goroutines in the process fork, and wait
// until the next one.
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
	l.f.Truncate(0) // clear the holder record, the file itself stays
	return l.f.Close()
}

// readHolder reads the holder record. The holder may be mid-write, in which
// case the fields stay zero.
func readHolder(path string) *HeldError {
	held := &HeldError{Path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		return held
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		return held
	}
	pid, pidErr := strconv.Atoi(lines[0])
	ts, tsErr := strconv.ParseInt(lines[1], 10, 64)
	if pidErr == nil && tsErr == nil {
		held.PID = pid
		held.Since = time.Unix(ts, 0)
	}
	return held
}
