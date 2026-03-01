package forge

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/msuozzo/jj-forge/internal/jj"
)

type mockRepoClient struct {
	jj.Client
	remoteURL string
	err       error
}

func (m *mockRepoClient) RemoteURL(ctx context.Context, remote string) (string, error) {
	return m.remoteURL, m.err
}

func TestGetRepoInfo(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    *RepoInfo
		wantErr bool
	}{
		{
			name: "github ssh",
			url:  "git@github.com:msuozzo/jj-forge.git",
			want: &RepoInfo{Host: "github.com", Owner: "msuozzo", Name: "jj-forge"},
		},
		{
			name: "github ssh no dot git",
			url:  "git@github.com:msuozzo/jj-forge",
			want: &RepoInfo{Host: "github.com", Owner: "msuozzo", Name: "jj-forge"},
		},
		{
			name: "github https",
			url:  "https://github.com/msuozzo/jj-forge.git",
			want: &RepoInfo{Host: "github.com", Owner: "msuozzo", Name: "jj-forge"},
		},
		{
			name: "github https no dot git",
			url:  "https://github.com/msuozzo/jj-forge",
			want: &RepoInfo{Host: "github.com", Owner: "msuozzo", Name: "jj-forge"},
		},
		{
			name: "ghes ssh",
			url:  "git@github.ubc.ca:owner/repo.git",
			want: &RepoInfo{Host: "github.ubc.ca", Owner: "owner", Name: "repo"},
		},
		{
			name: "ghes https",
			url:  "https://github.ubc.ca/owner/repo.git",
			want: &RepoInfo{Host: "github.ubc.ca", Owner: "owner", Name: "repo"},
		},
		{
			name: "gitlab ssh",
			url:  "git@gitlab.com:user/project.git",
			want: &RepoInfo{Host: "gitlab.com", Owner: "user", Name: "project"},
		},
		{
			name: "gitlab https",
			url:  "https://gitlab.com/user/project",
			want: &RepoInfo{Host: "gitlab.com", Owner: "user", Name: "project"},
		},
		{
			name:    "invalid url",
			url:     "not-a-url",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &mockRepoClient{remoteURL: tt.url}
			got, err := GetRepoInfo(context.Background(), client, "origin")
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetRepoInfo() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("GetRepoInfo() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestParseGitURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    *RepoInfo
		wantErr bool
	}{
		{
			name: "ssh with protocol",
			url:  "ssh://git@example.com/owner/repo.git",
			want: &RepoInfo{Host: "example.com", Owner: "owner", Name: "repo"},
		},
		{
			name: "http url",
			url:  "http://github.com/owner/repo",
			want: &RepoInfo{Host: "github.com", Owner: "owner", Name: "repo"},
		},
		{
			name:    "bare hostname",
			url:     "github.com",
			wantErr: true,
		},
		{
			name:    "empty string",
			url:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseGitURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseGitURL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseGitURL() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNormalizeRepoURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    string
		wantErr bool
	}{
		{
			name: "github ssh",
			url:  "git@github.com:owner/repo.git",
			want: "https://github.com/owner/repo",
		},
		{
			name: "ghes ssh",
			url:  "git@github.ubc.ca:owner/repo.git",
			want: "https://github.ubc.ca/owner/repo",
		},
		{
			name: "gitlab https",
			url:  "https://gitlab.com/user/project",
			want: "https://gitlab.com/user/project",
		},
		{
			name:    "invalid",
			url:     "not-a-url",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeRepoURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("NormalizeRepoURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("NormalizeRepoURL() = %v, want %v", got, tt.want)
			}
		})
	}
}
