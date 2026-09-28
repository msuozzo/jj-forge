package actionsstatus

import "testing"

func TestFilterPatterns(t *testing.T) {
	tests := []struct {
		patterns []string
		name     string
		want     bool
	}{
		{[]string{"*.go"}, "main.go", true},
		{[]string{"*.go"}, "cmd/main.go", false}, // * stops at /
		{[]string{"**.go"}, "cmd/main.go", true},
		{[]string{"docs/**"}, "docs/a/b.md", true},
		{[]string{"**/README.md"}, "README.md", true},
		{[]string{"release/v[0-9]"}, "release/v2", true},
		{[]string{"feature/**", "!feature/wip/**"}, "feature/wip/x", false},
		{[]string{"!feature/wip/**", "feature/**"}, "feature/wip/x", true}, // The last match decides.
		{[]string{"v1.+"}, "v1....", true},
		{[]string{`a\*b`}, "a*b", true},
		{[]string{`a\*b`}, "axb", false},
		{[]string{"*.jsx?"}, "app.js", true},
		{[]string{"*.jsx?"}, "app.jsx", true},
		{[]string{"[CB]at"}, "Cat", true},
		{[]string{"[CB]at"}, "Rat", false},
		{[]string{"**/migrate-*.sql"}, "migrate-10909.sql", true},
		{[]string{"**/migrate-*.sql"}, "db/migrate-v1.0.sql", true},
		{[]string{"**/docs/**"}, "docs/hello.md", true},
		{[]string{"**/docs/**"}, "dir/docs/my-file.txt", true},
		{[]string{"**/docs/**"}, "space/docs/plan/space.doc", true},
		{[]string{"releases/**-alpha"}, "releases/10/beta-alpha", true},
		{[]string{"?x"}, "?x", true}, // A leading ? has nothing to repeat.
		{[]string{"main"}, "main2", false},
	}
	for _, tt := range tests {
		f, err := compileFilter(tt.patterns)
		if err != nil {
			t.Fatalf("compileFilter(%q) error = %v", tt.patterns, err)
		}
		if got := matches(f, tt.name); got != tt.want {
			t.Errorf("matches(%q, %q) = %v, want %v", tt.patterns, tt.name, got, tt.want)
		}
	}
	for _, bad := range []string{"", "!", "[]", "[a-]", "[a_b]", "[ab", `trailing\`} {
		if _, err := compileFilter([]string{bad}); err == nil {
			t.Errorf("compileFilter(%q) succeeded, want an error", bad)
		}
	}
}
