package forge

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ForgeType represents the type of forge hosting a repository.
type ForgeType int

const (
	ForgeTypeUnknown ForgeType = iota
	ForgeTypeGitHub
	ForgeTypeGitLab
	ForgeTypeSSM
)

// HTTPDoer abstracts *http.Client for test mock injection.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// DetectForgeByHeaders sends a HEAD request to the given host and inspects
// response headers to determine the forge type.
//   - x-github-request-id → GitHub / GitHub Enterprise Server
//   - x-gitlab-meta → GitLab
func DetectForgeByHeaders(ctx context.Context, host string, client HTTPDoer) (ForgeType, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, fmt.Sprintf("https://%s/", host), nil)
	if err != nil {
		return ForgeTypeUnknown, fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return ForgeTypeUnknown, fmt.Errorf("failed to probe %s: %w", host, err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("x-github-request-id") != "" {
		return ForgeTypeGitHub, nil
	}
	if resp.Header.Get("x-gitlab-meta") != "" {
		return ForgeTypeGitLab, nil
	}
	return ForgeTypeUnknown, nil
}

// ParseForgeType parses a forge type string into its corresponding ForgeType constant.
func ParseForgeType(s string) ForgeType {
	switch strings.ToLower(s) {
	case "github":
		return ForgeTypeGitHub
	case "gitlab":
		return ForgeTypeGitLab
	case "ssm":
		return ForgeTypeSSM
	default:
		return ForgeTypeUnknown
	}
}

// DetectForge determines the forge type from a git remote URL.
// It parses the URL, resolves custom host overrides, checks built-in fast
// paths (SSM and github.com), before falling back to HTTP header probing
// for unknown hosts.
func DetectForge(ctx context.Context, url string, httpClient HTTPDoer, hosts map[string]string) (ForgeType, error) {
	info, err := ParseGitURL(url)
	if err != nil {
		return ForgeTypeUnknown, fmt.Errorf("could not parse URL: %w", err)
	}
	// check overrides first
	if hosts != nil {
		if t, ok := hosts[info.Host]; ok {
			if ft := ParseForgeType(t); ft != ForgeTypeUnknown {
				return ft, nil
			}
		}
	}
	if strings.HasSuffix(info.Host, ".sourcemanager.dev") {
		return ForgeTypeSSM, nil
	}
	if info.Host == "github.com" {
		return ForgeTypeGitHub, nil
	}
	return DetectForgeByHeaders(ctx, info.Host, httpClient)
}

// DefaultHTTPClient returns an *http.Client with a 5-second timeout for forge detection probes.
func DefaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Second}
}
