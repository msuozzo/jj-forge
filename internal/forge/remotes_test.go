package forge_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/jj"
	"github.com/msuozzo/jj-forge/internal/jjtest"
)

// configGet expects config get KEY, printing value or failing as unset when
// value is "".
func configGet(key, value string) jjtest.Call {
	call := jjtest.Call{Args: []string{"config", "get", key}}
	if value == "" {
		call.Err = jjtest.ConfigNotFound(key)
	} else {
		call.Output = jjtest.Output(value + "\n")
	}
	return call
}

func TestResolveRemotes(t *testing.T) {
	ogUp := "forge.default-fork-remote = \"og\"\nforge.default-upstream-remote = \"up\"\n"
	originUpstream := "forge.default-fork-remote = \"origin\"\nforge.default-upstream-remote = \"upstream\"\n"
	tests := []struct {
		name     string
		fork     string // given fork and upstream
		upstream string
		remotes  string // jj git remote list output
		push     string // git.push, "" for unset (read when fork is empty)
		config   string // jj config list forge output (read when either is empty)
		trunk    string // trunk() alias (read when upstream is empty)
		want     forge.Remotes
	}{
		{
			name: "both given",
			fork: "mine", upstream: "theirs",
			want: forge.Remotes{Fork: "mine", Upstream: "theirs"},
		},
		{
			name:    "forked repo from repo clone",
			remotes: "og url\nup url\n", push: "og", trunk: "master@up",
			want: forge.Remotes{Fork: "og", Upstream: "up"},
		},
		{
			name:    "develop-on-main from repo clone",
			remotes: "og url\n", push: "og", trunk: "main@og",
			want: forge.Remotes{Fork: "og", Upstream: "og"},
		},
		{
			name:    "plain jj git clone",
			remotes: "origin url\n", trunk: "master@origin",
			want: forge.Remotes{Fork: "origin", Upstream: "origin"},
		},
		{
			name:    "legacy fork without git.push, names configured",
			remotes: "og url\nup url\n", config: ogUp, trunk: "master@up",
			want: forge.Remotes{Fork: "og", Upstream: "up"},
		},
		{
			name:    "fork-only hybrid with trunk on the fork",
			remotes: "og url\nup url\n", push: "og", config: ogUp, trunk: "master@og",
			want: forge.Remotes{Fork: "og", Upstream: "up"},
		},
		{
			name:    "custom trunk revset",
			remotes: "og url\n", push: "og", trunk: "latest(remote_heads())",
			want: forge.Remotes{Fork: "og", Upstream: "og"},
		},
		{
			name:    "conventional names configured",
			remotes: "origin url\nupstream url\n", config: originUpstream, trunk: "main@origin",
			want: forge.Remotes{Fork: "origin", Upstream: "upstream"},
		},
		{
			name:    "conventional names by default",
			remotes: "origin url\nupstream url\n", trunk: "main@origin",
			want: forge.Remotes{Fork: "origin", Upstream: "upstream"},
		},
		{
			name:    "legacy fork without git.push or config",
			remotes: "og url\nup url\n", trunk: "master@up",
			want: forge.Remotes{Fork: "origin", Upstream: "up"},
		},
		{
			name: "fork given",
			fork: "mine", remotes: "mine url\n", trunk: "main@mine",
			want: forge.Remotes{Fork: "mine", Upstream: "mine"},
		},
		{
			name:     "upstream given",
			upstream: "theirs", remotes: "og url\n", push: "og",
			want: forge.Remotes{Fork: "og", Upstream: "theirs"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []jjtest.Call
			if tt.fork == "" || tt.upstream == "" {
				calls = append(calls, jjtest.Call{Args: []string{"git", "remote", "list"}, Output: jjtest.Output(tt.remotes)})
			}
			if tt.fork == "" {
				calls = append(calls, configGet(jj.GitPushKey, tt.push))
			}
			if tt.fork == "" || tt.upstream == "" {
				calls = append(calls, jjtest.Call{Args: []string{"config", "list", "forge"}, Output: jjtest.Output(tt.config)})
			}
			if tt.upstream == "" {
				calls = append(calls, configGet(jj.TrunkAliasKey, tt.trunk))
			}
			scenario := jjtest.NewScenario(t, jjtest.NewFakeRepo(), calls...)
			client := scenario.Client()
			got, err := forge.ResolveRemotes(t.Context(), client, forge.NewConfigManager(client), tt.fork, tt.upstream)
			if err != nil {
				t.Fatalf("ResolveRemotes() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("ResolveRemotes() = %+v, want %+v", got, tt.want)
			}
			scenario.Verify()
		})
	}
}

func TestResolveRemotes_ListError(t *testing.T) {
	scenario := jjtest.NewScenario(t, jjtest.NewFakeRepo(),
		jjtest.Call{Args: []string{"git", "remote", "list"}, Err: errors.New("not a jj repo")},
	)
	client := scenario.Client()
	_, err := forge.ResolveRemotes(t.Context(), client, forge.NewConfigManager(client), "", "")
	if err == nil || !strings.Contains(err.Error(), "failed to list remotes") {
		t.Fatalf("ResolveRemotes() error = %v, want remote listing error", err)
	}
	scenario.Verify()
}
