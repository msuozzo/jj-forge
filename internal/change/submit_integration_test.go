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

// Helper functions specific to submit tests
// Note: runCmd, runCmdOutput, writeFile, getChangeIDs, and getDescription
// are shared with upload_integration_test.go and defined there

func setupSubmitTest(t *testing.T) (tmpDir, remoteDir, repoDir string) {
	t.Helper()

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "jj-forge-submit-integration-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	remoteDir = filepath.Join(tmpDir, "remote.git")
	repoDir = filepath.Join(tmpDir, "repo")

	// Initialize bare remote repo
	if err := os.MkdirAll(remoteDir, 0755); err != nil {
		t.Fatalf("Failed to create remote dir: %v", err)
	}
	runCmd(t, remoteDir, "git", "init", "--bare")

	// Initialize jj repo
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("Failed to create repo dir: %v", err)
	}
	runCmd(t, repoDir, "jj", "git", "init")

	// Configure git author (required for push)
	runCmd(t, repoDir, "jj", "config", "set", "--repo", "user.name", "Test User")
	runCmd(t, repoDir, "jj", "config", "set", "--repo", "user.email", "test@example.com")

	// Add remote
	runCmd(t, repoDir, "jj", "git", "remote", "add", "og", remoteDir)
	runCmd(t, repoDir, "jj", "git", "fetch", "--remote", "og")

	return tmpDir, remoteDir, repoDir
}

func getRemoteCommits(t *testing.T, remoteDir, branch string) []string {
	t.Helper()

	// Use git log to list commits on branch
	output := runCmdOutput(t, remoteDir, "git", "log", "--format=%H", branch)
	if strings.TrimSpace(output) == "" {
		return []string{}
	}
	return strings.Split(strings.TrimSpace(output), "\n")
}

func hasTrailer(description, trailerKey string) bool {
	// Simple check: does description contain "trailerKey: "
	return strings.Contains(description, trailerKey+":")
}

func createCommitWithTrailer(t *testing.T, repoDir, message, trailer string) {
	t.Helper()

	// Create file and commit
	filename := strings.ReplaceAll(message, " ", "_") + ".txt"
	writeFile(t, filepath.Join(repoDir, filename), "content")

	// Commit with trailer in message
	fullMessage := message
	if trailer != "" {
		fullMessage = message + "\n\n" + trailer
	}
	runCmd(t, repoDir, "jj", "commit", "-m", fullMessage)
}

// Test functions

func TestSubmitIntegration_SingleCommit(t *testing.T) {
	// 1. Check jj availability
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj not found in PATH, skipping integration test")
	}

	// 2. Create temp directories
	tmpDir, remoteDir, repoDir := setupSubmitTest(t)
	defer os.RemoveAll(tmpDir)

	// 3. Create initial commit and push to establish remote main
	writeFile(t, filepath.Join(repoDir, "initial.txt"), "initial content")
	runCmd(t, repoDir, "jj", "commit", "-m", "Initial commit")

	// Create main bookmark and push it to remote
	runCmd(t, repoDir, "jj", "bookmark", "create", "main", "-r", "@-")
	runCmd(t, repoDir, "jj", "git", "push", "--bookmark", "main", "--allow-new")

	// 4. Create test commit on top of remote main
	writeFile(t, filepath.Join(repoDir, "file1.txt"), "content1")
	runCmd(t, repoDir, "jj", "commit", "-m", "feat: add file1")

	// 5. Execute Submit on the just-created commit (@-)
	client := jj.NewClient(repoDir)
	result, err := Submit(context.Background(), client, "@-", "og", "main")

	// 6. Verify no error
	if err != nil {
		t.Fatalf("Submit() failed: %v", err)
	}

	// 7. Verify result counts
	if result.Submitted != 1 {
		t.Errorf("Expected Submitted=1, got %d", result.Submitted)
	}

	// 8. Verify commit is on remote main
	remoteCommits := getRemoteCommits(t, remoteDir, "main")
	if len(remoteCommits) != 2 { // initial + new commit
		t.Errorf("Expected 2 commits on remote main, got %d", len(remoteCommits))
	}
}

