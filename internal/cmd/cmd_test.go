package cmd

import (
	"context"
	"testing"
)

type fakePrompter struct {
	response bool
	called   bool
	prompt   string
}

func (f *fakePrompter) Confirm(prompt string, defaultAccept bool) (bool, error) {
	f.called = true
	f.prompt = prompt
	return f.response, nil
}

func TestMatchesOp(t *testing.T) {
	patterns := [][]string{
		{"describe"},
		{"git", "push"},
		{"bookmark", "set"},
		{"bookmark", "delete"},
	}

	tests := []struct {
		name     string
		args     []string
		patterns [][]string
		want     bool
	}{
		{"exact match single", []string{"describe"}, patterns, true},
		{"exact match multi", []string{"git", "push"}, patterns, true},
		{"prefix match with extra args", []string{"describe", "abc123"}, patterns, true},
		{"prefix match multi with extra args", []string{"git", "push", "--remote", "og"}, patterns, true},
		{"no match", []string{"log"}, patterns, false},
		{"partial multi no match", []string{"git", "fetch"}, patterns, false},
		{"empty args", []string{}, patterns, false},
		{"bookmark set match", []string{"bookmark", "set", "main"}, patterns, true},
		{"bookmark delete match", []string{"bookmark", "delete", "old"}, patterns, true},
		{"bookmark list no match", []string{"bookmark", "list"}, patterns, false},
		{"with -R prefix stripped", []string{"-R", "/some/path", "describe", "abc"}, patterns, true},
		{"with -R prefix stripped multi", []string{"-R", "/some/path", "git", "push"}, patterns, true},
		{"with -R prefix no match", []string{"-R", "/some/path", "log"}, patterns, false},
		{"nil patterns matches all", []string{"log"}, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesOp(tt.args, tt.patterns)
			if got != tt.want {
				t.Errorf("matchesOp(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestNewPromptingExecutor_WriteConfirmed(t *testing.T) {
	innerCalled := false
	inner := func(ctx context.Context, opts Opts, args ...string) (*Result, error) {
		innerCalled = true
		return &Result{Stdout: "ok"}, nil
	}
	prompter := &fakePrompter{response: true}
	patterns := [][]string{{"describe"}}

	executor := NewPromptingExecutor(inner, prompter, patterns)
	out, err := executor(context.Background(), Opts{}, "jj", "describe", "abc123")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Stdout != "ok" {
		t.Errorf("got output %q, want %q", out.Stdout, "ok")
	}
	if !prompter.called {
		t.Error("prompter was not called")
	}
	if !innerCalled {
		t.Error("inner executor was not called")
	}
}

func TestNewPromptingExecutor_WriteDenied(t *testing.T) {
	innerCalled := false
	inner := func(ctx context.Context, opts Opts, args ...string) (*Result, error) {
		innerCalled = true
		return &Result{Stdout: "ok"}, nil
	}
	prompter := &fakePrompter{response: false}
	patterns := [][]string{{"describe"}}

	executor := NewPromptingExecutor(inner, prompter, patterns)
	_, err := executor(context.Background(), Opts{}, "jj", "describe", "abc123")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "command aborted by user" {
		t.Errorf("got error %q, want %q", err.Error(), "command aborted by user")
	}
	if prompter.called != true {
		t.Error("prompter was not called")
	}
	if innerCalled {
		t.Error("inner executor should not have been called")
	}
}

func TestNewPromptingExecutor_AllPromptsEverything(t *testing.T) {
	innerCalled := false
	inner := func(ctx context.Context, opts Opts, args ...string) (*Result, error) {
		innerCalled = true
		return &Result{Stdout: "ok"}, nil
	}
	prompter := &fakePrompter{response: true}

	executor := NewPromptingExecutor(inner, prompter, nil)
	out, err := executor(context.Background(), Opts{}, "jj", "log")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Stdout != "ok" {
		t.Errorf("got output %q, want %q", out.Stdout, "ok")
	}
	if !prompter.called {
		t.Error("prompter was not called for read command with nil patterns")
	}
	if !innerCalled {
		t.Error("inner executor was not called")
	}
}

func TestNewPromptingExecutor_ReadSkipsPrompt(t *testing.T) {
	innerCalled := false
	inner := func(ctx context.Context, opts Opts, args ...string) (*Result, error) {
		innerCalled = true
		return &Result{Stdout: "ok"}, nil
	}
	prompter := &fakePrompter{response: false}
	patterns := [][]string{{"describe"}}

	executor := NewPromptingExecutor(inner, prompter, patterns)
	out, err := executor(context.Background(), Opts{}, "jj", "log")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Stdout != "ok" {
		t.Errorf("got output %q, want %q", out.Stdout, "ok")
	}
	if prompter.called {
		t.Error("prompter should not have been called for non-matching command")
	}
	if !innerCalled {
		t.Error("inner executor was not called")
	}
}
