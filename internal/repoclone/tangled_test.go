package repoclone

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/msuozzo/jj-forge/internal/cmd"
	"github.com/msuozzo/jj-forge/internal/forge/tangled"
	"github.com/msuozzo/jj-forge/internal/ui"
)

const tangledTestURL = "git@tangled.org:alice.example.com/my-repo"

// fakeTGExecutor answers `tg auth status` and `tg repo view` with canned JSON.
func fakeTGExecutor(t *testing.T, authJSON string, repoJSON map[string]string) cmd.Executor {
	t.Helper()
	return func(ctx context.Context, _ cmd.Opts, args ...string) (*cmd.Result, error) {
		args = args[1:] // strip binary name
		joined := strings.Join(args, " ")
		switch {
		case joined == "auth status --json":
			return &cmd.Result{Stdout: authJSON}, nil
		case len(args) >= 3 && args[0] == "repo" && args[1] == "view":
			resp, ok := repoJSON[args[2]]
			if !ok {
				return nil, errors.New("repo not found")
			}
			return &cmd.Result{Stdout: resp}, nil
		}
		t.Errorf("unexpected tg command: %s", joined)
		return nil, errors.New("unexpected tg command: " + joined)
	}
}

// fakeGitExecutor answers `git ls-remote --symref <url> HEAD` with the given branch.
func fakeGitExecutor(t *testing.T, branch string) cmd.Executor {
	t.Helper()
	return func(ctx context.Context, _ cmd.Opts, args ...string) (*cmd.Result, error) {
		if len(args) >= 3 && args[0] == "git" && args[1] == "ls-remote" {
			return &cmd.Result{Stdout: "ref: refs/heads/" + branch + "\tHEAD\nabc\tHEAD\n"}, nil
		}
		t.Errorf("unexpected git command: %v", args)
		return nil, errors.New("unexpected git command")
	}
}

const meAuth = `{"authenticated":true,"status":"active","did":"did:plc:me","handle":"alice.example.com"}`

func myRepoJSON() map[string]string {
	return map[string]string{
		"alice.example.com/my-repo": `{"name":"my-repo","uri":"at://did:plc:me/sh.tangled.repo/my-repo","author":"alice.example.com","knot":"knot1.tangled.sh","repoDid":"did:plc:repo"}`,
	}
}

func otherRepoJSON() map[string]string {
	return map[string]string{
		"alice.example.com/my-repo": `{"name":"my-repo","uri":"at://did:plc:someone/sh.tangled.repo/my-repo","author":"alice.example.com","knot":"knot1.tangled.sh","repoDid":"did:plc:repo"}`,
	}
}