func TestSubmitIntegration_ThreeCommitStack(t *testing.T) {
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj not found in PATH, skipping integration test")
	}

	tmpDir, remoteDir, repoDir := setupSubmitTest(t)
	defer os.RemoveAll(tmpDir)

	// Create and push initial commit to establish remote main
	writeFile(t, filepath.Join(repoDir, "initial.txt"), "initial content")
	runCmd(t, repoDir, "jj", "commit", "-m", "Initial commit")
	runCmd(t, repoDir, "jj", "bookmark", "create", "main", "-r", "@-")
	runCmd(t, repoDir, "jj", "git", "push", "--bookmark", "main", "--allow-new")

	// Create 3 commits in stack
	writeFile(t, filepath.Join(repoDir, "file1.txt"), "content1")
	runCmd(t, repoDir, "jj", "commit", "-m", "feat: add file1")

	writeFile(t, filepath.Join(repoDir, "file2.txt"), "content2")
	runCmd(t, repoDir, "jj", "commit", "-m", "feat: add file2")

	writeFile(t, filepath.Join(repoDir, "file3.txt"), "content3")
	runCmd(t, repoDir, "jj", "commit", "-m", "feat: add file3")

	// Execute Submit (use main@og..@- to get all commits between remote and parent of working copy)
	client := jj.NewClient(repoDir)
	result, err := Submit(context.Background(), client, "main@og..@-", "og", "main")

	// Verify no error
	if err != nil {
		t.Fatalf("Submit() failed: %v", err)
	}

	// Verify result counts
	if result.Submitted != 3 {
		t.Errorf("Expected Submitted=3, got %d", result.Submitted)
	}

	// Verify all commits are on remote main
	remoteCommits := getRemoteCommits(t, remoteDir, "main")
	if len(remoteCommits) != 4 { // initial + 3 new commits
		t.Errorf("Expected 4 commits on remote main, got %d", len(remoteCommits))
	}
}

func TestSubmitIntegration_EmptyRevset(t *testing.T) {
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj not found in PATH, skipping integration test")
	}

	tmpDir, remoteDir, repoDir := setupSubmitTest(t)
	defer os.RemoveAll(tmpDir)

	// Create and push initial commit
	writeFile(t, filepath.Join(repoDir, "initial.txt"), "initial content")
	runCmd(t, repoDir, "jj", "commit", "-m", "Initial commit")
	_ = getChangeIDs(t, repoDir) // Verify commit created
	runCmd(t, repoDir, "jj", "bookmark", "create", "main", "-r", "@-")
	runCmd(t, repoDir, "jj", "git", "push", "--bookmark", "main", "--allow-new")

	// Execute Submit with empty revset (no mutable commits)
	client := jj.NewClient(repoDir)
	result, err := Submit(context.Background(), client, "none()", "og", "main")

	// Verify no error
	if err != nil {
		t.Fatalf("Submit() with empty revset failed: %v", err)
	}

	// Verify result counts are zero
	if result.Submitted != 0 {
		t.Errorf("Expected Submitted=0, got %d", result.Submitted)
	}

	// Verify remote unchanged
	remoteCommits := getRemoteCommits(t, remoteDir, "main")
	if len(remoteCommits) != 1 { // only initial commit
		t.Errorf("Expected 1 commit on remote main, got %d", len(remoteCommits))
	}
}

