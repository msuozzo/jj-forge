package check

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/msuozzo/jj-forge/internal/cmd"
	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
	"github.com/msuozzo/jj-forge/internal/ui"
)

var testUI = ui.New(io.Discard, ui.ColorNever)

// mockClient implements jj.Client for testing.
type mockClient struct {
	mu      sync.Mutex
	config  map[string]string
	revs    []*jj.Rev
	root    string
	gitDir  string
	callLog [][]string
}

func newMockClient(revs []*jj.Rev) *mockClient {
	return &mockClient{
		config: make(map[string]string),
		revs:   revs,
		root:   "/fake/repo",
		gitDir: "/fake/git/dir",
	}
}

func (m *mockClient) Run(ctx context.Context, args ...string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.callLog = append(m.callLog, args)

	if len(args) < 4 {
		return "", fmt.Errorf("unexpected args: %v", args)
	}

	if args[0] == "config" && args[1] == "list" && args[2] == "--repo" {
		key := args[3]
		if key == "forge" {
			var result string
			for k, v := range m.config {
				result += fmt.Sprintf("forge.%s = %s\n", k, v)
			}
			return result, nil
		}
		if val, ok := m.config[key]; ok {
			return fmt.Sprintf("%s = %s", key, val), nil
		}
		return "", nil
	}

	if args[0] == "config" && args[1] == "set" && args[2] == "--repo" {
		key := args[3]
		value := args[4]
		switch key {
		case "forge.checks":
			m.config["checks"] = value
		case "forge.check-command":
			m.config["check-command"] = value
		default:
			m.config[key] = value
		}
		return "", nil
	}

	return "", fmt.Errorf("unexpected command: %v", args)
}

func (m *mockClient) Rev(ctx context.Context, rev string) (*jj.Rev, error) {
	revs, err := m.Revs(ctx, rev)
	if err != nil {
		return nil, err
	}
	if len(revs) != 1 {
		return nil, fmt.Errorf("expected 1 rev, got %d", len(revs))
	}
	return revs[0], nil
}

func (m *mockClient) Revs(ctx context.Context, revset string) ([]*jj.Rev, error) {
	return m.revs, nil
}

func (m *mockClient) Root(ctx context.Context) (string, error) {
	return m.root, nil
}

func (m *mockClient) RemoteURL(ctx context.Context, remote string) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (m *mockClient) GitDir(ctx context.Context) (string, error) {
	return m.gitDir, nil
}

func TestRunNoConfig(t *testing.T) {
	t.Parallel()
	// No check command configured — should be a no-op.
	mock := newMockClient([]*jj.Rev{{ID: "c1", CommitID: "abc", IsMutable: true}})
	configMgr := forge.NewConfigManager(mock)

	ran := false
	runner := func(ctx context.Context, _ cmd.Opts, args ...string) (string, error) {
		ran = true
		return "", nil
	}

	err := Run(context.Background(), mock, configMgr, "@", true, runner, testUI)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ran {
		t.Error("runner should not have been called when no check command is configured")
	}
}

func TestRunPass(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".jj", "forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	revs := []*jj.Rev{{ID: "c1", CommitID: "abc123", IsMutable: true}}
	mock := newMockClient(revs)
	mock.root = tmpDir
	mock.config["check-command"] = "\"echo hello\""
	configMgr := forge.NewConfigManager(mock)

	var ranCheck atomic.Bool
	runner := func(ctx context.Context, opts cmd.Opts, args ...string) (string, error) {
		cmdStr := strings.Join(args, " ")
		if strings.Contains(cmdStr, "sh -c echo hello") {
			ranCheck.Store(true)
			return "", nil
		}
		// Materialization commands (git archive, tar)
		return "fake-data", nil
	}

	err := Run(context.Background(), mock, configMgr, "@", true, runner, testUI)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ranCheck.Load() {
		t.Error("check command was not run")
	}

	// Verify verdict was stored
	verdict, err := configMgr.GetCheckVerdictByChangeID("c1")
	if err != nil {
		t.Fatalf("GetCheckVerdictByChangeID failed: %v", err)
	}
	if verdict == nil {
		t.Fatal("expected verdict, got nil")
	}
	if verdict.Verdict != forge.CheckVerdictPass {
		t.Errorf("expected verdict 'pass', got %q", verdict.Verdict)
	}
	if verdict.CommitID != "abc123" {
		t.Errorf("expected commit ID 'abc123', got %q", verdict.CommitID)
	}
}

