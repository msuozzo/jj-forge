package repoclone

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/msuozzo/jj-forge/internal/cmd"
	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/ui"
)

// fakeGHExecutor builds a gh executor from a username and a map of
// API path ("repos/owner/name") → JSON response. Missing entries return 404.
func fakeGHExecutor(t *testing.T, user string, repos map[string]string) cmd.Executor {
	t.Helper()
	return func(ctx context.Context, _ cmd.Opts, args ...string) (string, error) {
		args = args[1:] // strip binary name
		if len(args) >= 2 && args[0] == "api" && args[1] == "user" {
			return user + "\n", nil
		}
		if len(args) >= 2 && args[0] == "api" && strings.HasPrefix(args[1], "repos/") {
			resp, ok := repos[args[1]]
			if !ok {
				return "", fmt.Errorf("gh command failed: 404 Not Found")
			}
			return resp, nil
		}
		t.Errorf("unexpected gh command: %s", strings.Join(args, " "))
		return "", fmt.Errorf("unexpected gh command: %s", strings.Join(args, " "))
	}
}

// recordingJJExecutor returns a jj executor that records all commands.
func recordingJJExecutor() (cmd.Executor, *[][]string) {
	var cmds [][]string
	exec := func(ctx context.Context, _ cmd.Opts, args ...string) (string, error) {
		cmds = append(cmds, args)
		return "", nil
	}
	return exec, &cmds
}

