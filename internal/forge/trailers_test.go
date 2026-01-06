package forge

import "testing"

func TestUpdateParentTrailer(t *testing.T) {
	tests := []struct {
		name        string
		description string
		parentID    string
		want        string
	}{
		{
			name:        "empty description",
			description: "",
			parentID:    "abc123",
			want:        "forge-parent: abc123\n",
		},
		{
			name:        "simple description",
			description: "feat: add something",
			parentID:    "abc123",
			want:        "feat: add something\n\nforge-parent: abc123\n",
		},
		{
			name:        "update existing",
			description: "feat: add something\n\nforge-parent: oldid\n",
			parentID:    "newid",
			want:        "feat: add something\n\nforge-parent: newid\n",
		},
		{
			name:        "append to existing trailers",
			description: "feat: add something\n\nSigned-off-by: Me <me@me.com>",
			parentID:    "abc123",
			want:        "feat: add something\n\nSigned-off-by: Me <me@me.com>\nforge-parent: abc123\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := UpdateParentTrailer(tt.description, tt.parentID); got != tt.want {
				t.Errorf("UpdateParentTrailer() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRemoveParentTrailer(t *testing.T) {
	tests := []struct {
		name        string
		description string
		want        string
	}{
		{
			name:        "no trailer",
			description: "feat: add something\n",
			want:        "feat: add something\n",
		},
		{
			name:        "remove trailer",
			description: "feat: add something\n\nforge-parent: abc123\n",
			want:        "feat: add something\n",
		},
		{
			name:        "remove middle trailer",
			description: "feat: add something\n\nforge-parent: abc123\nSigned-off-by: Me\n",
			want:        "feat: add something\n\nSigned-off-by: Me\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RemoveParentTrailer(tt.description); got != tt.want {
				t.Errorf("RemoveParentTrailer() = %q, want %q", got, tt.want)
			}
		})
	}
}