func TestRunFail(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".jj", "forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	revs := []*jj.Rev{{ID: "c1", CommitID: "abc123", IsMutable: true}}
	mock := newMockClient(revs)
	mock.root = tmpDir
	mock.config["check-command"] = "\"false\""
	configMgr := forge.NewConfigManager(mock)

	runner := func(ctx context.Context, opts cmd.Opts, args ...string) (string, error) {
		cmdStr := strings.Join(args, " ")
		if strings.Contains(cmdStr, "sh -c false") {
			return "", fmt.Errorf("exit status 1")
		}
		// Materialization commands
		return "fake-data", nil
	}

	err := Run(context.Background(), mock, configMgr, "@", true, runner, testUI)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify verdict was stored as "fail"
	verdict, err := configMgr.GetCheckVerdictByChangeID("c1")
	if err != nil {
		t.Fatalf("GetCheckVerdictByChangeID failed: %v", err)
	}
	if verdict == nil {
		t.Fatal("expected verdict, got nil")
	}
	if verdict.Verdict != forge.CheckVerdictFail {
		t.Errorf("expected verdict 'fail', got %q", verdict.Verdict)
	}
}

func TestRunSkipCached(t *testing.T) {
	t.Parallel()
	revs := []*jj.Rev{{ID: "c1", CommitID: "abc123", IsMutable: true}}
	mock := newMockClient(revs)
	mock.config["check-command"] = "\"echo hello\""
	configMgr := forge.NewConfigManager(mock)

	// Pre-populate a passing verdict with matching commit ID
	if err := configMgr.SetCheckVerdict(forge.CheckVerdict{
		ChangeID: "c1",
		Verdict:  forge.CheckVerdictPass,
		CommitID: "abc123",
	}); err != nil {
		t.Fatalf("SetCheckVerdict failed: %v", err)
	}

	ran := false
	runner := func(ctx context.Context, _ cmd.Opts, args ...string) (string, error) {
		ran = true
		return "", nil
	}

	// force=false should skip execution
	err := Run(context.Background(), mock, configMgr, "@", false, runner, testUI)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ran {
		t.Error("runner should not have been called when cached verdict is passing")
	}
}

func TestRunForceIgnoresCache(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".jj", "forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	revs := []*jj.Rev{{ID: "c1", CommitID: "abc123", IsMutable: true}}
	mock := newMockClient(revs)
	mock.root = tmpDir
	mock.config["check-command"] = "\"echo hello\""
	configMgr := forge.NewConfigManager(mock)

	// Pre-populate a passing verdict with matching commit ID
	if err := configMgr.SetCheckVerdict(forge.CheckVerdict{
		ChangeID: "c1",
		Verdict:  forge.CheckVerdictPass,
		CommitID: "abc123",
	}); err != nil {
		t.Fatalf("SetCheckVerdict failed: %v", err)
	}

	ran := false
	runner := func(ctx context.Context, _ cmd.Opts, args ...string) (string, error) {
		ran = true
		return "", nil
	}

	// force=true should run regardless
	err := Run(context.Background(), mock, configMgr, "@", true, runner, testUI)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ran {
		t.Error("runner should have been called when force=true")
	}
}

func TestRunMultipleRevs(t *testing.T) {
	t.Parallel()
	// Use a real temp dir so WorkPool can create pool directories.
	tmpDir := t.TempDir()

	revs := []*jj.Rev{
		{ID: "c1", CommitID: "abc", IsMutable: true},
		{ID: "c2", CommitID: "def", IsMutable: true},
	}
	mock := newMockClient(revs)
	mock.root = tmpDir
	mock.config["check-command"] = "\"echo hello\""
	configMgr := forge.NewConfigManager(mock)

	// Create .jj directory for pool base
	if err := os.MkdirAll(filepath.Join(tmpDir, ".jj", "forge"), 0o755); err != nil {
		t.Fatal(err)
	}

	var poolRuns atomic.Int32
	runner := func(ctx context.Context, opts cmd.Opts, args ...string) (string, error) {
		cmdStr := strings.Join(args, " ")
		if strings.Contains(cmdStr, "sh -c echo hello") && opts.WorkDir != "" {
			poolRuns.Add(1)
			return "", nil
		}
		// Materialization commands (git archive, tar)
		return "fake-data", nil
	}

	err := Run(context.Background(), mock, configMgr, "@-::@", true, runner, testUI)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if poolRuns.Load() != 2 {
		t.Errorf("expected 2 pool runs, got %d", poolRuns.Load())
	}

	// Verify verdicts were stored for both
	for _, rev := range revs {
		verdict, err := configMgr.GetCheckVerdictByChangeID(rev.ID)
		if err != nil {
			t.Fatalf("GetCheckVerdictByChangeID(%s) failed: %v", rev.ID, err)
		}
		if verdict == nil {
			t.Fatalf("expected verdict for %s, got nil", rev.ID)
		}
		if verdict.Verdict != forge.CheckVerdictPass {
			t.Errorf("expected verdict 'pass' for %s, got %q", rev.ID, verdict.Verdict)
		}
	}
}

