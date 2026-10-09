package filelock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTryLock_RecordsHolder(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "x.lock")
	l, err := TryLock(path)
	if err != nil {
		t.Fatalf("TryLock: %v", err)
	}
	defer l.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %q", data)
	}
	if pid, _ := strconv.Atoi(lines[0]); pid != os.Getpid() {
		t.Errorf("PID = %d, want %d", pid, os.Getpid())
	}
	ts, _ := strconv.ParseInt(lines[1], 10, 64)
	if time.Since(time.Unix(ts, 0)) > 5*time.Second {
		t.Errorf("timestamp too old: %d", ts)
	}
}

func TestTryLock_Held(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "x.lock")
	l, err := TryLock(path)
	if err != nil {
		t.Fatalf("TryLock: %v", err)
	}
	defer l.Unlock()

	_, err = TryLock(path)
	var held *HeldError
	if !errors.As(err, &held) {
		t.Fatalf("second TryLock: got %v, want *HeldError", err)
	}
	if held.PID != os.Getpid() {
		t.Errorf("HeldError.PID = %d, want %d", held.PID, os.Getpid())
	}
}

func TestTryLock_IgnoresLeftoverRecord(t *testing.T) {
	t.Parallel()
	for name, content := range map[string]string{
		"dead holder": "999999999\n1\n",
		"garbage":     "garbage",
		"empty":       "",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "x.lock")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			l, err := TryLock(path)
			if err != nil {
				t.Fatalf("TryLock: %v", err)
			}
			l.Unlock()
		})
	}
}

func TestUnlock_KeepsFileAndFreesLock(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "x.lock")
	l, err := TryLock(path)
	if err != nil {
		t.Fatalf("TryLock: %v", err)
	}
	if err := l.Unlock(); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("lock file should stay after Unlock: %v", err)
	}
	l2, err := TryLock(path)
	if err != nil {
		t.Fatalf("TryLock after Unlock: %v", err)
	}
	l2.Unlock()
}

func TestUnlock_Nil(t *testing.T) {
	t.Parallel()
	var l *Lock
	if err := l.Unlock(); err != nil {
		t.Errorf("nil Unlock: %v", err)
	}
}

func TestAcquire_WaitsForRelease(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "x.lock")
	l, err := TryLock(path)
	if err != nil {
		t.Fatalf("TryLock: %v", err)
	}
	var sawHeld atomic.Bool
	done := make(chan error, 1)
	go func() {
		l2, err := Acquire(context.Background(), path, 10*time.Millisecond, func(*HeldError) { sawHeld.Store(true) })
		l2.Unlock()
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	l.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Acquire: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Acquire did not return after the lock was released")
	}
	if !sawHeld.Load() {
		t.Error("onHeld was not called while waiting")
	}
}

func TestAcquire_ContextCancelled(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "x.lock")
	l, err := TryLock(path)
	if err != nil {
		t.Fatalf("TryLock: %v", err)
	}
	defer l.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := Acquire(ctx, path, 10*time.Millisecond, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Acquire: got %v, want context.DeadlineExceeded", err)
	}
}

// TestTryLock_MutualExclusion has many contenders take and release the lock in
// a tight loop and fails if two ever hold it at once. flock locks belong to the
// open file, so goroutines in one process contend as separate processes would.
func TestTryLock_MutualExclusion(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "x.lock")
	deadline := time.Now().Add(300 * time.Millisecond)
	var holders, acquisitions, overlaps atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(deadline) {
				l, err := TryLock(path)
				var held *HeldError
				if errors.As(err, &held) {
					continue
				}
				if err != nil {
					t.Errorf("TryLock: %v", err)
					return
				}
				if holders.Add(1) > 1 {
					overlaps.Add(1)
				}
				acquisitions.Add(1)
				time.Sleep(50 * time.Microsecond)
				holders.Add(-1)
				l.Unlock()
			}
		}()
	}
	wg.Wait()
	if acquisitions.Load() == 0 {
		t.Fatal("no contender ever took the lock")
	}
	if n := overlaps.Load(); n > 0 {
		t.Errorf("%d of %d acquisitions overlapped another holder", n, acquisitions.Load())
	}
}
