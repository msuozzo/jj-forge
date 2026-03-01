package review

import (
	"testing"
)

func TestFormatPRLinks(t *testing.T) {
	tests := []struct {
		name     string
		parents  []PRLink
		children []PRLink
		want     string
	}{
		{
			name:    "parents only",
			parents: []PRLink{{Number: 1, URL: "https://github.com/owner/repo/pull/1"}},
			want:    "> Parents: [#1](https://redirect.github.com/owner/repo/pull/1)",
		},
		{
			name:     "children only",
			children: []PRLink{{Number: 2, URL: "https://github.com/owner/repo/pull/2"}},
			want:     "> Children: [#2](https://redirect.github.com/owner/repo/pull/2)",
		},
		{
			name:    "both parents and children",
			parents: []PRLink{{Number: 1, URL: "https://github.com/owner/repo/pull/1"}},
			children: []PRLink{
				{Number: 3, URL: "https://github.com/owner/repo/pull/3"},
				{Number: 4, URL: "https://github.com/owner/repo/pull/4"},
			},
			want: "> Parents: [#1](https://redirect.github.com/owner/repo/pull/1)\n> Children: [#3](https://redirect.github.com/owner/repo/pull/3), [#4](https://redirect.github.com/owner/repo/pull/4)",
		},
		{
			name: "empty",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatPRLinks(tt.parents, tt.children)
			if got != tt.want {
				t.Errorf("FormatPRLinks() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestLinkDisplayURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "github URL",
			url:  "https://github.com/owner/repo/pull/1",
			want: "https://redirect.github.com/owner/repo/pull/1",
		},
		{
			name: "non-github URL unchanged",
			url:  "https://gitlab.com/owner/repo/merge_requests/1",
			want: "https://gitlab.com/owner/repo/merge_requests/1",
		},
		{
			name: "already redirect URL unchanged",
			url:  "https://redirect.github.com/owner/repo/pull/1",
			want: "https://redirect.github.com/owner/repo/pull/1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := linkDisplayURL(tt.url)
			if got != tt.want {
				t.Errorf("linkDisplayURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestStripPRLinks(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "with redirect URL",
			body: "Description\n\n> Parents: [#1](https://redirect.github.com/owner/repo/pull/1)",
			want: "Description",
		},
		{
			name: "with section",
			body: "Some PR description\n\n> Parents: [#1](https://github.com/owner/repo/pull/1)",
			want: "Some PR description",
		},
		{
			name: "with both parents and children",
			body: "Description\n\n> Parents: [#1](https://github.com/owner/repo/pull/1)\n> Children: [#3](https://github.com/owner/repo/pull/3)",
			want: "Description",
		},
		{
			name: "without section",
			body: "Some PR description\n\nMore text here",
			want: "Some PR description\n\nMore text here",
		},
		{
			name: "user content with HR",
			body: "Description\n\n---\nSome other section",
			want: "Description\n\n---\nSome other section",
		},
		{
			name: "empty body",
			body: "",
			want: "",
		},
		{
			name: "only links section",
			body: "> Parents: [#1](https://github.com/owner/repo/pull/1)",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripPRLinks(tt.body)
			if got != tt.want {
				t.Errorf("StripPRLinks() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSetPRLinks_RoundTrip(t *testing.T) {
	original := "My PR description\n\nSome details"
	parents := []PRLink{{Number: 1, URL: "https://github.com/owner/repo/pull/1"}}
	children := []PRLink{{Number: 3, URL: "https://github.com/owner/repo/pull/3"}}

	// Set links
	withLinks := SetPRLinks(original, parents, children)
	want := "My PR description\n\nSome details\n\n> Parents: [#1](https://redirect.github.com/owner/repo/pull/1)\n> Children: [#3](https://redirect.github.com/owner/repo/pull/3)"
	if withLinks != want {
		t.Errorf("SetPRLinks() =\n%q\nwant:\n%q", withLinks, want)
	}

	// Update links (strip old, add new)
	newChildren := []PRLink{
		{Number: 3, URL: "https://github.com/owner/repo/pull/3"},
		{Number: 4, URL: "https://github.com/owner/repo/pull/4"},
	}
	updated := SetPRLinks(withLinks, parents, newChildren)
	want2 := "My PR description\n\nSome details\n\n> Parents: [#1](https://redirect.github.com/owner/repo/pull/1)\n> Children: [#3](https://redirect.github.com/owner/repo/pull/3), [#4](https://redirect.github.com/owner/repo/pull/4)"
	if updated != want2 {
		t.Errorf("SetPRLinks() round-trip =\n%q\nwant:\n%q", updated, want2)
	}

	// Remove all links
	noLinks := SetPRLinks(updated, nil, nil)
	if noLinks != "My PR description\n\nSome details" {
		t.Errorf("SetPRLinks() clear =\n%q\nwant:\n%q", noLinks, "My PR description\n\nSome details")
	}
}

func TestSetPRLinks_EmptyBody(t *testing.T) {
	got := SetPRLinks("", []PRLink{{Number: 1, URL: "https://github.com/owner/repo/pull/1"}}, nil)
	want := "> Parents: [#1](https://redirect.github.com/owner/repo/pull/1)"
	if got != want {
		t.Errorf("SetPRLinks() = %q, want %q", got, want)
	}
}