func TestRunMultipleRevs_CachedSkip(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	revs := []*jj.Rev{
		{ID: "c1", CommitID: "abc", IsMutable: true},
		{ID: "c2", CommitID: "def", IsMutable: true},
	}
	mock := newMockClient(revs)
	mock.root = tmpDir
	mock.config["check-command"] = "\"echo hello\""
	configMgr := forge.NewConfigManager(mock)

	// Pre-populate passing verdicts for both
	for _, rev := range revs {
		if err := configMgr.SetCheckVerdict(forge.CheckVerdict{
			ChangeID: rev.ID,
			Verdict:  forge.CheckVerdictPass,
			CommitID: rev.CommitID,
		}); err != nil {
			t.Fatalf("SetCheckVerdict failed: %v", err)
		}
	}

	ran := false
	runner := func(ctx context.Context, _ cmd.Opts, args ...string) (string, error) {
		ran = true
		return "", nil
	}

	err := Run(context.Background(), mock, configMgr, "@-::@", false, runner, testUI)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ran {
		t.Error("runner should not have been called when all verdicts are cached")
	}
}

func TestRunMultipleRevs_MixedResults(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	revs := []*jj.Rev{
		{ID: "c1", CommitID: "abc", IsMutable: true},
		{ID: "c2", CommitID: "def", IsMutable: true},
	}
	mock := newMockClient(revs)
	mock.root = tmpDir
	mock.config["check-command"] = "\"echo hello\""
	configMgr := forge.NewConfigManager(mock)

	if err := os.MkdirAll(filepath.Join(tmpDir, ".jj", "forge"), 0o755); err != nil {
		t.Fatal(err)
	}

	var checkCount atomic.Int32
	runner := func(ctx context.Context, opts cmd.Opts, args ...string) (string, error) {
		cmdStr := strings.Join(args, " ")
		if strings.Contains(cmdStr, "sh -c echo hello") {
			if checkCount.Add(1) == 1 {
				return "", nil // first check passes
			}
			return "", fmt.Errorf("check failed") // second check fails
		}
		// Materialization commands
		return "fake-data", nil
	}

	err := Run(context.Background(), mock, configMgr, "@-::@", true, runner, testUI)
	if err == nil {
		t.Fatal("expected error for mixed results, got nil")
	}

	// Verify one passed and one failed (order depends on pool scheduling).
	var passCount, failCount int
	for _, rev := range revs {
		verdict, err := configMgr.GetCheckVerdictByChangeID(rev.ID)
		if err != nil {
			t.Fatalf("GetCheckVerdictByChangeID(%s) failed: %v", rev.ID, err)
		}
		if verdict == nil {
			t.Fatalf("expected verdict for %s, got nil", rev.ID)
		}
		switch verdict.Verdict {
		case forge.CheckVerdictPass:
			passCount++
		case forge.CheckVerdictFail:
			failCount++
		}
	}
	if passCount != 1 || failCount != 1 {
		t.Errorf("expected 1 pass and 1 fail, got %d passes and %d fails", passCount, failCount)
	}
}

func TestRunMultipleRevs_AllPool(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	revs := []*jj.Rev{
		{ID: "c1", CommitID: "abc", IsMutable: true},
		{ID: "c2", CommitID: "def", IsMutable: true},
	}
	mock := newMockClient(revs)
	mock.root = tmpDir
	mock.config["check-command"] = "\"echo hello\""
	configMgr := forge.NewConfigManager(mock)

	if err := os.MkdirAll(filepath.Join(tmpDir, ".jj", "forge"), 0o755); err != nil {
		t.Fatal(err)
	}

	var poolRuns atomic.Int32
	runner := func(ctx context.Context, opts cmd.Opts, args ...string) (string, error) {
		cmdStr := strings.Join(args, " ")
		if strings.Contains(cmdStr, "sh -c echo hello") && opts.WorkDir != "" {
			poolRuns.Add(1)
			return "", nil
		}
		// Materialization commands
		return "fake-data", nil
	}

	err := Run(context.Background(), mock, configMgr, "@-::@", true, runner, testUI)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if poolRuns.Load() != 2 {
		t.Errorf("expected 2 pool runs, got %d", poolRuns.Load())
	}
}

func TestRunStaleCache(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".jj", "forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	revs := []*jj.Rev{{ID: "c1", CommitID: "newcommit", IsMutable: true}}
	mock := newMockClient(revs)
	mock.root = tmpDir
	mock.config["check-command"] = "\"echo hello\""
	configMgr := forge.NewConfigManager(mock)

	// Pre-populate a passing verdict with OLD commit ID
	if err := configMgr.SetCheckVerdict(forge.CheckVerdict{
		ChangeID: "c1",
		Verdict:  forge.CheckVerdictPass,
		CommitID: "oldcommit",
	}); err != nil {
		t.Fatalf("SetCheckVerdict failed: %v", err)
	}

	ran := false
	runner := func(ctx context.Context, _ cmd.Opts, args ...string) (string, error) {
		ran = true
		return "", nil
	}

	// force=false, but commit ID changed — should re-run
	err := Run(context.Background(), mock, configMgr, "@", false, runner, testUI)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ran {
		t.Error("runner should have been called when commit ID is stale")
	}
}

