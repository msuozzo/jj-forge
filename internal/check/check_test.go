package check

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/msuozzo/jj-forge/internal/cmd"
	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
)

// mockClient implements jj.Client for testing.
type mockClient struct {
	mu      sync.Mutex
	config  map[string]string
	revs    []*jj.Rev
	wcRev   *jj.Rev // working copy revision returned by Rev(ctx, "@")
	root    string
	gitDir  string
	callLog [][]string
}

func newMockClient(revs []*jj.Rev) *mockClient {
	var wcRev *jj.Rev
	if len(revs) > 0 {
		wcRev = revs[0] // default: first rev is working copy
	}
	return &mockClient{
		config: make(map[string]string),
		revs:   revs,
		wcRev:  wcRev,
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
		if key == "forge.checks" {
			m.config["checks"] = value
		} else if key == "forge.check-command" {
			m.config["check-command"] = value
		} else {
			m.config[key] = value
		}
		return "", nil
	}

	return "", fmt.Errorf("unexpected command: %v", args)
}

func (m *mockClient) Rev(ctx context.Context, rev string) (*jj.Rev, error) {
	if rev == "@" && m.wcRev != nil {
		return m.wcRev, nil
	}
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
	// No check command configured — should be a no-op.
	mock := newMockClient([]*jj.Rev{{ID: "c1", CommitID: "abc", IsMutable: true}})
	configMgr := forge.NewConfigManager(mock)

	ran := false
	runner := func(ctx context.Context, _ cmd.Opts, args ...string) (string, error) {
		ran = true
		return "", nil
	}

	err := Run(context.Background(), mock, configMgr, "@", true, runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ran {
		t.Error("runner should not have been called when no check command is configured")
	}
}

func TestRunPass(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".jj"), 0o755); err != nil {
		t.Fatal(err)
	}
	revs := []*jj.Rev{{ID: "c1", CommitID: "abc123", IsMutable: true}}
	mock := newMockClient(revs)
	mock.root = tmpDir
	mock.config["check-command"] = "\"echo hello\""
	configMgr := forge.NewConfigManager(mock)

	runner := func(ctx context.Context, _ cmd.Opts, args ...string) (string, error) {
		// args: jj -R /fake/repo util exec -- sh -c "echo hello"
		wantSuffix := []string{"util", "exec", "--", "sh", "-c", "echo hello"}
		gotSuffix := args[len(args)-len(wantSuffix):]
		for i, w := range wantSuffix {
			if gotSuffix[i] != w {
				t.Errorf("arg %d = %q, want %q", i, gotSuffix[i], w)
			}
		}
		return "", nil
	}

	err := Run(context.Background(), mock, configMgr, "@", true, runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".jj"), 0o755); err != nil {
		t.Fatal(err)
	}
	revs := []*jj.Rev{{ID: "c1", CommitID: "abc123", IsMutable: true}}
	mock := newMockClient(revs)
	mock.root = tmpDir
	mock.config["check-command"] = "\"false\""
	configMgr := forge.NewConfigManager(mock)

	runner := func(ctx context.Context, _ cmd.Opts, args ...string) (string, error) {
		return "", fmt.Errorf("exit status 1")
	}

	err := Run(context.Background(), mock, configMgr, "@", true, runner)
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
	err := Run(context.Background(), mock, configMgr, "@", false, runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ran {
		t.Error("runner should not have been called when cached verdict is passing")
	}
}

