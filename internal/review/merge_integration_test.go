//go:build integration

package review

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Test helpers (mirroring internal/change/testutil.go)

func runCmd(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "JJ_CONFIG=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command %s %v failed: %v\noutput: %s", name, args, err, out)
	}
}

func runCmdOutput(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "JJ_CONFIG=")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("command %s %v failed: %v", name, args, err)
	}
	return string(out)
}

// runCmdCombinedOutput runs a command and returns combined stdout+stderr and any error.
// Does not fatal on error — use this when you expect the command to fail.
func runCmdCombinedOutput(t *testing.T, dir string, name string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "JJ_CONFIG=")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write file %s: %v", path, err)
	}
}

func setupMergeIntegrationTest(t *testing.T) (remoteDir, repoDir string) {
	t.Helper()

	tmpDir := t.TempDir()
	remoteDir = filepath.Join(tmpDir, "remote.git")
	repoDir = filepath.Join(tmpDir, "repo")

	// Initialize bare remote repo
	if err := os.MkdirAll(remoteDir, 0755); err != nil {
		t.Fatalf("failed to create remote dir: %v", err)
	}
	runCmd(t, remoteDir, "git", "init", "--bare")

	// Initialize jj repo
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}
	runCmd(t, repoDir, "jj", "git", "init")
	runCmd(t, repoDir, "jj", "config", "set", "--repo", "user.name", "Test User")
	runCmd(t, repoDir, "jj", "config", "set", "--repo", "user.email", "test@example.com")

	// Add remote
	runCmd(t, repoDir, "jj", "git", "remote", "add", "og", remoteDir)

	return remoteDir, repoDir
}

// setupPushedBookmark creates an initial commit on main, then a feature commit
// pushed as push-{changeID} to the "og" remote. Returns the change ID and bookmark name.
func setupPushedBookmark(t *testing.T, remoteDir, repoDir string) (changeID, bookmarkName string) {
	t.Helper()

	// Create initial commit and push main to establish remote
	writeFile(t, filepath.Join(repoDir, "init.txt"), "init")
	runCmd(t, repoDir, "jj", "commit", "-m", "initial")
	runCmd(t, repoDir, "jj", "bookmark", "create", "main", "-r", "@-")
	runCmd(t, repoDir, "jj", "git", "push", "--remote", "og", "--bookmark", "main")

	// Create feature commit and push via --change (mirrors real upload flow)
	writeFile(t, filepath.Join(repoDir, "feature.txt"), "feature")
	runCmd(t, repoDir, "jj", "commit", "-m", "feat: new feature")
	changeID = strings.TrimSpace(runCmdOutput(t, repoDir, "jj", "log", "--no-graph", "-r", "@-", "-T", "change_id.short()"))
	runCmd(t, repoDir, "jj", "git", "push", "--change", changeID, "--remote", "og")
	bookmarkName = "push-" + changeID

	// Fetch to establish tracking
	runCmd(t, repoDir, "jj", "git", "fetch", "--remote", "og")

	return changeID, bookmarkName
}

// simulateRemoteRefMove creates a new commit on the bare remote and moves the
// given ref to point at it, simulating a race where the remote ref changes
// between a fetch and a push (e.g. GitHub's async post-merge processing).
func simulateRemoteRefMove(t *testing.T, remoteDir, refName string) {
	t.Helper()
	tree := strings.TrimSpace(runCmdOutput(t, remoteDir, "git", "rev-parse", refName+"^{tree}"))
	newCommit := strings.TrimSpace(runCmdOutput(t, remoteDir, "git", "commit-tree", tree, "-m", "simulated post-merge ref update"))
	runCmd(t, remoteDir, "git", "update-ref", "refs/heads/"+refName, newCommit)
}

func TestMergeCleanup_PushDeleteFailsOnStaleRef(t *testing.T) {
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj not found in PATH")
	}

	remoteDir, repoDir := setupMergeIntegrationTest(t)
	_, bookmarkName := setupPushedBookmark(t, remoteDir, repoDir)

	// Simulate the cleanup fetch (sees no changes — replication lag).
	fetchOut, _ := runCmdCombinedOutput(t, repoDir, "jj", "git", "fetch", "--remote", "og", "--branch", bookmarkName)
	t.Logf("cleanup fetch output: %q", strings.TrimSpace(fetchOut))

	// Now simulate GitHub's async processing catching up: the remote ref
	// moves AFTER our fetch returned stale data.
	simulateRemoteRefMove(t, remoteDir, bookmarkName)

	// Delete bookmark locally (as Merge cleanup does).
	runCmd(t, repoDir, "jj", "bookmark", "delete", bookmarkName)

	// Attempt push deletion — should fail with stale info.
	output, err := runCmdCombinedOutput(t, repoDir, "jj", "git", "push", "--remote", "og", "--bookmark", bookmarkName)
	if err == nil {
		t.Fatal("expected push to fail with stale-info error, but it succeeded")
	}
	if !strings.Contains(strings.ToLower(output), "unexpectedly moved") && !strings.Contains(strings.ToLower(output), "stale") {
		t.Errorf("expected stale-info error, got: %s", output)
	}
}

func TestMergeCleanup_PushDeleteSucceedsAfterRefetch(t *testing.T) {
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj not found in PATH")
	}

	remoteDir, repoDir := setupMergeIntegrationTest(t)
	_, bookmarkName := setupPushedBookmark(t, remoteDir, repoDir)

	// Simulate the cleanup fetch (sees no changes — replication lag).
	runCmd(t, repoDir, "jj", "git", "fetch", "--remote", "og", "--branch", bookmarkName)

	// Remote ref moves after fetch (GitHub catches up).
	simulateRemoteRefMove(t, remoteDir, bookmarkName)

	// Delete bookmark locally.
	runCmd(t, repoDir, "jj", "bookmark", "delete", bookmarkName)

	// First push attempt fails (stale info).
	_, err := runCmdCombinedOutput(t, repoDir, "jj", "git", "push", "--remote", "og", "--bookmark", bookmarkName)
	if err == nil {
		t.Fatal("expected first push to fail with stale-info error, but it succeeded")
	}

	// Recovery: targeted re-fetch should detect the ref moved (not "Nothing changed.").
	fetchOut, fetchErr := runCmdCombinedOutput(t, repoDir, "jj", "git", "fetch", "--remote", "og", "--branch", bookmarkName)
	fetchOut = strings.TrimSpace(fetchOut)
	t.Logf("recovery fetch output: %q (err: %v)", fetchOut, fetchErr)
	if strings.Contains(fetchOut, "Nothing changed.") {
		t.Fatal("expected recovery fetch to detect changes, but got 'Nothing changed.'")
	}

	// Re-delete resolves the bookmark conflict (local=deleted vs remote=new-commit, jj#7722).
	runCmd(t, repoDir, "jj", "bookmark", "delete", bookmarkName)

	// Retry push — should succeed now.
	output, err := runCmdCombinedOutput(t, repoDir, "jj", "git", "push", "--remote", "og", "--bookmark", bookmarkName)
	if err != nil {
		t.Fatalf("expected push to succeed after recovery, but it failed: %v\noutput: %s", err, output)
	}
}
