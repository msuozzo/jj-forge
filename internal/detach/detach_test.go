package detach

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestRewriteArgs_ReplacesDetach(t *testing.T) {
	args := []string{"check", "--force", "--detach", "@-"}
	got := rewriteArgs(args, "--detach", "--_detached")
	want := []string{"check", "--force", "--_detached", "@-"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRewriteArgs_AppendsIfMissing(t *testing.T) {
	args := []string{"check", "@-"}
	got := rewriteArgs(args, "--detach", "--_detached")
	if got[len(got)-1] != "--_detached" {
		t.Errorf("last arg = %q, want --_detached", got[len(got)-1])
	}
}

func TestRewriteArgs_DoesNotMutateInput(t *testing.T) {
	args := []string{"check", "--detach"}
	orig := make([]string, len(args))
	copy(orig, args)
	_ = rewriteArgs(args, "--detach", "--_detached")
	for i := range args {
		if args[i] != orig[i] {
			t.Errorf("input mutated: arg[%d] = %q, want %q", i, args[i], orig[i])
		}
	}
}

func TestReadLivePID_NoFile(t *testing.T) {
	pid, alive := readLivePID(filepath.Join(t.TempDir(), "nope.pid"))
	if alive {
		t.Errorf("expected not alive for missing file, got pid %d", pid)
	}
}

func TestReadLivePID_CorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pid")
	os.WriteFile(path, []byte("not-a-number\n"), 0644)

	pid, alive := readLivePID(path)
	if alive {
		t.Errorf("expected not alive for corrupt file, got pid %d", pid)
	}
}

func TestReadLivePID_DeadProcess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")
	// PID 999999999 almost certainly doesn't exist.
	os.WriteFile(path, []byte("999999999\n"), 0644)

	pid, alive := readLivePID(path)
	if alive {
		t.Errorf("expected not alive for dead PID, got pid %d", pid)
	}
	// Stale PID file should be cleaned up.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("stale PID file should have been removed")
	}
}

func TestReadLivePID_CurrentProcess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")
	os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n"), 0644)

	pid, alive := readLivePID(path)
	if !alive {
		t.Error("expected current process to be alive")
	}
	if pid != os.Getpid() {
		t.Errorf("pid = %d, want %d", pid, os.Getpid())
	}
}

func TestCleanup_RemovesPIDFile(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "check.pid")
	os.WriteFile(pidPath, []byte("12345\n"), 0644)

	proc := New("check", dir, NoTransform())
	proc.Cleanup()

	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Error("PID file should have been removed")
	}
}

func TestCleanup_NoErrorOnMissing(t *testing.T) {
	// Should not panic when PID file or directory doesn't exist.
	proc := New("check", filepath.Join(t.TempDir(), "nonexistent"), NoTransform())
	proc.Cleanup()
}

func TestStart_SingleInstanceEnforcement(t *testing.T) {
	dir := t.TempDir()

	// Write PID file with our own PID (which is alive).
	pidPath := filepath.Join(dir, "check.pid")
	os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0644)

	proc := New("check", dir, NoTransform())
	_, err := proc.Start([]string{"jj-forge", "check", "--_detached"})
	if err == nil {
		t.Fatal("expected error for already-running process")
	}
	if got := err.Error(); got != "jj-forge check is already running (pid "+strconv.Itoa(os.Getpid())+")" {
		t.Errorf("unexpected error: %s", got)
	}
}

func TestStart_StaleInstanceAllowed(t *testing.T) {
	dir := t.TempDir()

	// Write PID file with a dead PID.
	pidPath := filepath.Join(dir, "check.pid")
	os.WriteFile(pidPath, []byte("999999999\n"), 0644)

	// The Start call will fail later (bad executable), but the single-instance
	// check should pass — the stale PID is cleaned up.
	proc := New("check", dir, NoTransform())
	_, err := proc.Start([]string{"jj-forge", "check", "--_detached"})
	if err != nil && err.Error() == "jj-forge check is already running (pid 999999999)" {
		t.Error("stale PID should not block a new instance")
	}
}

func TestLogPath(t *testing.T) {
	proc := New("check", "/some/dir", NoTransform())
	want := "/some/dir/check.log"
	if got := proc.LogPath(); got != want {
		t.Errorf("LogPath() = %q, want %q", got, want)
	}
}

func TestFlagReplace(t *testing.T) {
	transform := FlagReplace("--detach", "--_detached")
	got := transform([]string{"check", "--force", "--detach", "@-"})
	want := []string{"check", "--force", "--_detached", "@-"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNoTransform(t *testing.T) {
	transform := NoTransform()
	args := []string{"check", "--force"}
	got := transform(args)
	if len(got) != len(args) {
		t.Fatalf("len = %d, want %d", len(got), len(args))
	}
	for i := range got {
		if got[i] != args[i] {
			t.Errorf("arg[%d] = %q, want %q", i, got[i], args[i])
		}
	}
}
