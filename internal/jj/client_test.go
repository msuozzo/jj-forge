package jj

import (
	"context"
	"errors"
	"testing"

	"github.com/msuozzo/jj-forge/internal/cmd"
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
			executor := func(ctx context.Context, _ cmd.Opts, args ...string) (*cmd.Result, error) {
				args = args[1:] // strip binary name
				if len(args) == 3 && args[0] == "git" && args[1] == "remote" && args[2] == "list" {
					return &cmd.Result{Stdout: tt.listOutput}, nil
				}
				return nil, errors.New("unexpected command")
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

func TestGitDir(t *testing.T) {
	tests := []struct {
		name       string
		rootOutput string
		wantPath   string
		wantErr    bool
	}{
		{
			name:       "standard path",
			rootOutput: "/abs/path/to/git\n",
			wantPath:   "/abs/path/to/git",
		},
		{
			name:       "whitespace trimmed",
			rootOutput: "  /abs/path/to/git  \n",
			wantPath:   "/abs/path/to/git",
		},
		{
			name:       "empty output",
			rootOutput: "",
			wantPath:   "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := func(ctx context.Context, _ cmd.Opts, args ...string) (*cmd.Result, error) {
				args = args[1:] // strip binary name
				if len(args) == 2 && args[0] == "git" && args[1] == "root" {
					return &cmd.Result{Stdout: tt.rootOutput}, nil
				}
				return nil, errors.New("unexpected command")
			}

			client := NewClientWithExecutor("", executor)
			got, err := client.GitDir(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("GitDir() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.wantPath {
				t.Errorf("GitDir() = %v, want %v", got, tt.wantPath)
			}
		})
	}
}
