package tangled

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    *RepoRef
		wantErr string
	}{
		{
			name: "https",
			url:  "https://tangled.org/alice.example.com/repo",
			want: &RepoRef{Host: "tangled.org", Owner: "alice.example.com", Name: "repo"},
		},
		{
			name: "https with .git and trailing slash",
			url:  "https://tangled.org/alice.example.com/repo.git/",
			want: &RepoRef{Host: "tangled.org", Owner: "alice.example.com", Name: "repo"},
		},
		{
			name: "https with @handle",
			url:  "https://tangled.org/@alice.example.com/repo",
			want: &RepoRef{Host: "tangled.org", Owner: "alice.example.com", Name: "repo"},
		},
		{
			name: "https with DID owner",
			url:  "https://tangled.org/did:plc:abc123/repo",
			want: &RepoRef{Host: "tangled.org", Owner: "did:plc:abc123", Name: "repo"},
		},
		{
			name: "scp-like ssh",
			url:  "git@tangled.org:alice.example.com/repo",
			want: &RepoRef{Host: "tangled.org", Owner: "alice.example.com", Name: "repo"},
		},
		{
			name: "scp-like ssh with DID owner",
			url:  "git@tangled.org:did:plc:abc123/repo.git",
			want: &RepoRef{Host: "tangled.org", Owner: "did:plc:abc123", Name: "repo"},
		},
		{
			name: "ssh scheme",
			url:  "ssh://git@tangled.org/alice.example.com/repo",
			want: &RepoRef{Host: "tangled.org", Owner: "alice.example.com", Name: "repo"},
		},
		{
			name: "ssh scheme with port",
			url:  "ssh://git@knot.example.com:2222/did:plc:abc123/repo",
			want: &RepoRef{Host: "knot.example.com", Owner: "did:plc:abc123", Name: "repo"},
		},
		{
			name: "git scheme",
			url:  "git://tangled.org/alice.example.com/repo",
			want: &RepoRef{Host: "tangled.org", Owner: "alice.example.com", Name: "repo"},
		},
		{
			name: "host is lowercased",
			url:  "https://Tangled.ORG/alice.example.com/repo",
			want: &RepoRef{Host: "tangled.org", Owner: "alice.example.com", Name: "repo"},
		},
		{
			name:    "bare repo DID",
			url:     "https://tangled.org/did:plc:abc123",
			wantErr: "names a repository by DID only",
		},
		{
			name:    "too many segments",
			url:     "https://tangled.org/a/b/c",
			wantErr: "expected host/owner/repo",
		},
		{
			name:    "missing repo",
			url:     "https://tangled.org/alice.example.com",
			wantErr: "expected host/owner/repo",
		},
		{
			name:    "local path",
			url:     "/home/alice/repo",
			wantErr: "could not parse",
		},
		{
			name:    "empty",
			url:     "",
			wantErr: "empty",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseURL(tt.url)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ParseURL(%q) = %+v, want error containing %q", tt.url, got, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParseURL(%q) error = %v, want containing %q", tt.url, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseURL(%q) error = %v", tt.url, err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseURL(%q) mismatch (-want +got):\n%s", tt.url, diff)
			}
		})
	}
}

func TestRepoRef_URLs(t *testing.T) {
	ref := RepoRef{Host: "knot.example.com", Owner: "alice.example.com", Name: "repo"}
	if got, want := ref.Target(), "alice.example.com/repo"; got != want {
		t.Errorf("Target() = %q, want %q", got, want)
	}
	if got, want := ref.WebURL(), "https://tangled.org/alice.example.com/repo"; got != want {
		t.Errorf("WebURL() = %q, want %q", got, want)
	}
	if got, want := ref.PullsURL(), "https://tangled.org/alice.example.com/repo/pulls"; got != want {
		t.Errorf("PullsURL() = %q, want %q", got, want)
	}
	if got, want := ref.CloneURL(), "https://knot.example.com/alice.example.com/repo"; got != want {
		t.Errorf("CloneURL() = %q, want %q", got, want)
	}
}

func TestNormalizeURL(t *testing.T) {
	got, err := NormalizeURL("git@tangled.org:alice.example.com/repo.git")
	if err != nil {
		t.Fatalf("NormalizeURL() error = %v", err)
	}
	if want := "https://tangled.org/alice.example.com/repo"; got != want {
		t.Errorf("NormalizeURL() = %q, want %q", got, want)
	}
	if _, err := NormalizeURL("nonsense"); err == nil {
		t.Error("expected error for unparseable URL")
	}
}
