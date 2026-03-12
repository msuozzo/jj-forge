package repoclone

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/msuozzo/jj-forge/internal/ui"
)

func TestSSMRunner_InvalidURLFormat(t *testing.T) {
	var buf bytes.Buffer
	u := ui.New(&buf, ui.ColorNever)
	runner := NewSSMRunnerWithDeps(nil, u)
	_, err := runner.Run(context.Background(), Params{
		URL: "https://inst-897099121057.us-central1.sourcemanager.dev/ssci-demos/repo",
	})
	if err == nil {
		t.Fatal("expected error for inst- URL, got nil")
	}
	if !strings.Contains(err.Error(), "not in a git-compatible format") {
		t.Errorf("expected helpful error message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "-git or -ssh subdomain") {
		t.Errorf("expected error to mention -git or -ssh subdomain, got: %v", err)
	}
}

func TestSSMRunner_HappyPath(t *testing.T) {
	jjExec, jjCmds := recordingJJExecutor()
	var buf bytes.Buffer
	u := ui.New(&buf, ui.ColorNever)
	runner := NewSSMRunnerWithDeps(jjExec, u)

	result, err := runner.Run(context.Background(), Params{
		URL:  "https://loc-git.loc.sourcemanager.dev/proj/my-repo",
		Path: t.TempDir() + "/my-repo",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantResult := &Result{
		ClonePath:    result.ClonePath, // uses TempDir so match dynamically
		Workflow:     WorkflowPR,
		ForkRemote:   "og",
		UpstreamName: "up",
	}
	if diff := cmp.Diff(wantResult, result); diff != "" {
		t.Errorf("Result mismatch (-want +got):\n%s", diff)
	}

	absPath := result.ClonePath
	wantJJ := [][]string{
		{"jj", "git", "clone"},
		{"jj", "-R", absPath, "git", "remote", "rename", "origin", "og"},
		{"jj", "-R", absPath, "git", "remote", "add", "up"},
		{"jj", "-R", absPath, "config", "set", "--repo", "git.fetch"},
		{"jj", "-R", absPath, "config", "set", "--repo", "git.push", "og"},
		{"jj", "-R", absPath, "config", "set", "--repo", `revset-aliases."trunk()"`, "main@up"},
	}
	if len(*jjCmds) < len(wantJJ) {
		t.Fatalf("got %d jj commands, want at least %d", len(*jjCmds), len(wantJJ))
	}
	for i, prefix := range wantJJ {
		if !hasPrefix((*jjCmds)[i], prefix) {
			t.Errorf("jj command %d = %v, want prefix %v", i, (*jjCmds)[i], prefix)
		}
	}
}

func TestSSMRunner_TrackBranches(t *testing.T) {
	jjExec, jjCmds := recordingJJExecutor()
	var buf bytes.Buffer
	u := ui.New(&buf, ui.ColorNever)
	runner := NewSSMRunnerWithDeps(jjExec, u)

	result, err := runner.Run(context.Background(), Params{
		URL:           "https://loc-git.loc.sourcemanager.dev/proj/my-repo",
		Path:          t.TempDir() + "/my-repo",
		TrackBranches: []string{"push-*", "dev-*"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	absPath := result.ClonePath
	wantJJ := [][]string{
		{"jj", "git", "clone"},
		{"jj", "-R", absPath, "git", "remote", "rename", "origin", "og"},
		{"jj", "-R", absPath, "git", "remote", "add", "up"},
		{"jj", "-R", absPath, "config", "set", "--repo", "git.fetch"},
		{"jj", "-R", absPath, "config", "set", "--repo", "git.push", "og"},
		{"jj", "-R", absPath, "bookmark", "track", "push-*", "dev-*", "--remote", "og"},
		{"jj", "-R", absPath, "config", "set", "--repo", `revset-aliases."trunk()"`, "main@up"},
	}
	if len(*jjCmds) < len(wantJJ) {
		t.Fatalf("got %d jj commands, want at least %d", len(*jjCmds), len(wantJJ))
	}
	for i, prefix := range wantJJ {
		if !hasPrefix((*jjCmds)[i], prefix) {
			t.Errorf("jj command %d = %v, want prefix %v", i, (*jjCmds)[i], prefix)
		}
	}
}
