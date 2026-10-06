package forge

import (
	"cmp"
	"context"
	"slices"

	"github.com/msuozzo/jj-forge/internal/jj"
)

// Names repo clone gives the remotes it creates and the review commands look
// for, unless forge.default-fork-remote or forge.default-upstream-remote says
// otherwise.
const (
	DefaultForkRemote     = "og"
	DefaultUpstreamRemote = "up"
)

// Remotes are the remotes a review command works with.
type Remotes struct {
	Fork     string // where review branches are pushed
	Upstream string // where reviews are opened
}

// ResolveRemotes fills in whichever of fork and upstream is empty. The fork
// remote defaults to git.push, else the repository's only remote, else the
// configured fork name. The upstream remote defaults to the configured
// upstream name when the repository has that remote, else the remote
// trunk() names, else the fork remote. client and configMgr must address
// the same repository.
func ResolveRemotes(ctx context.Context, client jj.Client, configMgr *ConfigManager, fork, upstream string) (Remotes, error) {
	if fork != "" && upstream != "" {
		return Remotes{Fork: fork, Upstream: upstream}, nil
	}
	remotes, err := client.Remotes(ctx)
	if err != nil {
		return Remotes{}, err
	}
	// present returns name if the repository has that remote, and "" otherwise.
	present := func(name string) string {
		if slices.Contains(remotes, name) {
			return name
		}
		return ""
	}
	if fork == "" {
		push, err := configMgr.Get(jj.GitPushKey)
		if err != nil {
			return Remotes{}, err
		}
		name, err := configMgr.GetDefaultForkRemote()
		if err != nil {
			return Remotes{}, err
		}
		var sole string
		if len(remotes) == 1 {
			sole = remotes[0]
		}
		fork = cmp.Or(push, sole, present(name), name)
	}
	if upstream == "" {
		name, err := configMgr.GetDefaultUpstreamRemote()
		if err != nil {
			return Remotes{}, err
		}
		alias, err := configMgr.Get(jj.TrunkAliasKey)
		if err != nil {
			return Remotes{}, err
		}
		upstream = cmp.Or(present(name), jj.ParseRemoteBookmark(alias).Remote, fork)
	}
	return Remotes{Fork: fork, Upstream: upstream}, nil
}
