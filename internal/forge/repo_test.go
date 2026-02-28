package forge

import (
	"context"
	"testing"

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
		name      string
		url       string
		wantHost  string
		wantOwner string
		wantName  string
		wantErr   bool
	}{
		{
			name:      "github ssh",
			url:       "git@github.com:msuozzo/jj-forge.git",
			wantHost:  "github.com",
			wantOwner: "msuozzo",
			wantName:  "jj-forge",
		},
		{
			name:      "github ssh no dot git",
			url:       "git@github.com:msuozzo/jj-forge",
			wantHost:  "github.com",
			wantOwner: "msuozzo",
			wantName:  "jj-forge",
		},
		{
			name:      "github https",
			url:       "https://github.com/msuozzo/jj-forge.git",
			wantHost:  "github.com",
			wantOwner: "msuozzo",
			wantName:  "jj-forge",
		},
		{
			name:      "github https no dot git",
			url:       "https://github.com/msuozzo/jj-forge",
			wantHost:  "github.com",
			wantOwner: "msuozzo",
			wantName:  "jj-forge",
		},
		{
			name:      "ghes ssh",
			url:       "git@github.ubc.ca:owner/repo.git",
			wantHost:  "github.ubc.ca",
			wantOwner: "owner",
			wantName:  "repo",
		},
		{
			name:      "ghes https",
			url:       "https://github.ubc.ca/owner/repo.git",
			wantHost:  "github.ubc.ca",
			wantOwner: "owner",
			wantName:  "repo",
		},
		{
			name:      "gitlab ssh",
			url:       "git@gitlab.com:user/project.git",
			wantHost:  "gitlab.com",
			wantOwner: "user",
			wantName:  "project",
		},
		{
			name:      "gitlab https",
			url:       "https://gitlab.com/user/project",
			wantHost:  "gitlab.com",
			wantOwner: "user",
			wantName:  "project",
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
			info, err := GetRepoInfo(context.Background(), client, "origin")
			if (err != nil) != tt.wantErr {
				t.Errorf("GetRepoInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if info.Host != tt.wantHost {
					t.Errorf("GetRepoInfo() Host = %v, want %v", info.Host, tt.wantHost)
				}
				if info.Owner != tt.wantOwner {
					t.Errorf("GetRepoInfo() Owner = %v, want %v", info.Owner, tt.wantOwner)
				}
				if info.Name != tt.wantName {
					t.Errorf("GetRepoInfo() Name = %v, want %v", info.Name, tt.wantName)
				}
			}
		})
	}
}

func TestParseGitURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantHost  string
		wantOwner string
		wantName  string
		wantErr   bool
	}{
		{
			name:      "ssh with protocol",
			url:       "ssh://git@example.com/owner/repo.git",
			wantHost:  "example.com",
			wantOwner: "owner",
			wantName:  "repo",
		},
		{
			name:      "http url",
			url:       "http://github.com/owner/repo",
			wantHost:  "github.com",
			wantOwner: "owner",
			wantName:  "repo",
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
			info, err := ParseGitURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseGitURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if info.Host != tt.wantHost {
					t.Errorf("ParseGitURL() Host = %v, want %v", info.Host, tt.wantHost)
				}
				if info.Owner != tt.wantOwner {
					t.Errorf("ParseGitURL() Owner = %v, want %v", info.Owner, tt.wantOwner)
				}
				if info.Name != tt.wantName {
					t.Errorf("ParseGitURL() Name = %v, want %v", info.Name, tt.wantName)
				}
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