func TestDetermineWorkflow(t *testing.T) {
	tests := []struct {
		name     string
		analysis *RepoAnalysis
		want     WorkflowType
	}{
		{
			name:     "my non-fork → main workflow",
			analysis: &RepoAnalysis{IsMine: true, IsFork: false},
			want:     WorkflowMain,
		},
		{
			name:     "my fork → PR workflow",
			analysis: &RepoAnalysis{IsMine: true, IsFork: true},
			want:     WorkflowPR,
		},
		{
			name:     "external repo → PR workflow",
			analysis: &RepoAnalysis{IsMine: false, IsFork: false},
			want:     WorkflowPR,
		},
		{
			name:     "external fork → PR workflow",
			analysis: &RepoAnalysis{IsMine: false, IsFork: true},
			want:     WorkflowPR,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetermineWorkflow(tt.analysis)
			if got != tt.want {
				t.Errorf("DetermineWorkflow() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetAuthenticatedUser(t *testing.T) {
	executor := fakeGHExecutor(t, "testuser", nil)
	client := NewGitHubClientWithExecutor(executor)
	user, err := client.GetAuthenticatedUser(context.Background())
	if err != nil {
		t.Fatalf("GetAuthenticatedUser() error = %v", err)
	}
	if user != "testuser" {
		t.Errorf("GetAuthenticatedUser() = %v, want testuser", user)
	}
}

func TestAnalyzeRepository(t *testing.T) {
	tests := []struct {
		name  string
		url   string
		repos map[string]string
		want  *RepoAnalysis
	}{
		{
			name: "personal non-fork",
			url:  "git@github.com:testuser/my-project.git",
			repos: map[string]string{
				"repos/testuser/my-project": `{"fork": false, "parent_owner": null, "parent_name": null, "ssh_url": "git@github.com:testuser/my-project.git", "clone_url": "https://github.com/testuser/my-project.git"}`,
			},
			want: &RepoAnalysis{
				Owner: "testuser", Name: "my-project",
				Exists: true, IsMine: true,
				SSHURL: "git@github.com:testuser/my-project.git", HTTPSURL: "https://github.com/testuser/my-project.git",
			},
		},
		{
			name: "personal fork",
			url:  "git@github.com:testuser/forked-project.git",
			repos: map[string]string{
				"repos/testuser/forked-project": `{"fork": true, "parent_owner": "upstream-owner", "parent_name": "forked-project", "ssh_url": "git@github.com:testuser/forked-project.git", "clone_url": "https://github.com/testuser/forked-project.git"}`,
			},
			want: &RepoAnalysis{
				Owner: "testuser", Name: "forked-project",
				Exists: true, IsMine: true, IsFork: true,
				SSHURL:   "git@github.com:testuser/forked-project.git",
				HTTPSURL: "https://github.com/testuser/forked-project.git",
				Parent:   &forge.RepoInfo{Owner: "upstream-owner", Name: "forked-project"},
			},
		},
		{
			name: "external repo",
			url:  "git@github.com:external/project.git",
			repos: map[string]string{
				"repos/external/project": `{"fork": false, "parent_owner": null, "parent_name": null, "ssh_url": "git@github.com:external/project.git", "clone_url": "https://github.com/external/project.git"}`,
			},
			want: &RepoAnalysis{
				Owner: "external", Name: "project",
				Exists: true,
				SSHURL: "git@github.com:external/project.git", HTTPSURL: "https://github.com/external/project.git",
			},
		},
		{
			name:  "non-existent personal",
			url:   "git@github.com:testuser/new-project.git",
			repos: map[string]string{}, // 404
			want: &RepoAnalysis{
				Owner: "testuser", Name: "new-project",
				IsMine: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewGitHubClientWithExecutor(fakeGHExecutor(t, "testuser", tt.repos))
			got, err := client.AnalyzeRepository(context.Background(), tt.url)
			if err != nil {
				t.Fatalf("AnalyzeRepository() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("AnalyzeRepository() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFindMyFork(t *testing.T) {
	tests := []struct {
		name          string
		upstreamOwner string
		upstreamName  string
		repos         map[string]string
		want          *RepoAnalysis // nil = not found
	}{
		{
			name:          "found",
			upstreamOwner: "upstream-owner",
			upstreamName:  "upstream-repo",
			repos: map[string]string{
				"repos/testuser/upstream-repo": `{"fork": true, "parent_owner": "upstream-owner", "parent_name": "upstream-repo", "ssh_url": "git@github.com:testuser/upstream-repo.git", "clone_url": "https://github.com/testuser/upstream-repo.git"}`,
			},
			want: &RepoAnalysis{
				Owner: "testuser", Name: "upstream-repo",
				Exists: true, IsMine: true, IsFork: true,
				SSHURL:   "git@github.com:testuser/upstream-repo.git",
				HTTPSURL: "https://github.com/testuser/upstream-repo.git",
				Parent:   &forge.RepoInfo{Owner: "upstream-owner", Name: "upstream-repo"},
			},
		},
		{
			name:          "not found",
			upstreamOwner: "upstream-owner",
			upstreamName:  "upstream-repo",
			repos:         map[string]string{}, // 404
			want:          nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewGitHubClientWithExecutor(fakeGHExecutor(t, "testuser", tt.repos))
			got, err := client.FindMyFork(context.Background(), tt.upstreamOwner, tt.upstreamName)
			if err != nil {
				t.Fatalf("FindMyFork() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("FindMyFork() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// FakePrompter for testing
type FakePrompter struct {
	confirmResponses []bool
	chooseResponses  []int
	confirmIndex     int
	chooseIndex      int
}

func (f *FakePrompter) Confirm(prompt string, defaultYes bool) (bool, error) {
	if f.confirmIndex >= len(f.confirmResponses) {
		return defaultYes, nil
	}
	resp := f.confirmResponses[f.confirmIndex]
	f.confirmIndex++
	return resp, nil
}

func (f *FakePrompter) Choose(prompt string, options []string, defaultIndex int) (int, error) {
	if f.chooseIndex >= len(f.chooseResponses) {
		return defaultIndex, nil
	}
	resp := f.chooseResponses[f.chooseIndex]
	f.chooseIndex++
	return resp, nil
}

// hasPrefix reports whether args starts with prefix.
func hasPrefix(args, prefix []string) bool {
	if len(args) < len(prefix) {
		return false
	}
	for i, p := range prefix {
		if args[i] != p {
			return false
		}
	}
	return true
}

func TestRunner(t *testing.T) {
	tests := []struct {
		name       string
		repos      map[string]string
		params     Params
		wantErr    string     // substring match; empty = expect success
		wantResult *Result    // nil when wantErr is set
		wantJJ     [][]string // expected jj command prefixes, in order
	}{
		{
			name: "personal non-fork",
			repos: map[string]string{
				"repos/testuser/my-project": `{"fork": false, "parent_owner": null, "parent_name": null, "ssh_url": "git@github.com:testuser/my-project.git", "clone_url": "https://github.com/testuser/my-project.git", "default_branch": "main"}`,
			},
			params: Params{
				URL:            "git@github.com:testuser/my-project.git",
				Path:           "/tmp/test-clone",
				ForkRemote:     "og",
				UpstreamRemote: "up",
			},
			wantResult: &Result{
				ClonePath: "/tmp/test-clone", Workflow: WorkflowMain, ForkRemote: "og",
			},
			wantJJ: [][]string{
				{"jj", "git", "clone"},
				{"jj", "-R", "/tmp/test-clone", "git", "remote", "rename"},
				{"jj", "-R", "/tmp/test-clone", "config", "set", "--repo", "git.push", "og"},
				{"jj", "-R", "/tmp/test-clone", "config", "set", "--repo", "git.fetch", "og"},
				{"jj", "-R", "/tmp/test-clone", "config", "set", "--repo", `revset-aliases."trunk()"`, "main@og"},
			},
		},
		{
			name: "personal fork",
			repos: map[string]string{
				"repos/testuser/forked-project":       `{"fork": true, "parent_owner": "upstream-owner", "parent_name": "forked-project", "ssh_url": "git@github.com:testuser/forked-project.git", "clone_url": "https://github.com/testuser/forked-project.git", "default_branch": "main"}`,
				"repos/upstream-owner/forked-project": `{"ssh_url": "git@github.com:upstream-owner/forked-project.git", "clone_url": "https://github.com/upstream-owner/forked-project.git", "default_branch": "main"}`,
			},
			params: Params{
				URL:            "git@github.com:testuser/forked-project.git",
				Path:           "/tmp/test-clone",
				ForkRemote:     "og",
				UpstreamRemote: "up",
			},
			wantResult: &Result{
				ClonePath: "/tmp/test-clone", Workflow: WorkflowPR, ForkRemote: "og", UpstreamName: "up",
			},
			wantJJ: [][]string{
				{"jj", "git", "clone"},
				{"jj", "-R", "/tmp/test-clone", "git", "remote", "rename"},
				{"jj", "-R", "/tmp/test-clone", "config", "set", "--repo", "git.push", "og"},
				{"jj", "-R", "/tmp/test-clone", "git", "remote", "add"},
				{"jj", "-R", "/tmp/test-clone", "git", "fetch", "--remote", "up"},
				{"jj", "-R", "/tmp/test-clone", "config", "set", "--repo", "git.fetch", "['up', 'og']"},
				{"jj", "-R", "/tmp/test-clone", "config", "set", "--repo", `revset-aliases."trunk()"`, "main@up"},
			},
		},
		{
			name: "external repo with existing fork",
			repos: map[string]string{
				"repos/external/project": `{"fork": false, "parent_owner": null, "parent_name": null, "ssh_url": "git@github.com:external/project.git", "clone_url": "https://github.com/external/project.git", "default_branch": "main"}`,
				"repos/testuser/project": `{"fork": true, "parent_owner": "external", "parent_name": "project", "ssh_url": "git@github.com:testuser/project.git", "clone_url": "https://github.com/testuser/project.git", "default_branch": "main"}`,
			},
			params: Params{
				URL:            "git@github.com:external/project.git",
				Path:           "/tmp/test-clone",
				ForkRemote:     "og",
				UpstreamRemote: "up",
			},
			wantResult: &Result{
				ClonePath: "/tmp/test-clone", Workflow: WorkflowPR, ForkRemote: "og", UpstreamName: "up",
			},
			wantJJ: [][]string{
				{"jj", "git", "clone"},
				{"jj", "-R", "/tmp/test-clone", "git", "remote", "rename"},
				{"jj", "-R", "/tmp/test-clone", "config", "set", "--repo", "git.push", "og"},
				{"jj", "-R", "/tmp/test-clone", "git", "remote", "add"},
				{"jj", "-R", "/tmp/test-clone", "git", "fetch", "--remote", "up"},
				{"jj", "-R", "/tmp/test-clone", "config", "set", "--repo", "git.fetch", "['up', 'og']"},
				{"jj", "-R", "/tmp/test-clone", "config", "set", "--repo", `revset-aliases."trunk()"`, "main@up"},
			},
		},
		{
			name: "external repo with --no-fork",
			repos: map[string]string{
				"repos/external/project": `{"fork": false, "parent_owner": null, "parent_name": null, "ssh_url": "git@github.com:external/project.git", "clone_url": "https://github.com/external/project.git"}`,
			},
			params: Params{
				URL:    "git@github.com:external/project.git",
				Path:   "/tmp/test-clone",
				NoFork: true,
			},
			wantErr: "external repository requires fork",
		},
		{
			name:  "non-existent external",
			repos: map[string]string{}, // 404
			params: Params{
				URL:  "git@github.com:external/project.git",
				Path: "/tmp/test-clone",
			},
			wantErr: "doesn't exist and isn't owned by you",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jjExec, jjCmds := recordingJJExecutor()
			var buf bytes.Buffer
			u := ui.New(&buf, ui.ColorNever)
			runner := NewRunnerWithDeps(
				NewGitHubClientWithExecutor(fakeGHExecutor(t, "testuser", tt.repos)),
				jjExec,
				&FakePrompter{},
				u,
			)
			result, err := runner.Run(context.Background(), tt.params)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got: %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if diff := cmp.Diff(tt.wantResult, result); diff != "" {
				t.Errorf("Run() result mismatch (-want +got):\n%s", diff)
			}

			if len(*jjCmds) < len(tt.wantJJ) {
				t.Fatalf("got %d jj commands, want at least %d", len(*jjCmds), len(tt.wantJJ))
			}
			for i, prefix := range tt.wantJJ {
				if !hasPrefix((*jjCmds)[i], prefix) {
					t.Errorf("jj command %d = %v, want prefix %v", i, (*jjCmds)[i], prefix)
				}
			}
		})
	}
}
