package check

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/msuozzo/jj-forge/internal/filelock"
	"github.com/msuozzo/jj-forge/internal/ui"
)

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
	defer lock.Unlock()
}

func TestAcquireLockWait_WaitsForRelease(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tracker := testTracker(t)

	// Acquire the lock in the main goroutine.
	lock, err := filelock.TryLock(filepath.Join(dir, lockFileName))
	if err != nil {
		t.Fatalf("TryLock failed: %v", err)
	}

	// Start a goroutine that waits for the lock.
	type result struct {
		lock *filelock.Lock
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		lf, err := acquireLockWait(context.Background(), dir, tracker)
		ch <- result{lf, err}
	}()

	// Give the waiter time to start polling, then release.
	time.Sleep(100 * time.Millisecond)
	lock.Unlock()

	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("acquireLockWait failed: %v", r.err)
		}
		defer r.lock.Unlock()
	case <-time.After(5 * time.Second):
		t.Fatal("acquireLockWait did not return after lock was released")
	}
}
