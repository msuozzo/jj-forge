package check

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/msuozzo/jj-forge/internal/cmd"
)

// cmdRecord records a single command invocation for test assertions.
type cmdRecord struct {
	args   []string
	opts   cmd.Opts
	stdin  string
	output string
	err    error
}

// mockRunner builds a cmd.Executor that replays pre-configured command results
// and records every invocation.
type mockRunner struct {
	handlers []func(ctx context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error)
	calls    []cmdRecord
}

func (m *mockRunner) run(ctx context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error) {
	if len(m.handlers) == 0 {
		return nil, fmt.Errorf("unexpected call: %v", args)
	}
	h := m.handlers[0]
	m.handlers = m.handlers[1:]
	result, err := h(ctx, opts, args...)
	var stdinStr string
	if opts.Stdin != nil {
		b := make([]byte, 1024*1024)
		n, _ := opts.Stdin.Read(b)
		stdinStr = string(b[:n])
	}
	var output string
	if result != nil {
		output = result.Stdout
	}
	m.calls = append(m.calls, cmdRecord{args: args, opts: opts, stdin: stdinStr, output: output, err: err})
	return result, err
}

func TestNewWorkPool_CreatesDirectories(t *testing.T) {
	t.Parallel()
	baseDir := t.TempDir()
	mr := &mockRunner{}
	pool, err := NewWorkPool("/fake/git", baseDir, 3, mr.run)
	if err != nil {
		t.Fatalf("NewWorkPool failed: %v", err)
	}
	if len(pool.dirs) != 3 {
		t.Errorf("expected 3 dirs, got %d", len(pool.dirs))
	}
	for i := range 3 {
		dirPath := filepath.Join(baseDir, fmt.Sprintf("%d", i))
		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			t.Errorf("expected directory %s to exist", dirPath)
		}
	}
}

func TestNewWorkPool_ReadsExistingState(t *testing.T) {
	t.Parallel()
	baseDir := t.TempDir()
	// Pre-create a dir with a state file
	dir0 := filepath.Join(baseDir, "0")
	if err := os.MkdirAll(dir0, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir0, stateFileName), []byte("abc123\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mr := &mockRunner{}
	pool, err := NewWorkPool("/fake/git", baseDir, 2, mr.run)
	if err != nil {
		t.Fatalf("NewWorkPool failed: %v", err)
	}
	if pool.dirs[0].CommitID != "abc123" {
		t.Errorf("expected CommitID 'abc123', got %q", pool.dirs[0].CommitID)
	}
	if pool.dirs[1].CommitID != "" {
		t.Errorf("expected empty CommitID for dir 1, got %q", pool.dirs[1].CommitID)
	}
}

func TestAcquireRelease(t *testing.T) {
	t.Parallel()
	baseDir := t.TempDir()
	mr := &mockRunner{}
	pool, err := NewWorkPool("/fake/git", baseDir, 2, mr.run)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	wd1, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire 1 failed: %v", err)
	}
	wd2, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire 2 failed: %v", err)
	}

	// Pool is exhausted; cancel context to avoid hanging
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = pool.Acquire(cancelCtx)
	if err == nil {
		t.Error("expected error from Acquire on exhausted pool with cancelled context")
	}

	// Release and re-acquire
	pool.Release(wd1)
	wd3, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire 3 failed: %v", err)
	}
	if wd3.Path != wd1.Path {
		t.Errorf("expected re-acquired dir %s, got %s", wd1.Path, wd3.Path)
	}
	pool.Release(wd2)
	pool.Release(wd3)
}