func TestSubmitIntegration_NonLinearStackFails(t *testing.T) {
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj not found in PATH, skipping integration test")
	}

	tmpDir, remoteDir, repoDir := setupSubmitTest(t)
	defer os.RemoveAll(tmpDir)

	// Create and push initial commit
	writeFile(t, filepath.Join(repoDir, "initial.txt"), "initial content")
	runCmd(t, repoDir, "jj", "commit", "-m", "Initial commit")
	_ = getChangeIDs(t, repoDir) // Verify commit created
	runCmd(t, repoDir, "jj", "bookmark", "create", "main", "-r", "@-")
	runCmd(t, repoDir, "jj", "git", "push", "--bookmark", "main", "--allow-new")
	runCmd(t, repoDir, "jj", "git", "fetch", "--remote", "og")
	runCmd(t, repoDir, "jj", "git", "import")

	// Create commit A based on remote main
	writeFile(t, filepath.Join(repoDir, "fileA.txt"), "contentA")
	runCmd(t, repoDir, "jj", "commit", "-m", "feat: add A")
	changeIDs := getChangeIDs(t, repoDir)
	commitA := changeIDs[0]

	// Create a new change on top of the root (making it non-linear)
	runCmd(t, repoDir, "jj", "new", "root()")
	writeFile(t, filepath.Join(repoDir, "fileB.txt"), "contentB")
	runCmd(t, repoDir, "jj", "commit", "-m", "feat: add B (non-linear)")

	// Try to submit - should fail validation
	client := jj.NewClient(repoDir)
	result, err := Submit(context.Background(), client, "@-", "og", "main")

	// Verify error occurred
	if err == nil {
		t.Fatal("Expected Submit() to fail for non-linear stack, but it succeeded")
	}

	// Verify error message mentions validation
	if !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("Expected validation error, got: %v", err)
	}

	// Verify remote unchanged (still only has initial commit)
	remoteCommits := getRemoteCommits(t, remoteDir, "main")
	if len(remoteCommits) != 1 {
		t.Errorf("Expected remote to remain at 1 commit after failed submit, got %d", len(remoteCommits))
	}

	// Verify result is nil (since error occurred)
	if result != nil {
		t.Errorf("Expected nil result on error, got %+v", result)
	}

	// Suppress unused variable warning
	_ = commitA
}

func TestSubmitIntegration_NotBasedOnRemoteHeadFails(t *testing.T) {
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj not found in PATH, skipping integration test")
	}

	tmpDir, remoteDir, repoDir := setupSubmitTest(t)
	defer os.RemoveAll(tmpDir)

	// Create and push initial commit X
	writeFile(t, filepath.Join(repoDir, "initial.txt"), "initial content")
	runCmd(t, repoDir, "jj", "commit", "-m", "Initial commit X")
	changeIDsInitial := getChangeIDs(t, repoDir)
	commitX := changeIDsInitial[0]
	runCmd(t, repoDir, "jj", "bookmark", "create", "main", "-r", "@-")
	runCmd(t, repoDir, "jj", "git", "push", "--bookmark", "main", "--allow-new")

	// Create commit A based on current remote head
	writeFile(t, filepath.Join(repoDir, "fileA.txt"), "contentA")
	runCmd(t, repoDir, "jj", "commit", "-m", "feat: add A")
	changeIDsAfterA := getChangeIDs(t, repoDir)
	commitA := changeIDsAfterA[0]

	// Push another commit Y to remote to advance the remote head
	runCmd(t, repoDir, "jj", "new", commitX)
	writeFile(t, filepath.Join(repoDir, "fileY.txt"), "contentY")
	runCmd(t, repoDir, "jj", "commit", "-m", "feat: add Y (advances remote)")
	changeIDsAfterY := getChangeIDs(t, repoDir)
	commitY := changeIDsAfterY[0]
	// Move main bookmark to Y and push to advance remote
	runCmd(t, repoDir, "jj", "bookmark", "set", "main", "-r", commitY)
	runCmd(t, repoDir, "jj", "git", "push", "--bookmark", "main")

	// Now try to submit commit A, which is based on old remote head (X), not current (Y)
	// This should fail validation
	client := jj.NewClient(repoDir)
	result, err := Submit(context.Background(), client, commitA, "og", "main")

	// Verify error occurred
	if err == nil {
		t.Fatal("Expected Submit() to fail when commit not based on remote head, but it succeeded")
	}

	// Verify error message mentions validation
	if !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("Expected validation error, got: %v", err)
	}

	// Verify remote still points to Y (unchanged by failed submit)
	remoteCommits := getRemoteCommits(t, remoteDir, "main")
	if len(remoteCommits) != 2 { // X and Y
		t.Errorf("Expected remote to have 2 commits (X and Y), got %d", len(remoteCommits))
	}

	// Verify result is nil
	if result != nil {
		t.Errorf("Expected nil result on error, got %+v", result)
	}
}
