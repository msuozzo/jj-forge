package forge

import "testing"

func TestUpdateParentTrailers(t *testing.T) {
	tests := []struct {
		name        string
		description string
		parentIDs   []string
		want        string
	}{
		{
			name:        "empty description",
			description: "",
			parentIDs:   []string{"abc123"},
			want:        "forge-parent: abc123\n",
		},
		{
			name:        "simple description",
			description: "feat: add something",
			parentIDs:   []string{"abc123"},
			want:        "feat: add something\n\nforge-parent: abc123\n",
		},
		{
			name:        "update existing",
			description: "feat: add something\n\nforge-parent: oldid\n",
			parentIDs:   []string{"newid"},
			want:        "feat: add something\n\nforge-parent: newid\n",
		},
		{
			name:        "append to existing trailers",
			description: "feat: add something\n\nSigned-off-by: Me <me@me.com>",
			parentIDs:   []string{"abc123"},
			want:        "feat: add something\n\nSigned-off-by: Me <me@me.com>\nforge-parent: abc123\n",
		},
		{
			name:        "multiple parents",
			description: "feat: merge\n",
			parentIDs:   []string{"parent1", "parent2"},
			want:        "feat: merge\n\nforge-parent: parent1\nforge-parent: parent2\n",
		},
		{
			name:        "replace single with multiple",
			description: "feat: merge\n\nforge-parent: oldid\n",
			parentIDs:   []string{"parent1", "parent2"},
			want:        "feat: merge\n\nforge-parent: parent1\nforge-parent: parent2\n",
		},
		{
			name:        "replace multiple with different",
			description: "feat: merge\n\nforge-parent: old1\nforge-parent: old2\n",
			parentIDs:   []string{"new1", "new2", "new3"},
			want:        "feat: merge\n\nforge-parent: new1\nforge-parent: new2\nforge-parent: new3\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := UpdateParentTrailers(tt.description, tt.parentIDs); got != tt.want {
				t.Errorf("UpdateParentTrailers() = %q, want %q", got, tt.want)
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
