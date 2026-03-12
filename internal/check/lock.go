package check

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/msuozzo/jj-forge/internal/ui"
)

const lockFileName = "check.lock"

type lockFile struct {
	path string
}

// lockContention is returned by tryAcquire when the lock is held by a live process.
type lockContention struct {
	pid  int
	age  time.Duration
	path string
}

func (e *lockContention) Error() string {
	return fmt.Sprintf("another jj-forge change check is running (pid %d, started %s ago)", e.pid, e.age)
}

func acquireLock(dir string) (*lockFile, error) {
	path := filepath.Join(dir, lockFileName)
	return tryAcquire(path, false)
}

func tryAcquire(path string, isRetry bool) (*lockFile, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err == nil {
		content := fmt.Sprintf("%d\n%d\n", os.Getpid(), time.Now().Unix())
		if _, writeErr := f.WriteString(content); writeErr != nil {
			f.Close()
			os.Remove(path)
			return nil, fmt.Errorf("failed to write lock file: %w", writeErr)
		}
		f.Close()
		return &lockFile{path: path}, nil
	}
	if !os.IsExist(err) {
		return nil, fmt.Errorf("failed to create lock file: %w", err)
	}
	// Lock file exists — check if stale.
	if isRetry {
		return nil, &lockContention{path: path}
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		// Corrupt/unreadable — remove and retry.
		os.Remove(path)
		return tryAcquire(path, true)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		os.Remove(path)
		return tryAcquire(path, true)
	}
	pid, pidErr := strconv.Atoi(lines[0])
	ts, tsErr := strconv.ParseInt(lines[1], 10, 64)
	if pidErr != nil || tsErr != nil {
		os.Remove(path)
		return tryAcquire(path, true)
	}
	// Check if owning process is still alive.
	proc, procErr := os.FindProcess(pid)
	if procErr != nil || proc.Signal(syscall.Signal(0)) != nil {
		// Process is dead — stale lock.
		os.Remove(path)
		return tryAcquire(path, true)
	}
	// Process alive — real contention.
	age := time.Since(time.Unix(ts, 0)).Truncate(time.Second)
	return nil, &lockContention{pid: pid, age: age, path: path}
}

// acquireLockWait polls until the check lock can be acquired. If the lock is
// held by another process, it prints a waiting message and retries every 500ms.
func acquireLockWait(ctx context.Context, dir string, u *ui.UI) (*lockFile, error) {
	path := filepath.Join(dir, lockFileName)
	lf, err := tryAcquire(path, false)
	if err == nil {
		return lf, nil
	}
	var lc *lockContention
	if !errors.As(err, &lc) {
		return nil, err
	}
	lastPID := 0
	for {
		if lc.pid != lastPID {
			fmt.Fprintf(u, "Waiting for running check to complete (pid %d)...\n", lc.pid)
			lastPID = lc.pid
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
		lf, err = tryAcquire(path, false)
		if err == nil {
			return lf, nil
		}
		if !errors.As(err, &lc) {
			return nil, err
		}
	}
}

func (l *lockFile) release() error {
	if l == nil {
		return nil
	}
	return os.Remove(l.path)
}
