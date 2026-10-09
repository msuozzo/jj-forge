package check

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/msuozzo/jj-forge/internal/filelock"
	"github.com/msuozzo/jj-forge/internal/ui"
)

func TestAcquireLockWait_NoContention(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var out bytes.Buffer
	lock, err := acquireLockWait(context.Background(), dir, ui.New(&out, ui.ColorNever))
	if err != nil {
		t.Fatalf("acquireLockWait failed: %v", err)
	}
	defer lock.Unlock()
	if out.Len() != 0 {
		t.Errorf("expected no output without contention, got %q", out.String())
	}
}

func TestAcquireLockWait_WaitsForRelease(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var out bytes.Buffer

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
		lf, err := acquireLockWait(context.Background(), dir, ui.New(&out, ui.ColorNever))
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
	want := fmt.Sprintf("Waiting for the check run in pid %d...\n", os.Getpid())
	if got := out.String(); got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}
