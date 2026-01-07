package jj

import (
	"context"
	"errors"
	"testing"
)

func TestRemoteURL(t *testing.T) {
	tests := []struct {
		name       string
		remote     string
		listOutput string
		wantURL    string
		wantErr    bool
	}{
		{
			name:       "single remote",
			remote:     "origin",
			listOutput: "origin git@github.com:user/repo.git\n",
			wantURL:    "git@github.com:user/repo.git",
		},
		{
			name:       "multiple remotes",
			remote:     "og",
			listOutput: "origin git@github.com:user/repo.git\nog git@github.com:msuozzo/jj-forge.git\nupstream https://github.com/upstream/repo\n",
			wantURL:    "git@github.com:msuozzo/jj-forge.git",
		},
		{
			name:       "remote not found",
			remote:     "missing",
			listOutput: "origin git@github.com:user/repo.git\n",
			wantErr:    true,
		},
		{
			name:       "empty output",
			remote:     "origin",
			listOutput: "",
			wantErr:    true,
		},
		{
			name:       "extra whitespace",
			remote:     "og",
			listOutput: "  origin   url1  \n  og   url2  \n",
			wantURL:    "url2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := func(ctx context.Context, args ...string) (string, error) {
				if len(args) == 3 && args[0] == "git" && args[1] == "remote" && args[2] == "list" {
					return tt.listOutput, nil
				}
				return "", errors.New("unexpected command")
			}

			client := NewClientWithExecutor("", executor)
			got, err := client.RemoteURL(context.Background(), tt.remote)
			if (err != nil) != tt.wantErr {
				t.Errorf("RemoteURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.wantURL {
				t.Errorf("RemoteURL() = %v, want %v", got, tt.wantURL)
			}
		})
	}
}
