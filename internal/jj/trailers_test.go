package jj

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseDescriptionTrailers(t *testing.T) {
	tests := []struct {
		name        string
		description string
		want        []Trailer
	}{
		{
			name: "simple trailers",
			description: `chore: update itertools to version 0.14.0

Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed
do eiusmod tempor incididunt ut labore et dolore magna aliqua.

Co-authored-by: Alice <alice@example.com>
Co-authored-by: Bob <bob@example.com>
Reviewed-by: Charlie <charlie@example.com>
Change-Id: I1234567890abcdef1234567890abcdef12345678
`,
			want: []Trailer{
				{Key: "Co-authored-by", Value: "Alice <alice@example.com>"},
				{Key: "Co-authored-by", Value: "Bob <bob@example.com>"},
				{Key: "Reviewed-by", Value: "Charlie <charlie@example.com>"},
				{Key: "Change-Id", Value: "I1234567890abcdef1234567890abcdef12345678"},
			},
		},
		{
			name: "colon in body",
			description: `chore: update itertools to version 0.14.0

Summary: Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod
tempor incididunt ut labore et dolore magna aliqua.

Change-Id: I1234567890abcdef1234567890abcdef12345678
`,
			want: []Trailer{
				{Key: "Change-Id", Value: "I1234567890abcdef1234567890abcdef12345678"},
			},
		},
		{
			name: "multiline trailer",
			description: `chore: update itertools to version 0.14.0

key: This is a very long value, with spaces and
  newlines in it.
`,
			want: []Trailer{
				{Key: "key", Value: "This is a very long value, with spaces and\n  newlines in it."},
			},
		},
		{
			name: "ignore line in trailer",
			description: `chore: update itertools to version 0.14.0

Signed-off-by: Random J Developer <random@developer.example.org>
[lucky@maintainer.example.org: struct foo moved from foo.c to foo.h]
Signed-off-by: Lucky K Maintainer <lucky@maintainer.example.org>
`,
			want: []Trailer{
				{Key: "Signed-off-by", Value: "Random J Developer <random@developer.example.org>"},
				{Key: "Signed-off-by", Value: "Lucky K Maintainer <lucky@maintainer.example.org>"},
			},
		},
		{
			name:        "single line description",
			description: `chore: update itertools to version 0.14.0`,
			want:        nil,
		},
		{
			name: "blank line after trailer",
			description: `subject

foo: 1

`,
			want: []Trailer{{Key: "foo", Value: "1"}},
		},
		{
			name: "blank line inbetween",
			description: `subject

foo: 1

bar: 2
`,
			want: []Trailer{{Key: "bar", Value: "2"}},
		},
		{
			name: "no blank line",
			description: `subject: whatever
foo: 1
`,
			want: nil,
		},
		{
			name: "whitespace before key",
			description: `subject

 foo: 1
`,
			want: nil,
		},
		{
			name: "whitespace after key",
			description: `subject

foo : 1
`,
			want: []Trailer{{Key: "foo", Value: "1"}},
		},
		{
			name:        "whitespace around value",
			description: "subject\n\nfoo:  1 \n",
			want:        []Trailer{{Key: "foo", Value: "1"}},
		},
		{
			name:        "whitespace around multiline value",
			description: "subject\n\nfoo:  1 \n 2 \n",
			want:        []Trailer{{Key: "foo", Value: "1 \n 2"}},
		},
		{
			name:        "whitespace around multiple trailers",
			description: "subject\n\nfoo:  1 \nbar:  2 \n",
			want: []Trailer{
				{Key: "foo", Value: "1"},
				{Key: "bar", Value: "2"},
			},
		},
		{
			name: "no whitespace before value",
			description: `subject

foo:1
`,
			want: []Trailer{{Key: "foo", Value: "1"}},
		},
		{
			name: "empty value",
			description: `subject

foo:
`,
			want: []Trailer{{Key: "foo", Value: ""}},
		},
		{
			name: "invalid key (underscore)",
			description: `subject

f_o_o: bar
`,
			want: nil,
		},
		{
			name: "content after trailer",
			description: `subject

foo: bar
baz
`,
			want: nil,
		},
		{
			name: "invalid content after trailer (blank line)",
			description: `subject

foo: bar

baz
`,
			want: nil,
		},
		{
			name:        "empty description",
			description: "",
			want:        nil,
		},
		{
			name: "cherry pick trailer",
			description: `subject

some non-trailer text
foo: bar
(cherry picked from commit 72bb9f9cf4bbb6bbb11da9cda4499c55c44e87b9)
`,
			want: []Trailer{{Key: "foo", Value: "bar"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseDescriptionTrailers(tt.description)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseDescriptionTrailers() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestParseTrailers(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      []Trailer
		wantErr   bool
		errorKind TrailerErrorKind
	}{
		{
			name: "valid trailers",
			input: `foo: 1
bar: 2
`,
			want: []Trailer{
				{Key: "foo", Value: "1"},
				{Key: "bar", Value: "2"},
			},
		},
		{
			name: "blank line in trailers",
			input: `foo: 1

foo: 2
`,
			wantErr:   true,
			errorKind: TrailerErrorBlankLine,
		},
		{
			name: "non-trailer line",
			input: `bar
foo: 1
`,
			wantErr:   true,
			errorKind: TrailerErrorNonTrailerLine,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTrailers(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTrailers() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				if tErr, ok := err.(*TrailerParseError); !ok || tErr.Kind != tt.errorKind {
					t.Errorf("ParseTrailers() error kind = %v, want %v", err, tt.errorKind)
				}
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseTrailers() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetTrailer(t *testing.T) {
	trailers := []Trailer{
		{Key: "foo", Value: "1"},
		{Key: "Bar", Value: "2"},
		{Key: "foo", Value: "3"},
	}

	tests := []struct {
		name      string
		key       string
		want      Trailer
		wantFound bool
	}{
		{"exact match", "foo", Trailer{Key: "foo", Value: "1"}, true},
		{"case insensitive match", "FOO", Trailer{Key: "foo", Value: "1"}, true},
		{"case insensitive match 2", "bar", Trailer{Key: "Bar", Value: "2"}, true},
		{"not found", "baz", Trailer{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := GetTrailer(trailers, tt.key)
			if found != tt.wantFound {
				t.Errorf("GetTrailer() found = %v, want %v", found, tt.wantFound)
			}
			if found {
				if got == nil {
					t.Errorf("GetTrailer() = nil, want %v", tt.want)
				} else if *got != tt.want {
					t.Errorf("GetTrailer() = %v, want %v", *got, tt.want)
				}
			}
		})
	}
}

func TestGetAllTrailers(t *testing.T) {
	trailers := []Trailer{
		{Key: "foo", Value: "1"},
		{Key: "Bar", Value: "2"},
		{Key: "Foo", Value: "3"},
	}

	tests := []struct {
		name string
		key  string
		want []Trailer
	}{
		{"multiple matches", "FOO", []Trailer{{Key: "foo", Value: "1"}, {Key: "Foo", Value: "3"}}},
		{"single match", "bar", []Trailer{{Key: "Bar", Value: "2"}}},
		{"no match", "baz", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetAllTrailers(trailers, tt.key)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("GetAllTrailers() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSetTrailer(t *testing.T) {
	trailers := []Trailer{
		{Key: "foo", Value: "1"},
		{Key: "bar", Value: "2"},
	}

	tests := []struct {
		name  string
		key   string
		value string
		want  []Trailer
	}{
		{
			name:  "replace existing",
			key:   "FOO",
			value: "updated",
			want: []Trailer{
				{Key: "FOO", Value: "updated"},
				{Key: "bar", Value: "2"},
			},
		},
		{
			name:  "add new",
			key:   "baz",
			value: "3",
			want: []Trailer{
				{Key: "foo", Value: "1"},
				{Key: "bar", Value: "2"},
				{Key: "baz", Value: "3"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SetTrailer(trailers, tt.key, tt.value)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("SetTrailer() mismatch (-want +got):\n%s", diff)
			}
			// Verify original slice is unchanged (shallow check of first element)
			if trailers[0].Value != "1" {
				t.Errorf("SetTrailer() mutated original slice")
			}
		})
	}
}

func TestAddTrailer(t *testing.T) {
	trailers := []Trailer{
		{Key: "foo", Value: "1"},
	}

	tests := []struct {
		name  string
		key   string
		value string
		want  []Trailer
	}{
		{
			name:  "add new",
			key:   "bar",
			value: "2",
			want: []Trailer{
				{Key: "foo", Value: "1"},
				{Key: "bar", Value: "2"},
			},
		},
		{
			name:  "allow duplicate",
			key:   "foo",
			value: "3",
			want: []Trailer{
				{Key: "foo", Value: "1"},
				{Key: "foo", Value: "3"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AddTrailer(trailers, tt.key, tt.value)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("AddTrailer() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRemoveTrailer(t *testing.T) {
	trailers := []Trailer{
		{Key: "foo", Value: "1"},
		{Key: "Bar", Value: "2"},
		{Key: "Foo", Value: "3"},
	}

	tests := []struct {
		name  string
		input []Trailer
		key   string
		want  []Trailer
	}{
		{
			name:  "remove all matching",
			input: trailers,
			key:   "FOO",
			want:  []Trailer{{Key: "Bar", Value: "2"}},
		},
		{
			name:  "remove non-existent",
			input: trailers,
			key:   "baz",
			want: []Trailer{
				{Key: "foo", Value: "1"},
				{Key: "Bar", Value: "2"},
				{Key: "Foo", Value: "3"},
			},
		},
		{
			name:  "remove from empty",
			input: []Trailer{},
			key:   "foo",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RemoveTrailer(tt.input, tt.key)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("RemoveTrailer() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFormatTrailer(t *testing.T) {
	tests := []struct {
		name    string
		trailer Trailer
		want    string
	}{
		{"simple", Trailer{Key: "foo", Value: "bar"}, "foo: bar"},
		{"multiline", Trailer{Key: "foo", Value: "line1\n line2"}, "foo: line1\n line2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatTrailer(tt.trailer); got != tt.want {
				t.Errorf("FormatTrailer() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatTrailers(t *testing.T) {
	tests := []struct {
		name     string
		trailers []Trailer
		want     string
	}{
		{"empty", []Trailer{}, ""},
		{
			name: "multiple trailers",
			trailers: []Trailer{
				{Key: "foo", Value: "1"},
				{Key: "bar", Value: "2"},
			},
			want: "foo: 1\nbar: 2",
		},
		{
			name: "with multiline",
			trailers: []Trailer{
				{Key: "foo", Value: "line1\n line2"},
				{Key: "bar", Value: "3"},
			},
			want: "foo: line1\n line2\nbar: 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatTrailers(tt.trailers); got != tt.want {
				t.Errorf("FormatTrailers() = %q, want %q", got, tt.want)
			}
		})
	}
}
