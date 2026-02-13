package ssm

import (
	"testing"
)

func TestIsSSMURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{
			name: "HTTPS with .git suffix",
			url:  "https://us-central1-git.us-central1.sourcemanager.dev/my-project/my-repo.git",
			want: true,
		},
		{
			name: "HTTPS without .git suffix",
			url:  "https://us-central1-git.us-central1.sourcemanager.dev/my-project/my-repo",
			want: true,
		},
		{
			name: "different region",
			url:  "https://europe-west1-git.europe-west1.sourcemanager.dev/my-project/my-repo",
			want: true,
		},
		{
			name: "GitHub URL",
			url:  "https://github.com/owner/repo",
			want: false,
		},
		{
			name: "SSH GitHub URL",
			url:  "git@github.com:owner/repo.git",
			want: false,
		},
		{
			name: "empty string",
			url:  "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSSMURL(tt.url)
			if got != tt.want {
				t.Errorf("IsSSMURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestParseSSMURL(t *testing.T) {
	tests := []struct {
		name         string
		url          string
		wantInstance string
		wantLocation string
		wantProject  string
		wantRepo     string
		wantErr      bool
	}{
		{
			name:         "standard HTTPS URL",
			url:          "https://us-central1-git.us-central1.sourcemanager.dev/my-project/my-repo",
			wantInstance: "us-central1",
			wantLocation: "us-central1",
			wantProject:  "my-project",
			wantRepo:     "my-repo",
		},
		{
			name:         "with .git suffix",
			url:          "https://us-central1-git.us-central1.sourcemanager.dev/my-project/my-repo.git",
			wantInstance: "us-central1",
			wantLocation: "us-central1",
			wantProject:  "my-project",
			wantRepo:     "my-repo",
		},
		{
			name:         "europe-west1 region",
			url:          "https://europe-west1-git.europe-west1.sourcemanager.dev/proj123/repo456",
			wantInstance: "europe-west1",
			wantLocation: "europe-west1",
			wantProject:  "proj123",
			wantRepo:     "repo456",
		},
		{
			name:    "invalid URL",
			url:     "https://github.com/owner/repo",
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
			instance, location, project, repo, err := ParseSSMURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseSSMURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if instance != tt.wantInstance {
				t.Errorf("instance = %q, want %q", instance, tt.wantInstance)
			}
			if location != tt.wantLocation {
				t.Errorf("location = %q, want %q", location, tt.wantLocation)
			}
			if project != tt.wantProject {
				t.Errorf("project = %q, want %q", project, tt.wantProject)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}

func TestNormalizeSSMURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    string
		wantErr bool
	}{
		{
			name: "already canonical",
			url:  "https://us-central1-git.us-central1.sourcemanager.dev/my-project/my-repo",
			want: "https://us-central1-git.us-central1.sourcemanager.dev/my-project/my-repo",
		},
		{
			name: "strip .git suffix",
			url:  "https://us-central1-git.us-central1.sourcemanager.dev/my-project/my-repo.git",
			want: "https://us-central1-git.us-central1.sourcemanager.dev/my-project/my-repo",
		},
		{
			name:    "invalid URL",
			url:     "https://github.com/owner/repo",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeSSMURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NormalizeSSMURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got != tt.want {
				t.Errorf("NormalizeSSMURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestResourceName(t *testing.T) {
	got := ResourceName("my-project", "us-central1", "my-repo")
	want := "projects/my-project/locations/us-central1/repositories/my-repo"
	if got != want {
		t.Errorf("ResourceName() = %q, want %q", got, want)
	}
}
