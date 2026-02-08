package detach

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestRewriteArgs_ReplacesDetach(t *testing.T) {
	args := []string{"check", "--force", "--detach", "@-"}
	got := rewriteArgs(args)
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
	got := rewriteArgs(args)
	if got[len(got)-1] != "--_detached" {
		t.Errorf("last arg = %q, want --_detached", got[len(got)-1])
	}
}

func TestRewriteArgs_DoesNotMutateInput(t *testing.T) {
	args := []string{"check", "--detach"}
	orig := make([]string, len(args))
	copy(orig, args)
	_ = rewriteArgs(args)
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

	Cleanup("check", dir)

	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Error("PID file should have been removed")
	}
}

func TestCleanup_NoErrorOnMissing(t *testing.T) {
	// Should not panic when PID file or directory doesn't exist.
	Cleanup("check", filepath.Join(t.TempDir(), "nonexistent"))
}

func TestExec_SingleInstanceEnforcement(t *testing.T) {
	dir := t.TempDir()

	// Write PID file with our own PID (which is alive).
	pidPath := filepath.Join(dir, "check.pid")
	os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0644)

	_, err := Exec("check", dir)
	if err == nil {
		t.Fatal("expected error for already-running process")
	}
	if got := err.Error(); got != "jj-forge check is already running (pid "+strconv.Itoa(os.Getpid())+")" {
		t.Errorf("unexpected error: %s", got)
	}
}

func TestExec_StaleInstanceAllowed(t *testing.T) {
	dir := t.TempDir()

	// Write PID file with a dead PID.
	pidPath := filepath.Join(dir, "check.pid")
	os.WriteFile(pidPath, []byte("999999999\n"), 0644)

	// The Exec call will fail later (bad executable), but the single-instance
	// check should pass — the stale PID is cleaned up.
	_, err := Exec("check", dir)
	if err != nil && err.Error() == "jj-forge check is already running (pid 999999999)" {
		t.Error("stale PID should not block a new instance")
	}
}
