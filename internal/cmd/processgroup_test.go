package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// readPID waits for the command under test to write a PID to path.
func readPID(t *testing.T, path string) int {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			return pid
		}
	}
	t.Fatalf("no PID written to %s", path)
	return 0
}

// alive reports whether pid is running. A killed child of an exited shell is
// reaped by init, so it doesn't linger as a zombie.
func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func waitDead(t *testing.T, pid int) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if !alive(pid) {
			return
		}
	}
	syscall.Kill(pid, syscall.SIGKILL)
	t.Errorf("process %d outlived the command", pid)
}

func TestDefaultExecutor_ProcessGroupCancelKillsChildren(t *testing.T) {
	t.Parallel()
	pidFile := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := DefaultExecutor(ctx, Opts{ProcessGroup: true}, "sh", "-c", "sleep 30 & echo $! > "+pidFile+"; wait")
		done <- err
	}()
	pid := readPID(t, pidFile)
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("cancelled command returned no error")
		}
	case <-time.After(processGroupWaitDelay + 5*time.Second):
		t.Fatal("cancelled command did not return")
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("cancelled command took %v to return", d)
	}
	waitDead(t, pid)
}

func TestDefaultExecutor_ProcessGroupKillsLeftoversOnExit(t *testing.T) {
	t.Parallel()
	pidFile := filepath.Join(t.TempDir(), "pid")
	// The background sleep doesn't hold the command's output, so the command
	// returns at once and would otherwise leave it running.
	_, err := DefaultExecutor(context.Background(), Opts{ProcessGroup: true}, "sh", "-c", "sleep 30 >/dev/null 2>&1 & echo $! > "+pidFile)
	if err != nil {
		t.Fatalf("DefaultExecutor: %v", err)
	}
	waitDead(t, readPID(t, pidFile))
}
