package jj

import "regexp"

// Keys of the jj config that name the remotes this tool works with.
const (
	GitPushKey    = "git.push"                 // the remote jj git push uses by default
	TrunkAliasKey = `revset-aliases."trunk()"` // the trunk() revset alias
)

// RemoteBookmark is a bookmark on a remote, as the revset main@origin names it.
type RemoteBookmark struct {
	Name   string
	Remote string
}

func (b RemoteBookmark) String() string { return b.Name + "@" + b.Remote }

// remoteBookmarkRegex matches a revset that is only <name>@<remote>, with
// both in jj's unquoted symbol syntax.
var remoteBookmarkRegex = regexp.MustCompile(`^([\w/]+(?:[.+-][\w/]+)*)@([\w/]+(?:[.+-][\w/]+)*)$`)

// ParseRemoteBookmark returns the bookmark a revset names when the revset is
// a plain <name>@<remote>, and the zero RemoteBookmark otherwise.
func ParseRemoteBookmark(revset string) RemoteBookmark {
	m := remoteBookmarkRegex.FindStringSubmatch(revset)
	if m == nil {
		return RemoteBookmark{}
	}
	return RemoteBookmark{Name: m[1], Remote: m[2]}
}
