package jj

import "testing"

func TestParseRemoteBookmark(t *testing.T) {
	tests := []struct {
		revset string
		want   RemoteBookmark
	}{
		{revset: "master@og", want: RemoteBookmark{Name: "master", Remote: "og"}},
		{revset: "release-1.0@my.fork", want: RemoteBookmark{Name: "release-1.0", Remote: "my.fork"}},
		{revset: "latest(remote_heads() | root())"},
		{revset: "master@og-"},
		{revset: `"main"@og`},
		{revset: ""},
	}
	for _, tt := range tests {
		t.Run(tt.revset, func(t *testing.T) {
			if got := ParseRemoteBookmark(tt.revset); got != tt.want {
				t.Errorf("ParseRemoteBookmark(%q) = %+v, want %+v", tt.revset, got, tt.want)
			}
		})
	}
}
