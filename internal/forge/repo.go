package forge

import (
	"context"
	"fmt"
	"regexp"

	"github.com/msuozzo/jj-forge/internal/jj"
)

// gitURLRegex matches git remote URLs in SSH and HTTPS formats for any host.
// Captures: [1]=host, [2]=owner, [3]=repo
// Examples:
//
//	git@github.com:owner/repo.git
//	https://github.com/owner/repo.git
//	git@github.ubc.ca:owner/repo.git
//	https://gitlab.com/user/repo
//	ssh://git@example.com/owner/repo.git
var gitURLRegex = regexp.MustCompile(`(?:(?:https?|ssh)://(?:[^@]+@)?|[^@]+@)?([^/:]+)[:/]([^/]+)/([^/]+?)(?:\.git)?$`)

// RepoInfo contains repository owner and name extracted from a git remote.
type RepoInfo struct {
	Host  string // Repository host (e.g. "github.com", "github.ubc.ca")
	Owner string // Repository owner (user or organization)
	Name  string // Repository name
}

// ParseGitURL extracts host, owner, and repo from any SSH or HTTPS git remote URL.
func ParseGitURL(url string) (*RepoInfo, error) {
	matches := gitURLRegex.FindStringSubmatch(url)
	if matches == nil || len(matches) < 4 {
		return nil, fmt.Errorf("could not parse git URL: %s", url)
	}
	return &RepoInfo{
		Host:  matches[1],
		Owner: matches[2],
		Name:  matches[3],
	}, nil
}

// NormalizeRepoURL converts a remote URL to a canonical HTTPS format.
// Handles SSH (git@host:owner/repo.git) and HTTPS formats.
// Returns: https://{host}/owner/repo
func NormalizeRepoURL(url string) (string, error) {
	info, err := ParseGitURL(url)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("https://%s/%s/%s", info.Host, info.Owner, info.Name), nil
}

// GetRepoInfo extracts repository information from a git remote URL.
func GetRepoInfo(ctx context.Context, client jj.Client, remote string) (*RepoInfo, error) {
	url, err := client.RemoteURL(ctx, remote)
	if err != nil {
		return nil, err
	}
	info, err := ParseGitURL(url)
	if err != nil {
		return nil, fmt.Errorf("could not parse URL from remote %s: %w", remote, err)
	}
	return info, nil
}
