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
		wantOwner string
		wantName  string
		wantErr   bool
	}{
		{
			name:      "github ssh",
			url:       "git@github.com:msuozzo/jj-forge.git",
			wantOwner: "msuozzo",
			wantName:  "jj-forge",
		},
		{
			name:      "github ssh no dot git",
			url:       "git@github.com:msuozzo/jj-forge",
			wantOwner: "msuozzo",
			wantName:  "jj-forge",
		},
		{
			name:      "github https",
			url:       "https://github.com/msuozzo/jj-forge.git",
			wantOwner: "msuozzo",
			wantName:  "jj-forge",
		},
		{
			name:      "github https no dot git",
			url:       "https://github.com/msuozzo/jj-forge",
			wantOwner: "msuozzo",
			wantName:  "jj-forge",
		},
		{
			name:    "invalid url",
			url:     "https://gitlab.com/user/repo",
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
