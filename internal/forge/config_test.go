package forge

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/msuozzo/jj-forge/internal/jj"
)

// mockClient is a simple mock for testing ConfigManager
type mockClient struct {
	mu      sync.Mutex
	config  map[string]string
	callLog [][]string
}

func newMockClient() *mockClient {
	return &mockClient{
		config: make(map[string]string),
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

		// If requesting "forge", return all forge.* keys
		if key == "forge" {
			var result string
			for k, v := range m.config {
				result += fmt.Sprintf("forge.%s = %s\n", k, v)
			}
			return result, nil
		}

		// Otherwise return specific key
		if val, ok := m.config[key]; ok {
			return fmt.Sprintf("%s = %s", key, val), nil
		}
		return "", nil
	}

	if args[0] == "config" && args[1] == "set" && args[2] == "--repo" {
		key := args[3]
		value := args[4]

		// Extract the key name after "forge."
		switch key {
		case "forge.reviews":
			m.config["reviews"] = value
		case "forge.checks":
			m.config["checks"] = value
		case "forge.default-reviewer":
			m.config["default-reviewer"] = value
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
	return nil, fmt.Errorf("not implemented")
}

func (m *mockClient) Revs(ctx context.Context, revset string) ([]*jj.Rev, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockClient) Root(ctx context.Context) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (m *mockClient) RemoteURL(ctx context.Context, remote string) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (m *mockClient) GitDir(ctx context.Context) (string, error) {
	return "/fake/git/dir", nil
}

func TestParseReviewRecord(t *testing.T) {
	tests := []struct {
		input    string
		expected ReviewRecord
		wantErr  bool
	}{
		{
			input: "abc\npr/123\nhttp://url\nopen",
			expected: ReviewRecord{
				ChangeID: "abc",
				ForgeID:  "pr/123",
				URL:      "http://url",
				Status:   ReviewStateOpen,
			},
			wantErr: false,
		},
		{
			input:    "invalid",
			expected: ReviewRecord{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		got, err := ParseReviewRecord(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseReviewRecord(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if !tt.wantErr {
			if diff := cmp.Diff(tt.expected, got); diff != "" {
				t.Errorf("ParseReviewRecord(%q) mismatch (-want +got):\n%s", tt.input, diff)
			}
		}
	}
}

func TestConfigManager(t *testing.T) {
	mock := newMockClient()
	mgr := NewConfigManager(mock)

	rec1 := ReviewRecord{ChangeID: "c1", ForgeID: "f1", URL: "u1", Status: "s1"}
	rec2 := ReviewRecord{ChangeID: "c2", ForgeID: "f2", URL: "u2", Status: "s2"}

	// Test Add
	if err := mgr.AddReviewRecord(rec1); err != nil {
		t.Fatalf("AddReviewRecord failed: %v", err)
	}
	if err := mgr.AddReviewRecord(rec2); err != nil {
		t.Fatalf("AddReviewRecord failed: %v", err)
	}

	// Test Get
	records, err := mgr.GetReviewRecords()
	if err != nil {
		t.Fatalf("GetReviewRecords failed: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("expected 2 records, got %d", len(records))
	}

	// Test Update
	rec1Updated := rec1
	rec1Updated.Status = ReviewStateMerged
	if err := mgr.AddReviewRecord(rec1Updated); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	records, _ = mgr.GetReviewRecords()
	if len(records) != 2 {
		t.Errorf("expected 2 records after update, got %d", len(records))
	}
	found := false
	for _, r := range records {
		if r.ChangeID == "c1" {
			if r.Status != ReviewStateMerged {
				t.Errorf("expected status 'merged', got %q", r.Status)
			}
			found = true
		}
	}
	if !found {
		t.Error("updated record not found")
	}

	// Test Remove
	if err := mgr.RemoveReviewRecord("c2"); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	records, _ = mgr.GetReviewRecords()
	if len(records) != 1 {
		t.Errorf("expected 1 record after removal, got %d", len(records))
	}
	if records[0].ChangeID != "c1" {
		t.Errorf("expected c1 to remain, got %s", records[0].ChangeID)
	}
}

func TestGetDefaultReviewer(t *testing.T) {
	// Test: no config
	mock1 := newMockClient()
	mgr1 := NewConfigManager(mock1)
	reviewer, err := mgr1.GetDefaultReviewer()
	if err != nil {
		t.Fatalf("GetDefaultReviewer failed: %v", err)
	}
	if reviewer != "" {
		t.Errorf("expected empty reviewer, got %q", reviewer)
	}

	// Test: config with default-reviewer
	mock2 := newMockClient()
	mock2.config["default-reviewer"] = "\"test-reviewer\""
	mgr2 := NewConfigManager(mock2)
	reviewer, err = mgr2.GetDefaultReviewer()
	if err != nil {
		t.Fatalf("GetDefaultReviewer failed: %v", err)
	}
	if reviewer != "test-reviewer" {
		t.Errorf("expected reviewer 'test-reviewer', got %q", reviewer)
	}

	// Test: config without default-reviewer (empty)
	mock3 := newMockClient()
	mgr3 := NewConfigManager(mock3)
	reviewer, err = mgr3.GetDefaultReviewer()
	if err != nil {
		t.Fatalf("GetDefaultReviewer failed: %v", err)
	}
	if reviewer != "" {
		t.Errorf("expected empty reviewer, got %q", reviewer)
	}
}

func TestGetCheckCommand(t *testing.T) {
	// Test: no config
	mock1 := newMockClient()
	mgr1 := NewConfigManager(mock1)
	cmd, err := mgr1.GetCheckCommand()
	if err != nil {
		t.Fatalf("GetCheckCommand failed: %v", err)
	}
	if cmd != "" {
		t.Errorf("expected empty command, got %q", cmd)
	}

	// Test: config with check-command
	mock2 := newMockClient()
	mock2.config["check-command"] = "\"echo hello\""
	mgr2 := NewConfigManager(mock2)
	cmd, err = mgr2.GetCheckCommand()
	if err != nil {
		t.Fatalf("GetCheckCommand failed: %v", err)
	}
	if cmd != "echo hello" {
		t.Errorf("expected command 'echo hello', got %q", cmd)
	}
}

func TestCheckVerdictCRUD(t *testing.T) {
	mock := newMockClient()
	mgr := NewConfigManager(mock)

	v1 := CheckVerdict{ChangeID: "c1", Verdict: CheckVerdictPass, CommitID: "abc123"}
	v2 := CheckVerdict{ChangeID: "c2", Verdict: CheckVerdictFail, CommitID: "def456"}

	// Test Add
	if err := mgr.SetCheckVerdict(v1); err != nil {
		t.Fatalf("SetCheckVerdict failed: %v", err)
	}
	if err := mgr.SetCheckVerdict(v2); err != nil {
		t.Fatalf("SetCheckVerdict failed: %v", err)
	}

	// Test GetAll
	verdicts, err := mgr.GetCheckVerdicts()
	if err != nil {
		t.Fatalf("GetCheckVerdicts failed: %v", err)
	}
	if len(verdicts) != 2 {
		t.Errorf("expected 2 verdicts, got %d", len(verdicts))
	}

	// Test GetByChangeID
	got, err := mgr.GetCheckVerdictByChangeID("c1")
	if err != nil {
		t.Fatalf("GetCheckVerdictByChangeID failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected verdict, got nil")
	}
	if diff := cmp.Diff(v1, *got); diff != "" {
		t.Errorf("GetCheckVerdictByChangeID mismatch (-want +got):\n%s", diff)
	}

	// Test GetByChangeID not found
	got, err = mgr.GetCheckVerdictByChangeID("nonexistent")
	if err != nil {
		t.Fatalf("GetCheckVerdictByChangeID failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}

	// Test Update (upsert)
	v1Updated := CheckVerdict{ChangeID: "c1", Verdict: CheckVerdictFail, CommitID: "abc789"}
	if err := mgr.SetCheckVerdict(v1Updated); err != nil {
		t.Fatalf("SetCheckVerdict update failed: %v", err)
	}
	verdicts, err = mgr.GetCheckVerdicts()
	if err != nil {
		t.Fatalf("GetCheckVerdicts failed: %v", err)
	}
	if len(verdicts) != 2 {
		t.Errorf("expected 2 verdicts after update, got %d", len(verdicts))
	}
	got, err = mgr.GetCheckVerdictByChangeID("c1")
	if err != nil {
		t.Fatalf("GetCheckVerdictByChangeID failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected verdict, got nil")
	}
	if diff := cmp.Diff(v1Updated, *got); diff != "" {
		t.Errorf("updated verdict mismatch (-want +got):\n%s", diff)
	}
}

func TestParseCheckVerdict(t *testing.T) {
	tests := []struct {
		input    string
		expected CheckVerdict
		wantErr  bool
	}{
		{
			input:    "abc\npass\ncommit123",
			expected: CheckVerdict{ChangeID: "abc", Verdict: CheckVerdictPass, CommitID: "commit123"},
		},
		{
			input:    "xyz\nfail\ncommit456",
			expected: CheckVerdict{ChangeID: "xyz", Verdict: CheckVerdictFail, CommitID: "commit456"},
		},
		{
			input:    "def\nrunning\ncommit789",
			expected: CheckVerdict{ChangeID: "def", Verdict: CheckVerdictRunning, CommitID: "commit789"},
		},
		{
			input:   "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		got, err := ParseCheckVerdict(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseCheckVerdict(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if !tt.wantErr {
			if diff := cmp.Diff(tt.expected, got); diff != "" {
				t.Errorf("ParseCheckVerdict(%q) mismatch (-want +got):\n%s", tt.input, diff)
			}
		}
	}
}

func TestRemoveCheckVerdicts(t *testing.T) {
	t.Run("remove single from multiple", func(t *testing.T) {
		mock := newMockClient()
		mgr := NewConfigManager(mock)

		// Add 3 verdicts
		for _, v := range []CheckVerdict{
			{ChangeID: "c1", Verdict: CheckVerdictPass, CommitID: "aaa"},
			{ChangeID: "c2", Verdict: CheckVerdictFail, CommitID: "bbb"},
			{ChangeID: "c3", Verdict: CheckVerdictPass, CommitID: "ccc"},
		} {
			if err := mgr.SetCheckVerdict(v); err != nil {
				t.Fatalf("SetCheckVerdict failed: %v", err)
			}
		}

		// Remove c2
		if err := mgr.RemoveCheckVerdicts([]string{"c2"}); err != nil {
			t.Fatalf("RemoveCheckVerdicts failed: %v", err)
		}

		verdicts, err := mgr.GetCheckVerdicts()
		if err != nil {
			t.Fatalf("GetCheckVerdicts failed: %v", err)
		}
		if len(verdicts) != 2 {
			t.Fatalf("expected 2 verdicts, got %d", len(verdicts))
		}
		for _, v := range verdicts {
			if v.ChangeID == "c2" {
				t.Error("c2 should have been removed")
			}
		}
	})

	t.Run("remove multiple at once", func(t *testing.T) {
		mock := newMockClient()
		mgr := NewConfigManager(mock)

		for _, v := range []CheckVerdict{
			{ChangeID: "c1", Verdict: CheckVerdictPass, CommitID: "aaa"},
			{ChangeID: "c2", Verdict: CheckVerdictFail, CommitID: "bbb"},
			{ChangeID: "c3", Verdict: CheckVerdictPass, CommitID: "ccc"},
		} {
			if err := mgr.SetCheckVerdict(v); err != nil {
				t.Fatalf("SetCheckVerdict failed: %v", err)
			}
		}

		if err := mgr.RemoveCheckVerdicts([]string{"c1", "c3"}); err != nil {
			t.Fatalf("RemoveCheckVerdicts failed: %v", err)
		}

		verdicts, err := mgr.GetCheckVerdicts()
		if err != nil {
			t.Fatalf("GetCheckVerdicts failed: %v", err)
		}
		if len(verdicts) != 1 {
			t.Fatalf("expected 1 verdict, got %d", len(verdicts))
		}
		if verdicts[0].ChangeID != "c2" {
			t.Errorf("expected c2 to remain, got %s", verdicts[0].ChangeID)
		}
	})

	t.Run("no-op when not found", func(t *testing.T) {
		mock := newMockClient()
		mgr := NewConfigManager(mock)

		if err := mgr.SetCheckVerdict(CheckVerdict{ChangeID: "c1", Verdict: CheckVerdictPass, CommitID: "aaa"}); err != nil {
			t.Fatalf("SetCheckVerdict failed: %v", err)
		}

		// Remove nonexistent ID
		if err := mgr.RemoveCheckVerdicts([]string{"nonexistent"}); err != nil {
			t.Fatalf("RemoveCheckVerdicts failed: %v", err)
		}

		verdicts, err := mgr.GetCheckVerdicts()
		if err != nil {
			t.Fatalf("GetCheckVerdicts failed: %v", err)
		}
		if len(verdicts) != 1 {
			t.Fatalf("expected 1 verdict unchanged, got %d", len(verdicts))
		}

		// Verify no config set call was made for the no-op removal
		mock.mu.Lock()
		defer mock.mu.Unlock()
		// Count set calls: 1 from SetCheckVerdict, 0 from RemoveCheckVerdicts (no-op)
		var setCalls int
		for _, call := range mock.callLog {
			if len(call) >= 2 && call[0] == "config" && call[1] == "set" {
				setCalls++
			}
		}
		if setCalls != 1 {
			t.Errorf("expected 1 config set call (no-op removal should not write), got %d", setCalls)
		}
	})
}

func TestSetCheckVerdictsBatch(t *testing.T) {
	mock := newMockClient()
	mgr := NewConfigManager(mock)

	// Pre-populate one verdict.
	if err := mgr.SetCheckVerdict(CheckVerdict{ChangeID: "c1", Verdict: CheckVerdictPass, CommitID: "aaa"}); err != nil {
		t.Fatalf("SetCheckVerdict failed: %v", err)
	}

	// Batch: update c1, insert c2 and c3.
	batch := []CheckVerdict{
		{ChangeID: "c1", Verdict: CheckVerdictRunning, CommitID: "bbb"},
		{ChangeID: "c2", Verdict: CheckVerdictRunning, CommitID: "ccc"},
		{ChangeID: "c3", Verdict: CheckVerdictRunning, CommitID: "ddd"},
	}
	if err := mgr.SetCheckVerdicts(batch); err != nil {
		t.Fatalf("SetCheckVerdicts failed: %v", err)
	}

	verdicts, err := mgr.GetCheckVerdicts()
	if err != nil {
		t.Fatalf("GetCheckVerdicts failed: %v", err)
	}
	if len(verdicts) != 3 {
		t.Fatalf("expected 3 verdicts, got %d", len(verdicts))
	}

	// Verify c1 was updated (not duplicated).
	v1, err := mgr.GetCheckVerdictByChangeID("c1")
	if err != nil {
		t.Fatal(err)
	}
	if v1 == nil {
		t.Fatal("expected c1 verdict, got nil")
	}
	if v1.Verdict != CheckVerdictRunning || v1.CommitID != "bbb" {
		t.Errorf("c1: got verdict=%q commit=%q, want running/bbb", v1.Verdict, v1.CommitID)
	}

	// Verify c2 and c3 were inserted.
	for _, id := range []string{"c2", "c3"} {
		v, err := mgr.GetCheckVerdictByChangeID(id)
		if err != nil {
			t.Fatal(err)
		}
		if v == nil {
			t.Fatalf("expected %s verdict, got nil", id)
		}
		if v.Verdict != CheckVerdictRunning {
			t.Errorf("%s: got verdict=%q, want running", id, v.Verdict)
		}
	}

	// Count subprocess calls: initial SetCheckVerdict (2 calls) + SetCheckVerdicts (2 calls) + reads for verification.
	// The key thing is that SetCheckVerdicts only adds 2 calls (one read + one write), not 2*len(batch).
	mock.mu.Lock()
	defer mock.mu.Unlock()
	var setCalls int
	for _, call := range mock.callLog {
		if len(call) >= 2 && call[0] == "config" && call[1] == "set" {
			setCalls++
		}
	}
	// Expect exactly 2 config set calls: one from initial SetCheckVerdict, one from SetCheckVerdicts.
	if setCalls != 2 {
		t.Errorf("expected 2 config set calls, got %d", setCalls)
	}
}