func TestRunForceIgnoresCache(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".jj"), 0o755); err != nil {
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
	err := Run(context.Background(), mock, configMgr, "@", true, runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ran {
		t.Error("runner should have been called when force=true")
	}
}

func TestRunMultipleRevs(t *testing.T) {
	// Use a real temp dir so WorkPool can create pool directories.
	tmpDir := t.TempDir()

	revs := []*jj.Rev{
		{ID: "c1", CommitID: "abc", IsMutable: true},
		{ID: "c2", CommitID: "def", IsMutable: true},
	}
	mock := newMockClient(revs)
	mock.wcRev = revs[0] // c1 is working copy
	mock.root = tmpDir
	mock.config["check-command"] = "\"echo hello\""
	configMgr := forge.NewConfigManager(mock)

	// Create .jj directory for pool base
	if err := os.MkdirAll(filepath.Join(tmpDir, ".jj"), 0o755); err != nil {
		t.Fatal(err)
	}

	var runs sync.Map
	runner := func(ctx context.Context, opts cmd.Opts, args ...string) (string, error) {
		// Identify which command this is
		cmdStr := strings.Join(args, " ")
		if strings.Contains(cmdStr, "jj") && strings.Contains(cmdStr, "util exec") {
			runs.Store("wc", true)
			return "", nil
		}
		if strings.Contains(cmdStr, "sh -c echo hello") {
			runs.Store("pool", true)
			return "", nil
		}
		// Pool materialization commands (git archive, tar)
		return "fake-data", nil
	}

	err := Run(context.Background(), mock, configMgr, "@-::@", true, runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify both were checked
	if _, ok := runs.Load("wc"); !ok {
		t.Error("working copy check was not run")
	}
	if _, ok := runs.Load("pool"); !ok {
		t.Error("pool check was not run")
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
	tmpDir := t.TempDir()

	revs := []*jj.Rev{
		{ID: "c1", CommitID: "abc", IsMutable: true},
		{ID: "c2", CommitID: "def", IsMutable: true},
	}
	mock := newMockClient(revs)
	mock.wcRev = revs[0]
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

	err := Run(context.Background(), mock, configMgr, "@-::@", false, runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ran {
		t.Error("runner should not have been called when all verdicts are cached")
	}
}

func TestRunMultipleRevs_MixedResults(t *testing.T) {
	tmpDir := t.TempDir()

	revs := []*jj.Rev{
		{ID: "c1", CommitID: "abc", IsMutable: true},
		{ID: "c2", CommitID: "def", IsMutable: true},
	}
	mock := newMockClient(revs)
	mock.wcRev = revs[0]
	mock.root = tmpDir
	mock.config["check-command"] = "\"echo hello\""
	configMgr := forge.NewConfigManager(mock)

	if err := os.MkdirAll(filepath.Join(tmpDir, ".jj"), 0o755); err != nil {
		t.Fatal(err)
	}

	runner := func(ctx context.Context, opts cmd.Opts, args ...string) (string, error) {
		cmdStr := strings.Join(args, " ")
		// Working copy (c1) passes
		if strings.Contains(cmdStr, "jj") && strings.Contains(cmdStr, "util exec") {
			return "", nil
		}
		// Pool check (c2) fails
		if strings.Contains(cmdStr, "sh -c echo hello") {
			return "", fmt.Errorf("check failed")
		}
		// Materialization commands
		return "fake-data", nil
	}

	err := Run(context.Background(), mock, configMgr, "@-::@", true, runner)
	if err == nil {
		t.Fatal("expected error for mixed results, got nil")
	}

	// Verify c1 passed, c2 failed
	v1, err := configMgr.GetCheckVerdictByChangeID("c1")
	if err != nil {
		t.Fatal(err)
	}
	if v1 == nil || v1.Verdict != forge.CheckVerdictPass {
		t.Errorf("expected c1 to pass, got %v", v1)
	}

	v2, err := configMgr.GetCheckVerdictByChangeID("c2")
	if err != nil {
		t.Fatal(err)
	}
	if v2 == nil || v2.Verdict != forge.CheckVerdictFail {
		t.Errorf("expected c2 to fail, got %v", v2)
	}
}

func TestRunMultipleRevs_WorkingCopySpecialCase(t *testing.T) {
	// When all revisions are the working copy (single rev = wc), no pool is needed.
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".jj"), 0o755); err != nil {
		t.Fatal(err)
	}
	revs := []*jj.Rev{{ID: "c1", CommitID: "abc123", IsMutable: true}}
	mock := newMockClient(revs)
	mock.wcRev = revs[0]
	mock.root = tmpDir
	mock.config["check-command"] = "\"echo hello\""
	configMgr := forge.NewConfigManager(mock)

	var usedJJExec atomic.Bool
	runner := func(ctx context.Context, opts cmd.Opts, args ...string) (string, error) {
		cmdStr := strings.Join(args, " ")
		if strings.Contains(cmdStr, "util exec") {
			usedJJExec.Store(true)
		}
		return "", nil
	}

	err := Run(context.Background(), mock, configMgr, "@", true, runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !usedJJExec.Load() {
		t.Error("expected working copy to use jj util exec")
	}
}

func TestRunMultipleRevs_AllPool(t *testing.T) {
	// When no revision matches working copy, all go through pool.
	tmpDir := t.TempDir()

	revs := []*jj.Rev{
		{ID: "c1", CommitID: "abc", IsMutable: true},
		{ID: "c2", CommitID: "def", IsMutable: true},
	}
	mock := newMockClient(revs)
	mock.wcRev = &jj.Rev{ID: "wc", CommitID: "wc-commit"} // different from all revs
	mock.root = tmpDir
	mock.config["check-command"] = "\"echo hello\""
	configMgr := forge.NewConfigManager(mock)

	if err := os.MkdirAll(filepath.Join(tmpDir, ".jj"), 0o755); err != nil {
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

	err := Run(context.Background(), mock, configMgr, "@-::@", true, runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if poolRuns.Load() != 2 {
		t.Errorf("expected 2 pool runs, got %d", poolRuns.Load())
	}
}

func TestRunStaleCache(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".jj"), 0o755); err != nil {
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
	err := Run(context.Background(), mock, configMgr, "@", false, runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ran {
		t.Error("runner should have been called when commit ID is stale")
	}
}

func TestRunImmutableSkipped(t *testing.T) {
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

	err := Run(context.Background(), mock, configMgr, "@-::@", true, runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ran {
		t.Error("runner should not have been called for immutable revisions")
	}
}

func TestRunMixedMutability(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".jj"), 0o755); err != nil {
		t.Fatal(err)
	}

	revs := []*jj.Rev{
		{ID: "c1", CommitID: "abc", IsMutable: true},
		{ID: "c2", CommitID: "def", IsMutable: false}, // immutable — should be skipped
	}
	mock := newMockClient(revs)
	mock.wcRev = revs[0] // c1 is working copy
	mock.root = tmpDir
	mock.config["check-command"] = "\"echo hello\""
	configMgr := forge.NewConfigManager(mock)

	var checkedIDs sync.Map
	runner := func(ctx context.Context, opts cmd.Opts, args ...string) (string, error) {
		cmdStr := strings.Join(args, " ")
		if strings.Contains(cmdStr, "util exec") {
			checkedIDs.Store("c1", true)
		}
		return "", nil
	}

	err := Run(context.Background(), mock, configMgr, "@-::@", true, runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// c1 (mutable) should have been checked.
	if _, ok := checkedIDs.Load("c1"); !ok {
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
