package forge

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/msuozzo/jj-forge/internal/jj"
)

// githubURLRegex matches GitHub URLs in both SSH and HTTPS formats.
// Examples:
//
//	git@github.com:owner/repo.git
//	https://github.com/owner/repo.git
//	https://github.com/owner/repo
var githubURLRegex = regexp.MustCompile(`github\.com[:/]([^/]+)/(.+?)(\.git)?$`)

// RepoInfo contains repository owner and name extracted from a git remote.
type RepoInfo struct {
	Owner string // Repository owner (user or organization)
	Name  string // Repository name
}

// GetRepoInfo extracts repository information from a git remote URL.
func GetRepoInfo(ctx context.Context, client jj.Client, remote string) (*RepoInfo, error) {
	// Get the remote URL
	url, err := client.RemoteURL(ctx, remote)
	if err != nil {
		return nil, err
	}
	matches := githubURLRegex.FindStringSubmatch(url)
	if matches == nil || len(matches) < 3 {
		return nil, fmt.Errorf("could not parse GitHub URL from remote %s: %s", remote, url)
	}
	return &RepoInfo{
		Owner: matches[1],
		Name:  strings.TrimSuffix(matches[2], ".git"),
	}, nil
}
