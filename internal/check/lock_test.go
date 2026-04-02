package check

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/msuozzo/jj-forge/internal/ui"
)

func TestAcquireLock_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lock, err := acquireLock(dir)
	if err != nil {
		t.Fatalf("acquireLock failed: %v", err)
	}
	defer lock.release()

	// Verify lock file exists and has expected contents.
	path := filepath.Join(dir, lockFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading lock file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), string(data))
	}
	pid, err := strconv.Atoi(lines[0])
	if err != nil {
		t.Fatalf("parsing PID: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("PID = %d, want %d", pid, os.Getpid())
	}
	ts, err := strconv.ParseInt(lines[1], 10, 64)
	if err != nil {
		t.Fatalf("parsing timestamp: %v", err)
	}
	if time.Since(time.Unix(ts, 0)) > 5*time.Second {
		t.Errorf("timestamp too old: %d", ts)
	}
}

func TestAcquireLock_AlreadyHeld(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lock, err := acquireLock(dir)
	if err != nil {
		t.Fatalf("first acquireLock failed: %v", err)
	}
	defer lock.release()

	// Second acquire should fail — current PID is alive.
	_, err = acquireLock(dir)
	if err == nil {
		t.Fatal("expected error on second acquire, got nil")
	}
	var lc *lockContention
	if !errors.As(err, &lc) {
		t.Fatalf("expected *lockContention, got %T: %v", err, err)
	}
}

func TestAcquireLock_StaleDeadProcess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, lockFileName)

	// Write a lock with a bogus PID that doesn't exist.
	content := fmt.Sprintf("%d\n%d\n", 999999999, time.Now().Unix())
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	lock, err := acquireLock(dir)
	if err != nil {
		t.Fatalf("acquireLock should break stale lock: %v", err)
	}
	defer lock.release()

	// Verify we now own the lock.
	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	pid, _ := strconv.Atoi(lines[0])
	if pid != os.Getpid() {
		t.Errorf("PID = %d, want %d (our PID)", pid, os.Getpid())
	}
}

func TestAcquireLock_CorruptFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, lockFileName)

	// Write garbage to the lock file.
	if err := os.WriteFile(path, []byte("garbage"), 0644); err != nil {
		t.Fatal(err)
	}

	lock, err := acquireLock(dir)
	if err != nil {
		t.Fatalf("acquireLock should break corrupt lock: %v", err)
	}
	defer lock.release()

	// Verify we now own the lock.
	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	pid, _ := strconv.Atoi(lines[0])
	if pid != os.Getpid() {
		t.Errorf("PID = %d, want %d (our PID)", pid, os.Getpid())
	}
}

func TestRelease(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lock, err := acquireLock(dir)
	if err != nil {
		t.Fatalf("acquireLock failed: %v", err)
	}

	if err := lock.release(); err != nil {
		t.Fatalf("release failed: %v", err)
	}

	// File should be gone.
	path := filepath.Join(dir, lockFileName)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("lock file still exists after release")
	}

	// Double release should not panic (returns error for missing file, which is fine).
	_ = lock.release()
}

func TestRelease_Nil(t *testing.T) {
	t.Parallel()
	var lock *lockFile
	if err := lock.release(); err != nil {
		t.Errorf("nil release should return nil, got %v", err)
	}
}

func testTracker(t *testing.T) *ui.TaskTracker {
	t.Helper()
	u := ui.New(os.Stdout, ui.ColorNever)
	return ui.NewTaskTracker(u, []string{"test"})
}

func TestAcquireLockWait_NoContention(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tracker := testTracker(t)
	lock, err := acquireLockWait(context.Background(), dir, tracker)
	if err != nil {
		t.Fatalf("acquireLockWait failed: %v", err)
	}
	defer lock.release()
}

func TestAcquireLockWait_WaitsForRelease(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tracker := testTracker(t)

	// Acquire the lock in the main goroutine.
	lock, err := acquireLock(dir)
	if err != nil {
		t.Fatalf("acquireLock failed: %v", err)
	}

	// Start a goroutine that waits for the lock.
	type result struct {
		lock *lockFile
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		lf, err := acquireLockWait(context.Background(), dir, tracker)
		ch <- result{lf, err}
	}()

	// Give the waiter time to start polling, then release.
	time.Sleep(100 * time.Millisecond)
	lock.release()

	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("acquireLockWait failed: %v", r.err)
		}
		defer r.lock.release()
	case <-time.After(5 * time.Second):
		t.Fatal("acquireLockWait did not return after lock was released")
	}
}
