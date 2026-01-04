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
		if key == "forge.reviews" {
			m.config["reviews"] = value
		} else if key == "forge.default-reviewer" {
			m.config["default-reviewer"] = value
		} else {
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
				Status:   "open",
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
	rec1Updated.Status = "merged"
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
			if r.Status != "merged" {
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
