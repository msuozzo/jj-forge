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

func TestSubmit_TrunkBranch(t *testing.T) {
	// With no branch given and trunk() at master@og, submit fast-forwards master.
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
		jjtest.Call{Args: []string{"git", "fetch", "--remote", testRemote}},
		jjtest.Call{Args: logArgs("master@og"), Output: jjtest.LogOutput("mmmmmmmmmmmm")},
		jjtest.Call{Args: logArgs("@-"), Output: jjtest.LogOutput("aaaaaaaaaaaa")},
		jjtest.Call{Args: logArgs("parents(@-)~(@-)"), Output: jjtest.LogOutput("mmmmmmmmmmmm")},
		jjtest.Call{Args: []string{"bookmark", "set", "master", "-r", "aaaaaaaaaaaa"}},
		jjtest.Call{Args: []string{"git", "push", "--bookmark", "master", "--remote", testRemote}},
		jjtest.Call{Args: []string{"git", "fetch", "--remote", testRemote}},
		jjtest.Call{Args: logArgs("master@og"), Output: jjtest.LogOutput("aaaaaaaaaaaa")},
	)

	result, err := Submit(context.Background(), scenario.Client(), nil, "@-", testRemote, "", testUI)
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if result.Submitted != 1 {
		t.Errorf("expected 1 submitted, got %d", result.Submitted)
	}
	scenario.Verify()
}

func TestSubmit_DefaultBranch(t *testing.T) {
	// Each case stops at the missing-branch error, which shows the branch
	// Submit chose and the hint explaining where it came from.
	unset := &cmd.ExecError{
		Args:   append([]string{"jj"}, trunkConfigGet...),
		Stderr: "Config error: Value not found for revset-aliases.\"trunk()\"\n",
		Err:    errors.New("exit status 1"),
	}
	tests := []struct {
		name       string
		branch     string // Submit's branch argument; trunk() is only read if empty
		trunk      string
		trunkErr   error
		wantBranch string
		wantNote   string // Expected in the hint; "" means no mention of trunk()
	}{
		{
			name:       "simple alias on remote",
			trunk:      "master@og\n",
			wantBranch: "master",
			wantNote:   `The branch comes from trunk() (master@og).`,
		},
		{
			name:       "alias on another remote",
			trunk:      "master@up\n",
			wantBranch: "main",
			wantNote:   `trunk() names no branch on "og", so the default main was used`,
		},
		{
			name:       "complex revset",
			trunk:      "latest(remote_heads() | root())\n",
			wantBranch: "main",
			wantNote:   `trunk() names no branch on "og", so the default main was used`,
		},
		{
			name:       "operator after remote bookmark",
			trunk:      "master@og-\n",
			wantBranch: "main",
			wantNote:   `trunk() names no branch on "og", so the default main was used`,
		},
		{
			name:       "unset alias",
			trunkErr:   unset,
			wantBranch: "main",
			wantNote:   `trunk() names no branch on "og", so the default main was used`,
		},
		{
			name:       "explicit branch",
			branch:     "release/1.0",
			wantBranch: "release/1.0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remoteBookmark := tt.wantBranch + "@" + testRemote
			var calls []jjtest.Call
			if tt.branch == "" {
				calls = append(calls, jjtest.Call{
					Args:   trunkConfigGet,
					Output: func(*jjtest.FakeRepo) string { return tt.trunk },
					Err:    tt.trunkErr,
				})
			}
			calls = append(calls,
				jjtest.Call{Args: []string{"git", "fetch", "--remote", testRemote}},
				jjtest.Call{
					Args: logArgs(remoteBookmark),
					Err:  errors.New("Error: Revision `" + remoteBookmark + "` doesn't exist"),
				},
			)
			scenario := jjtest.NewScenario(t, jjtest.NewFakeRepo(), calls...)

			_, err := Submit(context.Background(), scenario.Client(), nil, "@-", testRemote, tt.branch, testUI)
			var userErr *ui.UserError
			if !errors.As(err, &userErr) {
				t.Fatalf("Submit() error = %v, want UserError", err)
			}
			wantMsg := `branch "` + tt.wantBranch + `" does not exist on remote "og"`
			if userErr.Msg != wantMsg {
				t.Errorf("Msg = %q, want %q", userErr.Msg, wantMsg)
			}
			if tt.wantNote == "" {
				if strings.Contains(userErr.Hint, "trunk()") {
					t.Errorf("Hint mentions trunk() for an explicit branch:\n%s", userErr.Hint)
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
	// falling back to main.
	scenario := jjtest.NewScenario(t, jjtest.NewFakeRepo(),
		jjtest.Call{Args: trunkConfigGet, Err: errors.New("Config error: invalid TOML")},
	)
	_, err := Submit(context.Background(), scenario.Client(), nil, "@-", testRemote, "", testUI)
	if err == nil || !strings.Contains(err.Error(), "reading trunk() alias") {
		t.Fatalf("Submit() error = %v, want trunk() read error", err)
	}
	scenario.Verify()
}
