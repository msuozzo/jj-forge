package detach

import (
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRewriteArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "replaces detach flag",
			args: []string{"check", "--force", "--detach", "@-"},
			want: []string{"check", "--force", "--_detached", "@-"},
		},
		{
			name: "appends if missing",
			args: []string{"check", "@-"},
			want: []string{"check", "@-", "--_detached"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rewriteArgs(tt.args, "--detach", "--_detached")
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("rewriteArgs() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRewriteArgs_DoesNotMutateInput(t *testing.T) {
	args := []string{"check", "--detach"}
	orig := []string{"check", "--detach"}
	_ = rewriteArgs(args, "--detach", "--_detached")
	if diff := cmp.Diff(orig, args); diff != "" {
		t.Errorf("input was mutated (-want +got):\n%s", diff)
	}
}

func TestStart_AppendsToLog(t *testing.T) {
	dir := t.TempDir()
	proc := New("check", dir, NoTransform())
	if err := os.WriteFile(proc.LogPath(), []byte("earlier run\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The child is this test binary, which runs no tests and exits.
	for range 2 {
		if _, err := proc.Start([]string{"jj-forge", "-test.run=^$"}); err != nil {
			t.Fatalf("Start: %v", err)
		}
	}
	data, err := os.ReadFile(proc.LogPath())
	if err != nil {
		t.Fatal(err)
	}
	log := string(data)
	if !strings.HasPrefix(log, "earlier run\n") {
		t.Errorf("log lost the earlier run: %q", log)
	}
	if n := strings.Count(log, "=== "); n != 2 {
		t.Errorf("log has %d run headers, want 2: %q", n, log)
	}
}

func TestLogPath(t *testing.T) {
	proc := New("check", "/some/dir", NoTransform())
	want := "/some/dir/check.log"
	if got := proc.LogPath(); got != want {
		t.Errorf("LogPath() = %q, want %q", got, want)
	}
}

func TestFlagReplace(t *testing.T) {
	transform := FlagReplace("--detach", "--_detached")
	got := transform([]string{"check", "--force", "--detach", "@-"})
	want := []string{"check", "--force", "--_detached", "@-"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("FlagReplace() mismatch (-want +got):\n%s", diff)
	}
}

func TestNoTransform(t *testing.T) {
	transform := NoTransform()
	got := transform([]string{"check", "--force"})
	want := []string{"check", "--force"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("NoTransform() mismatch (-want +got):\n%s", diff)
	}
}