func TestMaterialize_FullMaterialization(t *testing.T) {
	t.Parallel()
	baseDir := t.TempDir()
	mr := &mockRunner{
		handlers: []func(ctx context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error){
			// git archive
			func(_ context.Context, _ cmd.Opts, args ...string) (*cmd.Result, error) {
				if !contains(args, "archive") {
					t.Errorf("expected 'archive' in args, got %v", args)
				}
				if !contains(args, "commit1") {
					t.Errorf("expected 'commit1' in args, got %v", args)
				}
				return &cmd.Result{Stdout: "fake-tar-data"}, nil
			},
			// tar -xf -
			func(_ context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error) {
				if !contains(args, "tar") {
					t.Errorf("expected 'tar' in args, got %v", args)
				}
				if opts.WorkDir == "" {
					t.Error("expected WorkDir to be set for tar")
				}
				return &cmd.Result{}, nil
			},
		},
	}

	pool, err := NewWorkPool("/fake/git", baseDir, 1, mr.run)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	wd, _ := pool.Acquire(ctx)
	if err := pool.Materialize(ctx, wd, "commit1"); err != nil {
		t.Fatalf("Materialize failed: %v", err)
	}

	// Verify state file was written
	data, err := os.ReadFile(filepath.Join(wd.Path, stateFileName))
	if err != nil {
		t.Fatalf("failed to read state file: %v", err)
	}
	if strings.TrimSpace(string(data)) != "commit1" {
		t.Errorf("expected state 'commit1', got %q", strings.TrimSpace(string(data)))
	}
	if wd.CommitID != "commit1" {
		t.Errorf("expected CommitID 'commit1', got %q", wd.CommitID)
	}
}