func TestRunImmutableSkipped(t *testing.T) {
	t.Parallel()
	// All revisions are immutable — should be a no-op.
	revs := []*jj.Rev{
		{ID: "c1", CommitID: "abc", IsMutable: false},
		{ID: "c2", CommitID: "def", IsMutable: false},
	}
	mock := newMockClient(revs)
	mock.config["check-command"] = "\"echo hello\""
	configMgr := forge.NewConfigManager(mock)

	ran := false
	runner := func(ctx context.Context, _ cmd.Opts, args ...string) (string, error) {
		ran = true
		return "", nil
	}

	err := Run(context.Background(), mock, configMgr, "@-::@", true, runner, testUI)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ran {
		t.Error("runner should not have been called for immutable revisions")
	}
}

func TestRunSetsRunningBeforeExecution(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".jj", "forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	revs := []*jj.Rev{{ID: "c1", CommitID: "abc123", IsMutable: true}}
	mock := newMockClient(revs)
	mock.root = tmpDir
	mock.config["check-command"] = "\"echo hello\""
	configMgr := forge.NewConfigManager(mock)

	// Two-phase channel sync: runner signals it has started, test reads
	// verdict to confirm "running", then unblocks the runner.
	started := make(chan struct{})
	proceed := make(chan struct{})

	runner := func(ctx context.Context, _ cmd.Opts, args ...string) (string, error) {
		cmdStr := strings.Join(args, " ")
		if strings.Contains(cmdStr, "echo hello") {
			started <- struct{}{}
			<-proceed
		}
		return "", nil
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(context.Background(), mock, configMgr, "@", true, runner, testUI)
	}()

	// Wait for runner to signal it has started.
	<-started

	// Read the verdict — it should be "running" since the check hasn't completed yet.
	verdict, err := configMgr.GetCheckVerdictByChangeID("c1")
	if err != nil {
		t.Fatalf("GetCheckVerdictByChangeID failed: %v", err)
	}
	if verdict == nil {
		t.Fatal("expected verdict, got nil")
	}
	if verdict.Verdict != forge.CheckVerdictRunning {
		t.Errorf("expected verdict 'running' during execution, got %q", verdict.Verdict)
	}
	if verdict.CommitID != "abc123" {
		t.Errorf("expected commit ID 'abc123', got %q", verdict.CommitID)
	}

	// Unblock the runner and wait for Run to finish.
	close(proceed)
	if err := <-errCh; err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// After completion, the verdict should be overwritten to "pass".
	verdict, err = configMgr.GetCheckVerdictByChangeID("c1")
	if err != nil {
		t.Fatalf("GetCheckVerdictByChangeID failed: %v", err)
	}
	if verdict == nil {
		t.Fatal("expected verdict, got nil")
	}
	if verdict.Verdict != forge.CheckVerdictPass {
		t.Errorf("expected verdict 'pass' after completion, got %q", verdict.Verdict)
	}
}

func TestRunMixedMutability(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".jj", "forge"), 0o755); err != nil {
		t.Fatal(err)
	}

	revs := []*jj.Rev{
		{ID: "c1", CommitID: "abc", IsMutable: true},
		{ID: "c2", CommitID: "def", IsMutable: false}, // immutable — should be skipped
	}
	mock := newMockClient(revs)
	mock.root = tmpDir
	mock.config["check-command"] = "\"echo hello\""
	configMgr := forge.NewConfigManager(mock)

	var ranCheck atomic.Bool
	runner := func(ctx context.Context, opts cmd.Opts, args ...string) (string, error) {
		cmdStr := strings.Join(args, " ")
		if strings.Contains(cmdStr, "sh -c echo hello") {
			ranCheck.Store(true)
		}
		// Materialization commands
		return "", nil
	}

	err := Run(context.Background(), mock, configMgr, "@-::@", true, runner, testUI)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// c1 (mutable) should have been checked.
	if !ranCheck.Load() {
		t.Error("mutable revision c1 should have been checked")
	}

	// c2 (immutable) should not have a verdict stored.
	verdict, err := configMgr.GetCheckVerdictByChangeID("c2")
	if err != nil {
		t.Fatalf("GetCheckVerdictByChangeID failed: %v", err)
	}
	if verdict != nil {
		t.Errorf("immutable revision c2 should not have a verdict, got %v", verdict)
	}
}
