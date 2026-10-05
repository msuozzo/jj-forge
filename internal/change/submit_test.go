package change

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/msuozzo/jj-forge/internal/cmd"
	"github.com/msuozzo/jj-forge/internal/jjtest"
	"github.com/msuozzo/jj-forge/internal/ui"
)

var trunkConfigGet = []string{"config", "get", `revset-aliases."trunk()"`}

func logArgs(revset string) []string {
	return []string{"log", "--no-graph", "--template", templateMatcher, "-r", revset}
}

func TestSubmit_TrunkTarget(t *testing.T) {
	// With no remote or branch given and trunk() at master@og, submit
	// fast-forwards master on og.
	repo := jjtest.NewFakeRepo()
	repo.AddCommits(
		jjtest.Commit{ID: "mmmmmmmmmmmm", Parents: []string{"root"}, Description: "initial\n", RemoteBookmarks: []string{"og/master"}},
		jjtest.Commit{ID: "aaaaaaaaaaaa", Parents: []string{"mmmmmmmmmmmm"}, IsMutable: true, Description: "feat: A\n"},
	)

	scenario := jjtest.NewScenario(t, repo,
		jjtest.Call{
			Args:   trunkConfigGet,
			Output: func(*jjtest.FakeRepo) string { return "master@og\n" },
		},
		jjtest.Call{Args: []string{"git", "fetch", "--remote", "og"}},
		jjtest.Call{Args: logArgs("master@og"), Output: jjtest.LogOutput("mmmmmmmmmmmm")},
		jjtest.Call{Args: logArgs("@-"), Output: jjtest.LogOutput("aaaaaaaaaaaa")},
		jjtest.Call{Args: logArgs("parents(@-)~(@-)"), Output: jjtest.LogOutput("mmmmmmmmmmmm")},
		jjtest.Call{Args: []string{"bookmark", "set", "master", "-r", "aaaaaaaaaaaa"}},
		jjtest.Call{Args: []string{"git", "push", "--bookmark", "master", "--remote", "og"}},
		jjtest.Call{Args: []string{"git", "fetch", "--remote", "og"}},
		jjtest.Call{Args: logArgs("master@og"), Output: jjtest.LogOutput("aaaaaaaaaaaa")},
	)

	result, err := Submit(context.Background(), scenario.Client(), nil, "@-", "", "", testUI)
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if result.Submitted != 1 {
		t.Errorf("expected 1 submitted, got %d", result.Submitted)
	}
	scenario.Verify()
}

func TestSubmit_DefaultTarget(t *testing.T) {
	// Each case stops at the missing-branch error, which shows the target
	// Submit chose and the hint explaining where the branch came from.
	unset := &cmd.ExecError{
		Args:   append([]string{"jj"}, trunkConfigGet...),
		Stderr: "Config error: Value not found for revset-aliases.\"trunk()\"\n",
		Err:    errors.New("exit status 1"),
	}
	fallback := `trunk() names no branch on "og", so the default main was used`
	tests := []struct {
		name       string
		remote     string // Submit's remote and branch arguments. trunk() is
		branch     string // only read when one of them is empty.
		trunk      string
		trunkErr   error
		wantRemote string
		wantBranch string
		wantNote   string // Expected in the hint, or "" for no mention of trunk()
	}{
		{
			name:       "trunk alias",
			trunk:      "master@og\n",
			wantRemote: "og",
			wantBranch: "master",
			wantNote:   `The branch comes from trunk() (master@og).`,
		},
		{
			name:       "trunk alias on another remote",
			trunk:      "main@origin\n",
			wantRemote: "origin",
			wantBranch: "main",
			wantNote:   `The branch comes from trunk() (main@origin).`,
		},
		{
			name:       "remote given with trunk on it",
			remote:     "up",
			trunk:      "master@up\n",
			wantRemote: "up",
			wantBranch: "master",
			wantNote:   `The branch comes from trunk() (master@up).`,
		},
		{
			name:       "remote given with trunk elsewhere",
			remote:     "og",
			trunk:      "master@up\n",
			wantRemote: "og",
			wantBranch: "main",
			wantNote:   fallback,
		},
		{
			name:       "complex revset",
			trunk:      "latest(remote_heads() | root())\n",
			wantRemote: "og",
			wantBranch: "main",
			wantNote:   fallback,
		},
		{
			name:       "operator after remote bookmark",
			trunk:      "master@og-\n",
			wantRemote: "og",
			wantBranch: "main",
			wantNote:   fallback,
		},
		{
			name:       "unset alias",
			trunkErr:   unset,
			wantRemote: "og",
			wantBranch: "main",
			wantNote:   fallback,
		},
		{
			name:       "branch given",
			branch:     "release/1.0",
			trunk:      "master@up\n",
			wantRemote: "up",
			wantBranch: "release/1.0",
		},
		{
			name:       "remote and branch given",
			remote:     "up",
			branch:     "release/1.0",
			wantRemote: "up",
			wantBranch: "release/1.0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remoteBookmark := tt.wantBranch + "@" + tt.wantRemote
			var calls []jjtest.Call
			if tt.remote == "" || tt.branch == "" {
				calls = append(calls, jjtest.Call{
					Args:   trunkConfigGet,
					Output: func(*jjtest.FakeRepo) string { return tt.trunk },
					Err:    tt.trunkErr,
				})
			}
			calls = append(calls,
				jjtest.Call{Args: []string{"git", "fetch", "--remote", tt.wantRemote}},
				jjtest.Call{
					Args: logArgs(remoteBookmark),
					Err:  errors.New("Error: Revision `" + remoteBookmark + "` doesn't exist"),
				},
			)
			scenario := jjtest.NewScenario(t, jjtest.NewFakeRepo(), calls...)

			_, err := Submit(context.Background(), scenario.Client(), nil, "@-", tt.remote, tt.branch, testUI)
			var userErr *ui.UserError
			if !errors.As(err, &userErr) {
				t.Fatalf("Submit() error = %v, want UserError", err)
			}
			wantMsg := `branch "` + tt.wantBranch + `" does not exist on remote "` + tt.wantRemote + `"`
			if userErr.Msg != wantMsg {
				t.Errorf("Msg = %q, want %q", userErr.Msg, wantMsg)
			}
			if tt.wantNote == "" {
				if strings.Contains(userErr.Hint, "trunk()") {
					t.Errorf("Hint mentions trunk() for an explicit target:\n%s", userErr.Hint)
				}
			} else if !strings.Contains(userErr.Hint, tt.wantNote) {
				t.Errorf("Hint = %q, want it to contain %q", userErr.Hint, tt.wantNote)
			}
			scenario.Verify()
		})
	}
}

func TestSubmit_TrunkConfigError(t *testing.T) {
	// A failure to read trunk() other than it being unset is not masked by
	// falling back to main@og.
	scenario := jjtest.NewScenario(t, jjtest.NewFakeRepo(),
		jjtest.Call{Args: trunkConfigGet, Err: errors.New("Config error: invalid TOML")},
	)
	_, err := Submit(context.Background(), scenario.Client(), nil, "@-", "", "", testUI)
	if err == nil || !strings.Contains(err.Error(), "reading trunk() alias") {
		t.Fatalf("Submit() error = %v, want trunk() read error", err)
	}
	scenario.Verify()
}
