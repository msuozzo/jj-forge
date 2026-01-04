//go:build integration

package change

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/msuozzo/jj-forge/internal/jj"
)

// TestUploadIntegration tests the upload flow with a real jj repo.
// Run with: go test -tags=integration ./internal/stack/
func TestUploadIntegration(t *testing.T) {
	// Check jj is available
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj not found in PATH, skipping integration test")
	}

	// Create temp directory for test
	tmpDir, err := os.MkdirTemp("", "jj-forge-integration-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	remoteDir := filepath.Join(tmpDir, "remote.git")
	repoDir := filepath.Join(tmpDir, "repo")

	// Initialize bare git repo as remote
	if err := os.MkdirAll(remoteDir, 0755); err != nil {
		t.Fatalf("failed to create remote dir: %v", err)
	}
	runCmd(t, remoteDir, "git", "init", "--bare")

	// Initialize jj repo
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}
	runCmd(t, repoDir, "jj", "git", "init")

	// Configure author for commits (required for push)
	runCmd(t, repoDir, "jj", "config", "set", "--repo", "user.name", "Test User")
	runCmd(t, repoDir, "jj", "config", "set", "--repo", "user.email", "test@example.com")

	// Add remote as "og"
	runCmd(t, repoDir, "jj", "git", "remote", "add", "og", remoteDir)

	// Create first commit
	writeFile(t, filepath.Join(repoDir, "file1.txt"), "content1")
	runCmd(t, repoDir, "jj", "commit", "-m", "feat: add file1")

	// Create second commit
	writeFile(t, filepath.Join(repoDir, "file2.txt"), "content2")
	runCmd(t, repoDir, "jj", "commit", "-m", "feat: add file2")

	// Create third commit
	writeFile(t, filepath.Join(repoDir, "file3.txt"), "content3")
	runCmd(t, repoDir, "jj", "commit", "-m", "feat: add file3")

	// Get change IDs for verification
	changeIDs := getChangeIDs(t, repoDir)
	if len(changeIDs) < 3 {
		t.Fatalf("expected at least 3 mutable commits, got %d", len(changeIDs))
	}

	// Run upload
	ctx := context.Background()
	client := jj.NewClient(repoDir)
	result, err := Upload(ctx, client, "mutable()", "og")
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if result.Pushed < 3 {
		t.Errorf("expected at least 3 pushes, got %d", result.Pushed)
	}

	// Verify trailers
	// First commit (on root) should have no forge-parent
	desc1 := getDescription(t, repoDir, changeIDs[0])
	if strings.Contains(desc1, "forge-parent") {
		t.Errorf("first commit should not have forge-parent trailer, got: %s", desc1)
	}

	// Second commit should have forge-parent pointing to first
	desc2 := getDescription(t, repoDir, changeIDs[1])
	expectedTrailer := "forge-parent: " + changeIDs[0]
	if !strings.Contains(desc2, expectedTrailer) {
		t.Errorf("second commit should have %q, got: %s", expectedTrailer, desc2)
	}

	// Third commit should have forge-parent pointing to second
	desc3 := getDescription(t, repoDir, changeIDs[2])
	expectedTrailer = "forge-parent: " + changeIDs[1]
	if !strings.Contains(desc3, expectedTrailer) {
		t.Errorf("third commit should have %q, got: %s", expectedTrailer, desc3)
	}

	// Verify branches exist on remote
	remoteRefs := runCmdOutput(t, remoteDir, "git", "branch", "-a")
	for _, cid := range changeIDs {
		// jj creates branches like "push-<change_id_prefix>"
		if !strings.Contains(remoteRefs, cid[:8]) {
			t.Logf("warning: branch for %s may not exist on remote (refs: %s)", cid, remoteRefs)
		}
	}
}

func TestUploadIntegration_Idempotent(t *testing.T) {
	// Check jj is available
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj not found in PATH, skipping integration test")
	}

	tmpDir, err := os.MkdirTemp("", "jj-forge-integration-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	remoteDir := filepath.Join(tmpDir, "remote.git")
	repoDir := filepath.Join(tmpDir, "repo")

	// Setup
	os.MkdirAll(remoteDir, 0755)
	runCmd(t, remoteDir, "git", "init", "--bare")
	os.MkdirAll(repoDir, 0755)
	runCmd(t, repoDir, "jj", "git", "init")
	runCmd(t, repoDir, "jj", "config", "set", "--repo", "user.name", "Test User")
	runCmd(t, repoDir, "jj", "config", "set", "--repo", "user.email", "test@example.com")
	runCmd(t, repoDir, "jj", "git", "remote", "add", "og", remoteDir)

	// Create commits
	writeFile(t, filepath.Join(repoDir, "file1.txt"), "content1")
	runCmd(t, repoDir, "jj", "commit", "-m", "feat: add file1")
	writeFile(t, filepath.Join(repoDir, "file2.txt"), "content2")
	runCmd(t, repoDir, "jj", "commit", "-m", "feat: add file2")

	ctx := context.Background()
	client := jj.NewClient(repoDir)

	// First upload
	result1, err := Upload(ctx, client, "mutable()", "og")
	if err != nil {
		t.Fatalf("first Upload() error = %v", err)
	}
	if result1.Pushed == 0 {
		t.Error("first upload should push commits")
	}

	changeIDs := getChangeIDs(t, repoDir)
	desc1Before := getDescription(t, repoDir, changeIDs[1])

	// Second upload should skip already-synced commits
	result2, err := Upload(ctx, client, "mutable()", "og")
	if err != nil {
		t.Fatalf("second Upload() error = %v", err)
	}
	if result2.Pushed != 0 {
		t.Errorf("second upload should not push (already synced), but pushed %d", result2.Pushed)
	}
	if result2.SkippedSynced == 0 {
		t.Error("second upload should skip synced commits")
	}

	desc1After := getDescription(t, repoDir, changeIDs[1])
	if desc1Before != desc1After {
		t.Errorf("description changed after idempotent upload:\nbefore: %s\nafter: %s", desc1Before, desc1After)
	}
}