func TestTangledRunner_OwnedRepo_MainWorkflow(t *testing.T) {
	jjExec, jjCmds := recordingJJExecutor()
	var buf bytes.Buffer
	u := ui.New(&buf, ui.ColorNever)
	cli := tangled.NewCLI(fakeTGExecutor(t, meAuth, myRepoJSON()))
	runner := NewTangledRunnerWithDeps(cli, fakeGitExecutor(t, "master"), jjExec, u)

	result, err := runner.Run(context.Background(), Params{
		URL:  tangledTestURL,
		Path: t.TempDir() + "/my-repo",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantResult := &Result{
		ClonePath:  result.ClonePath,
		Workflow:   WorkflowMain,
		ForkRemote: "og",
	}
	if diff := cmp.Diff(wantResult, result); diff != "" {
		t.Errorf("Result mismatch (-want +got):\n%s", diff)
	}

	absPath := result.ClonePath
	wantJJ := [][]string{
		{"jj", "git", "clone", tangledTestURL},
		{"jj", "-R", absPath, "git", "remote", "rename", "origin", "og"},
		{"jj", "-R", absPath, "config", "set", "--repo", "git.fetch", "og"},
		{"jj", "-R", absPath, "config", "set", "--repo", "git.push", "og"},
		{"jj", "-R", absPath, "config", "set", "--repo", `revset-aliases."trunk()"`, "master@og"},
	}
	if len(*jjCmds) != len(wantJJ) {
		t.Fatalf("got %d jj commands, want %d:\n%v", len(*jjCmds), len(wantJJ), *jjCmds)
	}
	for i, prefix := range wantJJ {
		if !hasPrefix((*jjCmds)[i], prefix) {
			t.Errorf("jj command %d = %v, want prefix %v", i, (*jjCmds)[i], prefix)
		}
	}
	if !strings.Contains(buf.String(), "Repository owned by you") {
		t.Errorf("expected ownership message, got:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "develop-on-main") {
		t.Errorf("expected develop-on-main summary, got:\n%s", buf.String())
	}
}

func TestTangledRunner_ExternalRepo_PRWorkflow(t *testing.T) {
	jjExec, jjCmds := recordingJJExecutor()
	var buf bytes.Buffer
	u := ui.New(&buf, ui.ColorNever)
	cli := tangled.NewCLI(fakeTGExecutor(t, meAuth, otherRepoJSON()))
	runner := NewTangledRunnerWithDeps(cli, fakeGitExecutor(t, "main"), jjExec, u)

	result, err := runner.Run(context.Background(), Params{
		URL:           tangledTestURL,
		Path:          t.TempDir() + "/my-repo",
		TrackBranches: []string{"push-*"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantResult := &Result{
		ClonePath:    result.ClonePath,
		Workflow:     WorkflowPR,
		ForkRemote:   "og",
		UpstreamName: "up",
	}
	if diff := cmp.Diff(wantResult, result); diff != "" {
		t.Errorf("Result mismatch (-want +got):\n%s", diff)
	}

	absPath := result.ClonePath
	wantJJ := [][]string{
		{"jj", "git", "clone", tangledTestURL},
		{"jj", "-R", absPath, "git", "remote", "rename", "origin", "og"},
		{"jj", "-R", absPath, "git", "remote", "add", "up", tangledTestURL},
		{"jj", "-R", absPath, "git", "fetch", "--remote", "up"},
		{"jj", "-R", absPath, "config", "set", "--repo", "git.fetch", "['up', 'og']"},
		{"jj", "-R", absPath, "config", "set", "--repo", "git.push", "og"},
		{"jj", "-R", absPath, "bookmark", "track", "push-*", "--remote", "og"},
		{"jj", "-R", absPath, "config", "set", "--repo", `revset-aliases."trunk()"`, "main@up"},
	}
	if len(*jjCmds) != len(wantJJ) {
		t.Fatalf("got %d jj commands, want %d:\n%v", len(*jjCmds), len(wantJJ), *jjCmds)
	}
	for i, prefix := range wantJJ {
		if !hasPrefix((*jjCmds)[i], prefix) {
			t.Errorf("jj command %d = %v, want prefix %v", i, (*jjCmds)[i], prefix)
		}
	}
	if !strings.Contains(buf.String(), "Repository owned by alice.example.com") {
		t.Errorf("expected ownership message, got:\n%s", buf.String())
	}
}

func TestTangledRunner_NotLoggedIn(t *testing.T) {
	var buf bytes.Buffer
	u := ui.New(&buf, ui.ColorNever)
	cli := tangled.NewCLI(fakeTGExecutor(t, `{"authenticated":false}`, nil))
	runner := NewTangledRunnerWithDeps(cli, nil, nil, u)

	_, err := runner.Run(context.Background(), Params{URL: tangledTestURL})
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("expected not-logged-in error, got %v", err)
	}
}

func TestTangledRunner_RepoNotFound(t *testing.T) {
	var buf bytes.Buffer
	u := ui.New(&buf, ui.ColorNever)
	cli := tangled.NewCLI(fakeTGExecutor(t, meAuth, map[string]string{}))
	runner := NewTangledRunnerWithDeps(cli, nil, nil, u)

	_, err := runner.Run(context.Background(), Params{URL: tangledTestURL})
	if err == nil || !strings.Contains(err.Error(), "failed to view repository") {
		t.Fatalf("expected repo lookup error, got %v", err)
	}
}

func TestTangledRunner_BareDIDURL(t *testing.T) {
	var buf bytes.Buffer
	u := ui.New(&buf, ui.ColorNever)
	runner := NewTangledRunnerWithDeps(nil, nil, nil, u)

	_, err := runner.Run(context.Background(), Params{URL: "https://tangled.org/did:plc:onlyrepo"})
	if err == nil || !strings.Contains(err.Error(), "invalid Tangled repository URL") {
		t.Fatalf("expected URL error, got %v", err)
	}
}
