package forge

import (
	"cmp"
	"context"

	"github.com/msuozzo/jj-forge/internal/jj"
)

// Remotes are the remotes a review command works with.
type Remotes struct {
	Fork     string // where review branches are pushed
	Upstream string // where reviews are opened
}

// ResolveRemotes fills in whichever of fork and upstream is empty. The fork
// remote defaults to git.push, else the repository's only remote, else "og".
// The upstream remote defaults to the remote trunk() names, else the fork
// remote. client and configMgr must address the same repository.
func ResolveRemotes(ctx context.Context, client jj.Client, configMgr *ConfigManager, fork, upstream string) (Remotes, error) {
	if fork == "" {
		remotes, err := client.Remotes(ctx)
		if err != nil {
			return Remotes{}, err
		}
		push, err := configMgr.Get(jj.GitPushKey)
		if err != nil {
			return Remotes{}, err
		}
		var sole string
		if len(remotes) == 1 {
			sole = remotes[0]
		}
		fork = cmp.Or(push, sole, "og")
	}
	if upstream == "" {
		alias, err := configMgr.Get(jj.TrunkAliasKey)
		if err != nil {
			return Remotes{}, err
		}
		upstream = cmp.Or(jj.ParseRemoteBookmark(alias).Remote, fork)
	}
	return Remotes{Fork: fork, Upstream: upstream}, nil
}
