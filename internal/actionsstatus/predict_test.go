package actionsstatus

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseTriggers(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []trigger
		wantErr bool
	}{
		{name: "event name", content: "on: push\n", want: []trigger{{Event: "push"}}},
		{name: "event list", content: "on: [push, pull_request]\n", want: []trigger{{Event: "push"}, {Event: "pull_request"}}},
		{
			name:    "event map",
			content: "on:\n  pull_request:\n  push:\n    branches: main\n    paths: ['src/**', '!src/gen/**']\n",
			want: []trigger{
				{Event: "pull_request", Filters: map[string][]string{}},
				{Event: "push", Filters: map[string][]string{"branches": {"main"}, "paths": {"src/**", "!src/gen/**"}}},
			},
		},
		{
			name:    "other events may hold anything",
			content: "on:\n  workflow_call:\n    inputs:\n      x: {type: string}\n  schedule:\n    - cron: '0 0 * * *'\n",
			want:    []trigger{{Event: "schedule", Filters: map[string][]string{}}, {Event: "workflow_call", Filters: map[string][]string{}}},
		},
		{
			name:    "empty filters are dropped",
			content: "on:\n  pull_request:\n    types:\n    paths:\n",
			want:    []trigger{{Event: "pull_request", Filters: map[string][]string{}}},
		},
		{
			name:    "anchors are resolved",
			content: "x: &main [main]\non:\n  push:\n    branches: *main\n",
			want:    []trigger{{Event: "push", Filters: map[string][]string{"branches": {"main"}}}},
		},
		{name: "no on key", content: "jobs: {}\n", wantErr: true},
		{name: "unsupported filter shape", content: "on:\n  push:\n    branches: {main: true}\n", wantErr: true},
		{name: "invalid yaml", content: "on: [", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTriggers(tt.content)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseTriggers() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("parseTriggers() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestWorkflowEvents(t *testing.T) {
	pr := prContext{
		BaseBranch: "main", HeadBranch: "push-abc", SameRepo: true,
		Files:     []string{"src/a.go", "README.md"},
		PushFiles: []string{"README.md"},
	}
	tests := []struct {
		name       string
		content    string
		pr         *prContext // Defaults to pr
		want       []string   // Events that run
		wantReason string
	}{
		{name: "plain pull_request", content: "on: pull_request", want: []string{"pull_request"}},
		{name: "types without opened or synchronize", content: "on:\n  pull_request:\n    types: [labeled]\n", wantReason: "pull_request types"},
		{name: "types with synchronize", content: "on:\n  pull_request:\n    types: [synchronize]\n", want: []string{"pull_request"}},
		{name: "opened only, on a later push", content: "on:\n  pull_request:\n    types: [opened]\n", pr: &prContext{BaseBranch: "main", Action: "synchronize"}, wantReason: "pull_request types"},
		{name: "opened only, on the first push", content: "on:\n  pull_request:\n    types: [opened]\n", pr: &prContext{BaseBranch: "main", Action: "opened"}, want: []string{"pull_request"}},
		{name: "empty types filter nothing", content: "on:\n  pull_request:\n    types:\n", want: []string{"pull_request"}},
		{name: "base branch matches", content: "on:\n  pull_request:\n    branches: [main, 'release/**']\n", want: []string{"pull_request"}},
		{name: "base branch excluded", content: "on:\n  pull_request:\n    branches: [develop]\n", wantReason: "pull_request branches"},
		{name: "base branch ignored", content: "on:\n  pull_request:\n    branches-ignore: [main]\n", wantReason: "pull_request branches-ignore"},
		{name: "branches and branches-ignore together", content: "on:\n  pull_request:\n    branches: [x]\n    branches-ignore: [y]\n", want: []string{"pull_request"}},
		{name: "path matches", content: "on:\n  pull_request:\n    paths: ['src/**']\n", want: []string{"pull_request"}},
		{name: "empty paths filter nothing", content: "on:\n  pull_request:\n    paths:\n", want: []string{"pull_request"}},
		{name: "path negated later", content: "on:\n  pull_request:\n    paths: ['**.go', '!src/**']\n", wantReason: "pull_request paths"},
		{name: "every file ignored", content: "on:\n  pull_request:\n    paths-ignore: ['src/**', '*.md']\n", wantReason: "pull_request paths-ignore"},
		{name: "a file not ignored", content: "on:\n  pull_request:\n    paths-ignore: ['src/**']\n", want: []string{"pull_request"}},
		{name: "paths cannot be listed", content: "on:\n  pull_request:\n    paths: ['docs/**']\n", pr: &prContext{BaseBranch: "main"}, want: []string{"pull_request"}},
		{name: "invalid pattern", content: "on:\n  pull_request:\n    paths: ['[]']\n", want: []string{"pull_request"}},
		{name: "empty pattern", content: "on:\n  pull_request:\n    paths: ['']\n", want: []string{"pull_request"}},
		{name: "pull_request_target ignores skip instructions", content: "on: pull_request_target", pr: &prContext{BaseBranch: "main", SkipCI: "[skip ci]"}, want: []string{"pull_request_target"}},
		{name: "push to the head branch", content: "on: push", want: []string{"push"}},
		{name: "push and pull_request", content: "on: [push, pull_request]", want: []string{"push", "pull_request"}},
		{name: "push limited to main", content: "on:\n  push:\n    branches: [main]\n", wantReason: "push branches"},
		{name: "push on tags only", content: "on:\n  push:\n    tags: ['v*']\n", wantReason: "push on tags only"},
		{name: "push paths match the push", content: "on:\n  push:\n    paths: ['*.md']\n", want: []string{"push"}},
		{name: "push paths miss the push", content: "on:\n  push:\n    paths: ['src/**']\n", wantReason: "push paths"},
		{name: "push paths cannot be listed", content: "on:\n  push:\n    paths: ['src/**']\n", pr: &prContext{BaseBranch: "main", SameRepo: true}, want: []string{"push"}},
		{name: "push from a fork", content: "on: push", pr: &prContext{BaseBranch: "main"}, wantReason: "push runs in the fork"},
		{name: "unrelated events", content: "on: [workflow_dispatch, schedule]", wantReason: "no pull_request or push trigger"},
		{
			name:       "reasons are combined",
			content:    "on:\n  push:\n    branches: [main]\n  pull_request:\n    paths: ['docs/**']\n",
			wantReason: "pull_request paths, push branches",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			triggers, err := parseTriggers(tt.content)
			if err != nil {
				t.Fatalf("parseTriggers() error = %v", err)
			}
			p := pr
			if tt.pr != nil {
				p = *tt.pr
			}
			got, reason := workflowEvents(triggers, p)
			if !slices.Equal(got, tt.want) || reason != tt.wantReason {
				t.Errorf("workflowEvents() = %v, %q, want %v, %q", got, reason, tt.want, tt.wantReason)
			}
		})
	}
}

func TestSkipCIInstruction(t *testing.T) {
	tests := []struct {
		message, want string
	}{
		{"Fix typo [skip ci]", "[skip ci]"},
		{"Docs\n\nDetails [actions skip]\n", "[actions skip]"},
		{"Docs\n\n\nskip-checks: true\n", "skip-checks: true"},
		{"Docs\n\nskip-checks:true", ""}, // Needs two empty lines.
		{"Mention skip ci without brackets", ""},
	}
	for _, tt := range tests {
		if got := skipCIInstruction(tt.message); got != tt.want {
			t.Errorf("skipCIInstruction(%q) = %q, want %q", tt.message, got, tt.want)
		}
	}
}
