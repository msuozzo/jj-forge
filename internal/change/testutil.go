package change

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func runCmd(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "JJ_CONFIG=") // Use empty config
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command %s %v failed: %v\noutput: %s", name, args, err, out)
	}
}

func runCmdOutput(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("command %s %v failed: %v", name, args, err)
	}
	return string(out)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write file %s: %v", path, err)
	}
}

func getChangeIDs(t *testing.T, repoDir string) []string {
	t.Helper()
	// Get change IDs in topological order (parents first)
	out := runCmdOutput(t, repoDir, "jj", "log", "--no-graph", "-r", "mutable()", "-T", `change_id.short()++"\n"`)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	slices.Reverse(lines)
	return lines
}

func getDescription(t *testing.T, repoDir, changeID string) string {
	t.Helper()
	out := runCmdOutput(t, repoDir, "jj", "log", "--no-graph", "-r", changeID, "-T", "description")
	return out
}
