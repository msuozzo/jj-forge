package tangled

import (
	"fmt"
	"strings"
)

// WebHost is the host of Tangled's web UI (the appview). Repository and pull
// request pages live here regardless of which knot hosts the git data.
const WebHost = "tangled.org"

// RepoRef identifies a Tangled repository by its git host, owner, and name.
type RepoRef struct {
	Host  string // Git host (e.g. "tangled.org" or a self-hosted knot)
	Owner string // Owner handle (e.g. "alice.example.com") or DID (e.g. "did:plc:...")
	Name  string // Repository name
}

// Target returns the "owner/name" form accepted by tg's repository arguments.
func (r RepoRef) Target() string {
	return r.Owner + "/" + r.Name
}

// WebURL returns the repository's page on the Tangled web UI.
func (r RepoRef) WebURL() string {
	return fmt.Sprintf("https://%s/%s/%s", WebHost, r.Owner, r.Name)
}

// PullsURL returns the repository's pull request list page.
//
// Tangled identifies pull requests on the web by a per-repository sequence
// number assigned by the appview, which is not exposed through tg or the
// atproto record. The list page is the fallback link when the numbered page
// cannot be resolved (see web.go).
func (r RepoRef) PullsURL() string {
	return r.WebURL() + "/pulls"
}

// CloneURL returns the canonical HTTPS clone URL on the git host.
func (r RepoRef) CloneURL() string {
	return fmt.Sprintf("https://%s/%s/%s", r.Host, r.Owner, r.Name)
}

// ParseURL parses a Tangled git remote URL into its components.
//
// Supported forms (with optional trailing ".git"):
//
//	https://tangled.org/owner/repo
//	https://tangled.org/@owner/repo
//	git@tangled.org:owner/repo
//	git@tangled.org:did:plc:abc123/repo
//	ssh://git@tangled.org[:port]/owner/repo
//	git://tangled.org/owner/repo
//
// The same shapes are accepted for self-hosted knots. Bare repository-DID
// URLs (https://tangled.org/did:plc:...) carry no owner or name and are
// rejected with a hint to use the owner/repo form.
func ParseURL(raw string) (*RepoRef, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty Tangled URL")
	}
	var hostPart, path string
	if i := strings.Index(raw, "://"); i >= 0 {
		rest := raw[i+3:]
		var ok bool
		hostPart, path, ok = strings.Cut(rest, "/")
		if !ok {
			return nil, fmt.Errorf("could not parse Tangled URL: %s", raw)
		}
	} else {
		// SCP-like syntax: [user@]host:path. The first colon separates the
		// host. Later colons may belong to a DID owner (did:plc:...).
		var ok bool
		hostPart, path, ok = strings.Cut(raw, ":")
		if !ok || strings.Contains(hostPart, "/") {
			return nil, fmt.Errorf("could not parse Tangled URL: %s", raw)
		}
	}
	if at := strings.LastIndex(hostPart, "@"); at >= 0 {
		hostPart = hostPart[at+1:]
	}
	if h, _, ok := strings.Cut(hostPart, ":"); ok {
		hostPart = h // strip port
	}
	host := strings.ToLower(hostPart)
	if host == "" {
		return nil, fmt.Errorf("could not parse Tangled URL: %s", raw)
	}

	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	segments := strings.Split(path, "/")
	if len(segments) == 1 && strings.HasPrefix(segments[0], "did:") {
		return nil, fmt.Errorf(
			"Tangled URL %s names a repository by DID only (use the owner/repo form, e.g. https://%s/<owner>/<repo>)",
			raw, host)
	}
	if len(segments) != 2 || segments[0] == "" || segments[1] == "" {
		return nil, fmt.Errorf("could not parse Tangled URL (expected host/owner/repo): %s", raw)
	}
	owner := strings.TrimPrefix(segments[0], "@")
	return &RepoRef{Host: host, Owner: owner, Name: segments[1]}, nil
}

// NormalizeURL converts a Tangled remote URL to its canonical HTTPS clone form.
func NormalizeURL(raw string) (string, error) {
	ref, err := ParseURL(raw)
	if err != nil {
		return "", err
	}
	return ref.CloneURL(), nil
}
