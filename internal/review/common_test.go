package review

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/msuozzo/jj-forge/internal/jj"
)

func TestIsUploaded(t *testing.T) {
	tests := []struct {
		name             string
		rev              *jj.Rev
		remote           string
		expectedUploaded bool
	}{
		{
			name: "uploaded",
			rev: &jj.Rev{
				ID:              "aaaaaaaaaaaa",
				RemoteBookmarks: []string{"og/push-aaaaaaaaaaaa"},
			},
			remote:           "og",
			expectedUploaded: true,
		},
		{
			name: "not uploaded",
			rev: &jj.Rev{
				ID:              "aaaaaaaaaaaa",
				RemoteBookmarks: []string{},
			},
			remote:           "og",
			expectedUploaded: false,
		},
		{
			name: "uploaded to different remote",
			rev: &jj.Rev{
				ID:              "aaaaaaaaaaaa",
				RemoteBookmarks: []string{"origin/push-aaaaaaaaaaaa"},
			},
			remote:           "og",
			expectedUploaded: false,
		},
		{
			name: "multiple bookmarks including uploaded",
			rev: &jj.Rev{
				ID:              "aaaaaaaaaaaa",
				RemoteBookmarks: []string{"origin/main", "og/push-aaaaaaaaaaaa", "og/other"},
			},
			remote:           "og",
			expectedUploaded: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isUploaded(tt.rev, tt.remote)
			if got != tt.expectedUploaded {
				t.Errorf("isUploaded() = %v, want %v", got, tt.expectedUploaded)
			}
		})
	}
}

func TestSplitTitleBody(t *testing.T) {
	type result struct {
		Title string
		Body  string
	}
	tests := []struct {
		name        string
		description string
		want        result
	}{
		{
			name:        "title only",
			description: "feat: add feature",
			want:        result{"feat: add feature", ""},
		},
		{
			name:        "title and body",
			description: "feat: add feature\n\nThis is the body",
			want:        result{"feat: add feature", "This is the body"},
		},
		{
			name:        "title and multiline body",
			description: "feat: add feature\n\nThis is line 1\nThis is line 2\nThis is line 3",
			want:        result{"feat: add feature", "This is line 1\nThis is line 2\nThis is line 3"},
		},
		{
			name:        "empty description",
			description: "",
			want:        result{"", ""},
		},
		{
			name:        "only newlines",
			description: "\n\n\n",
			want:        result{"", ""},
		},
		{
			name:        "title with leading/trailing whitespace",
			description: "  feat: add feature  \n\n  body text  ",
			want:        result{"feat: add feature", "body text"},
		},
		{
			name:        "title with blank line then body",
			description: "feat: add feature\n\nBody paragraph 1\n\nBody paragraph 2",
			want:        result{"feat: add feature", "Body paragraph 1\n\nBody paragraph 2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTitle, gotBody := splitTitleBody(tt.description)
			got := result{gotTitle, gotBody}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("splitTitleBody() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