func TestMaterialize_SkipsWhenUpToDate(t *testing.T) {
	t.Parallel()
	baseDir := t.TempDir()
	// Pre-create state
	dir0 := filepath.Join(baseDir, "0")
	if err := os.MkdirAll(dir0, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir0, stateFileName), []byte("commit1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mr := &mockRunner{}
	pool, err := NewWorkPool("/fake/git", baseDir, 1, mr.run)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	wd, _ := pool.Acquire(ctx)
	if err := pool.Materialize(ctx, wd, "commit1"); err != nil {
		t.Fatalf("Materialize failed: %v", err)
	}
	// No commands should have been run
	if len(mr.calls) != 0 {
		t.Errorf("expected 0 commands, got %d", len(mr.calls))
	}
}

func TestMaterialize_IncrementalUpdate(t *testing.T) {
	t.Parallel()
	baseDir := t.TempDir()
	dir0 := filepath.Join(baseDir, "0")
	if err := os.MkdirAll(dir0, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir0, stateFileName), []byte("commit1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mr := &mockRunner{
		handlers: []func(ctx context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error){
			// git diff
			func(_ context.Context, _ cmd.Opts, args ...string) (*cmd.Result, error) {
				if !contains(args, "diff") {
					t.Errorf("expected 'diff' in args, got %v", args)
				}
				if !contains(args, "commit1..commit2") {
					t.Errorf("expected 'commit1..commit2' in args, got %v", args)
				}
				return &cmd.Result{Stdout: "fake-diff-output"}, nil
			},
			// git apply
			func(_ context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error) {
				if !contains(args, "apply") {
					t.Errorf("expected 'apply' in args, got %v", args)
				}
				if opts.WorkDir == "" {
					t.Error("expected WorkDir to be set for apply")
				}
				return &cmd.Result{}, nil
			},
		},
	}

	pool, err := NewWorkPool("/fake/git", baseDir, 1, mr.run)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	wd, _ := pool.Acquire(ctx)
	if err := pool.Materialize(ctx, wd, "commit2"); err != nil {
		t.Fatalf("Materialize failed: %v", err)
	}

	if wd.CommitID != "commit2" {
		t.Errorf("expected CommitID 'commit2', got %q", wd.CommitID)
	}
	// Should have used diff + apply (2 commands), not archive + tar
	if len(mr.calls) != 2 {
		t.Errorf("expected 2 commands (diff + apply), got %d", len(mr.calls))
	}
}

func TestMaterialize_IncrementalFallback(t *testing.T) {
	t.Parallel()
	baseDir := t.TempDir()
	dir0 := filepath.Join(baseDir, "0")
	if err := os.MkdirAll(dir0, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir0, stateFileName), []byte("commit1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mr := &mockRunner{
		handlers: []func(ctx context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error){
			// git diff - succeeds
			func(_ context.Context, _ cmd.Opts, args ...string) (*cmd.Result, error) {
				return &cmd.Result{Stdout: "fake-diff"}, nil
			},
			// git apply - fails
			func(_ context.Context, _ cmd.Opts, args ...string) (*cmd.Result, error) {
				return nil, fmt.Errorf("apply conflict")
			},
			// git archive - fallback full materialization
			func(_ context.Context, _ cmd.Opts, args ...string) (*cmd.Result, error) {
				if !contains(args, "archive") {
					t.Errorf("expected 'archive' in fallback, got %v", args)
				}
				return &cmd.Result{Stdout: "fake-tar"}, nil
			},
			// tar -xf -
			func(_ context.Context, _ cmd.Opts, args ...string) (*cmd.Result, error) {
				return &cmd.Result{}, nil
			},
		},
	}

	pool, err := NewWorkPool("/fake/git", baseDir, 1, mr.run)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	wd, _ := pool.Acquire(ctx)
	if err := pool.Materialize(ctx, wd, "commit2"); err != nil {
		t.Fatalf("Materialize failed: %v", err)
	}

	if wd.CommitID != "commit2" {
		t.Errorf("expected CommitID 'commit2', got %q", wd.CommitID)
	}
	// diff + apply(fail) + archive + tar = 4 calls
	if len(mr.calls) != 4 {
		t.Errorf("expected 4 commands (diff, apply-fail, archive, tar), got %d", len(mr.calls))
	}
}

// TestMaterialize_InvalidatesStateBeforeMutation verifies the journaling
// contract: the state file is removed before any git command runs, and a
// failed mutation leaves the slot in the "fresh" state (no state file,
// empty wd.CommitID) so the next call falls into fullMaterialize.
func TestMaterialize_InvalidatesStateBeforeMutation(t *testing.T) {
	t.Parallel()
	baseDir := t.TempDir()
	dir0 := filepath.Join(baseDir, "0")
	if err := os.MkdirAll(dir0, 0o755); err != nil {
		t.Fatal(err)
	}
	stateFile := filepath.Join(dir0, stateFileName)
	if err := os.WriteFile(stateFile, []byte("commit1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mr := &mockRunner{
		handlers: []func(ctx context.Context, opts cmd.Opts, args ...string) (*cmd.Result, error){
			// git diff: assert state already invalidated, then "crash".
			func(_ context.Context, _ cmd.Opts, _ ...string) (*cmd.Result, error) {
				if _, err := os.Stat(stateFile); !os.IsNotExist(err) {
					t.Errorf("expected state file removed before git diff, stat err=%v", err)
				}
				return nil, fmt.Errorf("simulated crash during diff")
			},
			// fullMaterialize archive: also fail so the mutation never completes.
			func(_ context.Context, _ cmd.Opts, _ ...string) (*cmd.Result, error) {
				return nil, fmt.Errorf("simulated crash during archive")
			},
		},
	}

	pool, err := NewWorkPool("/fake/git", baseDir, 1, mr.run)
	if err != nil {
		t.Fatal(err)
	}
	if pool.dirs[0].CommitID != "commit1" {
		t.Fatalf("expected initial CommitID 'commit1', got %q", pool.dirs[0].CommitID)
	}

	ctx := context.Background()
	wd, _ := pool.Acquire(ctx)
	if err := pool.Materialize(ctx, wd, "commit2"); err == nil {
		t.Error("expected Materialize to fail when all mutations fail")
	}
	if _, err := os.Stat(stateFile); !os.IsNotExist(err) {
		t.Errorf("expected state file still missing after failed materialize, stat err=%v", err)
	}
	if wd.CommitID != "" {
		t.Errorf("expected wd.CommitID cleared, got %q", wd.CommitID)
	}
}

func contains(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
