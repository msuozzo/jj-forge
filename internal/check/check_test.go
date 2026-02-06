package check

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
)

// mockClient implements jj.Client for testing.
type mockClient struct {
	mu      sync.Mutex
	config  map[string]string
	revs    []*jj.Rev
	root    string
	callLog [][]string
}

func newMockClient(revs []*jj.Rev) *mockClient {
	return &mockClient{
		config: make(map[string]string),
		revs:   revs,
		root:   "/fake/repo",
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
	return "/fake/git/dir", nil
}

func TestRunNoConfig(t *testing.T) {
	// No check command configured — should be a no-op.
	mock := newMockClient([]*jj.Rev{{ID: "c1", CommitID: "abc"}})
	configMgr := forge.NewConfigManager(mock)

	ran := false
	runner := func(ctx context.Context, repoPath, command string) error {
		ran = true
		return nil
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
	revs := []*jj.Rev{{ID: "c1", CommitID: "abc123"}}
	mock := newMockClient(revs)
	mock.config["check-command"] = "\"echo hello\""
	configMgr := forge.NewConfigManager(mock)

	runner := func(ctx context.Context, repoPath, command string) error {
		if command != "echo hello" {
			t.Errorf("expected command 'echo hello', got %q", command)
		}
		if repoPath != "/fake/repo" {
			t.Errorf("expected repoPath '/fake/repo', got %q", repoPath)
		}
		return nil
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
	revs := []*jj.Rev{{ID: "c1", CommitID: "abc123"}}
	mock := newMockClient(revs)
	mock.config["check-command"] = "\"false\""
	configMgr := forge.NewConfigManager(mock)

	runner := func(ctx context.Context, repoPath, command string) error {
		return fmt.Errorf("exit status 1")
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
	revs := []*jj.Rev{{ID: "c1", CommitID: "abc123"}}
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
	runner := func(ctx context.Context, repoPath, command string) error {
		ran = true
		return nil
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
	revs := []*jj.Rev{{ID: "c1", CommitID: "abc123"}}
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
	runner := func(ctx context.Context, repoPath, command string) error {
		ran = true
		return nil
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
	revs := []*jj.Rev{
		{ID: "c1", CommitID: "abc"},
		{ID: "c2", CommitID: "def"},
	}
	mock := newMockClient(revs)
	mock.config["check-command"] = "\"echo hello\""
	configMgr := forge.NewConfigManager(mock)

	runner := func(ctx context.Context, repoPath, command string) error {
		t.Error("runner should not have been called")
		return nil
	}

	err := Run(context.Background(), mock, configMgr, "@", true, runner)
	if err == nil {
		t.Fatal("expected error for multiple revisions, got nil")
	}
}

func TestRunStaleCache(t *testing.T) {
	revs := []*jj.Rev{{ID: "c1", CommitID: "newcommit"}}
	mock := newMockClient(revs)
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
	runner := func(ctx context.Context, repoPath, command string) error {
		ran = true
		return nil
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
