package forge_test

import (
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
	tests := []struct {
		name     string
		fork     string // given fork and upstream
		upstream string
		remotes  string // jj git remote list output (read when fork is empty)
		push     string // git.push, "" for unset (read when fork is empty)
		trunk    string // trunk() alias (read when upstream is empty)
		want     forge.Remotes
	}{
		{name: "both given", fork: "mine", upstream: "theirs", want: forge.Remotes{Fork: "mine", Upstream: "theirs"}},
		{name: "forked repo from repo clone", remotes: "og url\nup url\n", push: "og", trunk: "master@up", want: forge.Remotes{Fork: "og", Upstream: "up"}},
		{name: "develop-on-main from repo clone", remotes: "og url\n", push: "og", trunk: "main@og", want: forge.Remotes{Fork: "og", Upstream: "og"}},
		{name: "plain jj git clone", remotes: "origin url\n", trunk: "master@origin", want: forge.Remotes{Fork: "origin", Upstream: "origin"}},
		{name: "custom trunk revset", remotes: "og url\n", push: "og", trunk: "latest(remote_heads())", want: forge.Remotes{Fork: "og", Upstream: "og"}},
		{name: "two remotes without git.push", remotes: "origin url\nupstream url\n", trunk: "main@origin", want: forge.Remotes{Fork: "og", Upstream: "origin"}},
		{name: "fork given", fork: "mine", trunk: "main@mine", want: forge.Remotes{Fork: "mine", Upstream: "mine"}},
		{name: "upstream given", upstream: "theirs", remotes: "og url\n", push: "og", want: forge.Remotes{Fork: "og", Upstream: "theirs"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []jjtest.Call
			if tt.fork == "" {
				calls = append(calls,
					jjtest.Call{Args: []string{"git", "remote", "list"}, Output: jjtest.Output(tt.remotes)},
					configGet(jj.GitPushKey, tt.push),
				)
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
