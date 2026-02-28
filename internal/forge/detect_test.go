package forge

import (
	"context"
	"testing"
)

func TestDetectForge(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		wantType ForgeType
		wantErr  bool
	}{
		{
			name:     "SSM URL",
			url:      "https://us-central1-git.us-central1.sourcemanager.dev/my-project/my-repo.git",
			wantType: ForgeTypeSSM,
		},
		{
			name:     "github.com SSH",
			url:      "git@github.com:owner/repo.git",
			wantType: ForgeTypeGitHub,
		},
		{
			name:     "github.com HTTPS",
			url:      "https://github.com/owner/repo.git",
			wantType: ForgeTypeGitHub,
		},
		{
			name:     "unknown host",
			url:      "git@unknown.example.com:owner/repo.git",
			wantType: ForgeTypeUnknown,
		},
		{
			name:    "invalid URL",
			url:     "not-a-url",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DetectForge(context.Background(), tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("DetectForge() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.wantType {
				t.Errorf("DetectForge() = %v, want %v", got, tt.wantType)
			}
		})
	}
}
