package actionsstatus

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

const (
	headOID  = "c0ffeec0ffee"
	mergeOID = "3e43e3e43e43"
	baseOID  = "ba5eba5eba5e"
	oldOID   = "0ld0ld0ld0ld"
)

// fakeGitHub answers the gh api requests made by Wait with what their --jq
// expressions would print.
type fakeGitHub struct {
	t             *testing.T
	prs           []*pullRequest    // In order, the last one repeating
	workflows     map[string]string // Workflow file name to content at the merge commit
	baseWorkflows map[string]string // The same on the default branch, defaulting to workflows
	disabled      []string          // Paths of disabled workflows
	files         []string          // The pull request's changed files
	push          string            // Activity for the head as "kind\tbefore\ttimestamp", or "" when not listed
	branchAt      string            // Where git says the head branch is, defaulting to headOID
	trees         map[string]string // Commit to "id path" lines, or "truncated"
	calls         [][]string
}

func (a *fakeGitHub) API(ctx context.Context, args ...string) (string, error) {
	a.calls = append(a.calls, args)
	has := func(sub string) bool {
		return slices.ContainsFunc(args, func(s string) bool { return strings.Contains(s, sub) })
	}
	switch {
	case slices.Contains(args, "query="+pullRequestQuery):
		pr := a.prs[0]
		if len(a.prs) > 1 {
			a.prs = a.prs[1:]
		}
		out, _ := json.Marshal(pr)
		return string(out), nil
	case slices.Contains(args, "query="+workflowFilesQuery):
		workflows := a.workflows
		if slices.Contains(args, "expr="+baseOID+":.github/workflows") && a.baseWorkflows != nil {
			workflows = a.baseWorkflows
		}
		files := []workflowFile{}
		for _, name := range slices.Sorted(maps.Keys(workflows)) {
			files = append(files, workflowFile{Path: ".github/workflows/" + name, Text: workflows[name]})
		}
		out, _ := json.Marshal(files)
		return string(out), nil
	case has("/actions/workflows"):
		return strings.Join(a.disabled, "\n"), nil
	case has("/files?"):
		return strings.Join(a.files, "\n"), nil
	case has("/activity?"):
		return a.push, nil
	case has("/git/ref/heads/"):
		if a.branchAt == "" {
			return headOID + "\n", nil
		}
		return a.branchAt + "\n", nil
	case has("/git/trees/"):
		for commit, tree := range a.trees {
			if has("/git/trees/" + commit + "?") {
				return tree, nil
			}
		}
	}
	a.t.Errorf("unexpected request: %v", args)
	return "", nil
}

// newPR returns a pull request whose head is headOID, merged into mergeOID on
// top of baseOID, changed by edit.
func newPR(edit func(*pullRequest)) *pullRequest {
	pr := &pullRequest{
		CreatedAt: time.Date(2026, 10, 3, 0, 0, 10, 0, time.UTC), BaseRefName: "main", HeadRefName: "push-abc",
		HeadRepo: "owner/repo", Mergeable: "MERGEABLE", MergeCommit: mergeOID, MergeParents: []string{baseOID, headOID},
		DefaultBranchOID: baseOID,
	}
	pr.Head.OID = headOID
	if edit != nil {
		edit(pr)
	}
	return pr
}

// run returns a check suite for a workflow run, or for another app without a file.
func run(status, conclusion, file, event string) checkSuite {
	if file == "" {
		return checkSuite{Status: status, Conclusion: conclusion}
	}
	return checkSuite{Status: status, Conclusion: conclusion, Workflow: strings.ToUpper(strings.TrimSuffix(file, ".yml")),
		Path: ".github/workflows/" + file, Event: event}
}

var (
	ciDone    = run("COMPLETED", "SUCCESS", "ci.yml", "pull_request")
	ciRunning = run("IN_PROGRESS", "", "ci.yml", "pull_request")
)

const (
	ciWorkflow   = "on: pull_request\njobs: {}\n"
	docsWorkflow = "on:\n  pull_request:\n    paths: ['docs/**']\njobs: {}\n"
	newBranch    = "branch_creation\t0000000000000000000000000000000000000000\t2026-10-03T00:00:05Z" // Before the PR was opened
	forcePush    = "force_push\t" + oldOID + "\t2026-10-03T00:01:00Z"
)

// rollup sets the combined state of the head's checks and its suites.
func rollup(state string, suites ...checkSuite) func(*pullRequest) {
	return func(pr *pullRequest) { pr.Head.RollupState, pr.Head.CheckSuites = state, suites }
}

func check(t *testing.T, api *fakeGitHub) (*Status, error) {
	t.Helper()
	return Wait(context.Background(), api, "git@github.com:owner/repo.git", "42", headOID, 0, 0, io.Discard)
}

func TestCheck_Requests(t *testing.T) {
	api := &fakeGitHub{t: t, prs: []*pullRequest{newPR(rollup("SUCCESS", ciDone))}, workflows: map[string]string{"ci.yml": ciWorkflow}}
	if _, err := check(t, api); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	gql := func(query, jq string, vars ...string) []string {
		args := append([]string{"--hostname", "github.com", "graphql", "-f", "owner=owner", "-f", "name=repo"}, vars...)
		return append(args, "-f", "query="+query, "--jq", jq)
	}
	want := [][]string{
		gql(pullRequestQuery, pullRequestJQ, "-F", "number=42"),
		gql(workflowFilesQuery, workflowFilesJQ, "-f", "expr="+mergeOID+":.github/workflows"),
		gql(workflowFilesQuery, workflowFilesJQ, "-f", "expr="+baseOID+":.github/workflows"),
		{"--hostname", "github.com", "--paginate", "repos/owner/repo/actions/workflows?per_page=100",
			"--jq", `.workflows[] | select(.state | startswith("disabled")) | .path`},
		{"--hostname", "github.com", "repos/owner/repo/activity?ref=refs%2Fheads%2Fpush-abc&per_page=30",
			"--jq", `first(.[] | select(.after == "c0ffeec0ffee")) | [.activity_type, .before, .timestamp] | @tsv`},
		{"--hostname", "github.com", "repos/owner/repo/git/ref/heads/push-abc", "--jq", ".object.sha"},
	}
	if diff := cmp.Diff(want, api.calls); diff != "" {
		t.Errorf("requests mismatch (-want +got):\n%s", diff)
	}
}

func TestCheck(t *testing.T) {
	const (
		target     = "on:\n  pull_request_target:\n    paths: ['target/**']\n"
		openedOnly = "on:\n  pull_request:\n    types: [opened]\n"
		pushPaths  = "on:\n  push:\n    paths: ['src/**']\n"
	)
	ci := map[string]string{"ci.yml": ciWorkflow}
	tests := []struct {
		name          string
		pr            func(*pullRequest)
		workflows     map[string]string
		baseWorkflows map[string]string
		disabled      []string
		files         []string
		push          string
		trees         map[string]string
		want          Status
	}{
		{
			name:      "push not registered",
			pr:        func(pr *pullRequest) { pr.Head.OID = oldOID },
			workflows: ci,
			want:      Status{Running, "GitHub has not registered c0ffeec0ffee yet (PR head is 0ld0ld0ld0ld)"},
		},
		{
			name:      "merge commit not made yet",
			pr:        func(pr *pullRequest) { pr.MergeCommit, pr.MergeParents = "", nil },
			workflows: ci,
			want:      Status{Running, "waiting for GitHub to test-merge c0ffeec0ffee into main"},
		},
		{
			name:      "merge commit of the previous head",
			pr:        func(pr *pullRequest) { pr.MergeParents = []string{baseOID, oldOID} },
			workflows: ci,
			want:      Status{Running, "waiting for GitHub to test-merge c0ffeec0ffee into main"},
		},
		{
			name:      "only a fast app has reported",
			pr:        rollup("SUCCESS", run("COMPLETED", "SUCCESS", "", "")),
			workflows: ci,
			want:      Status{Running, "waiting for ci.yml (pull_request) to start on c0ffeec0ffee"},
		},
		{
			name:      "a run for another event finished first",
			pr:        rollup("SUCCESS", run("COMPLETED", "SKIPPED", "ci.yml", "push")),
			workflows: ci,
			want:      Status{Running, "waiting for ci.yml (pull_request) to start on c0ffeec0ffee"},
		},
		{
			name:      "running while the rollup reads success",
			pr:        rollup("SUCCESS", ciRunning, run("IN_PROGRESS", "", "ci.yml", "push")),
			workflows: ci,
			want:      Status{Running, "running: CI"},
		},
		{
			name:      "failed while another runs",
			pr:        rollup("PENDING", run("COMPLETED", "FAILURE", "ci.yml", "pull_request"), run("QUEUED", "", "lint.yml", "pull_request")),
			workflows: ci,
			want:      Status{Failed, "CI (failure)"},
		},
		{
			name:      "other app failed before any workflow started",
			pr:        rollup("FAILURE"),
			workflows: ci,
			want:      Status{Failed, "checks from other apps failed"},
		},
		{
			name:      "other app pending",
			pr:        rollup("PENDING", ciDone),
			workflows: ci,
			want:      Status{Running, "checks from other apps have not finished"},
		},
		{
			name:      "passed despite a stuck app suite",
			pr:        rollup("SUCCESS", run("QUEUED", "", "", ""), ciDone, run("COMPLETED", "SKIPPED", "lint.yml", "pull_request")),
			workflows: ci,
			want:      Status{Passed, ""},
		},
		{
			name: "no workflows",
			want: Status{NoChecks, "no Actions workflows"},
		},
		{
			name:      "path filter skips the push",
			pr:        func(pr *pullRequest) { pr.ChangedFiles = 1 },
			workflows: map[string]string{"docs.yml": docsWorkflow},
			files:     []string{"src/main.go"},
			want:      Status{NoChecks, "skipped docs.yml (pull_request paths)"},
		},
		{
			name:      "path filter matches a renamed file's old name",
			pr:        func(pr *pullRequest) { pr.ChangedFiles = 1 },
			workflows: map[string]string{"docs.yml": docsWorkflow},
			files:     []string{"guide.md", "docs/guide.md"},
			want:      Status{Running, "waiting for docs.yml (pull_request) to start on c0ffeec0ffee"},
		},
		{
			name:      "too many files to filter",
			pr:        func(pr *pullRequest) { pr.ChangedFiles = 301 },
			workflows: map[string]string{"docs.yml": docsWorkflow},
			want:      Status{Running, "waiting for docs.yml (pull_request) to start on c0ffeec0ffee"},
		},
		{
			name:      "skip instruction",
			pr:        func(pr *pullRequest) { pr.Head.Message = "Fix typo [skip ci]" },
			workflows: ci,
			want:      Status{NoChecks, "skipped ci.yml ([skip ci] in the commit message)"},
		},
		{
			name:      "disabled workflow",
			workflows: ci,
			disabled:  []string{".github/workflows/ci.yml"},
			want:      Status{NoChecks, "skipped ci.yml (disabled)"},
		},
		{
			name:      "unparseable workflow is expected to run",
			workflows: map[string]string{"weird.yml": "on: [", "big.yml": ""},
			want:      Status{Running, "waiting for big.yml, weird.yml to start on c0ffeec0ffee"},
		},
		{
			name:      "no workflow runs and another app passed",
			pr:        rollup("SUCCESS"),
			workflows: map[string]string{"release.yml": "on:\n  push:\n    tags: ['v*']\n"},
			want:      Status{Passed, "no Actions workflow runs for this push, and the other checks passed"},
		},
		{
			name:      "push workflows run in the fork",
			pr:        func(pr *pullRequest) { pr.IsCrossRepository = true },
			workflows: map[string]string{"push.yml": "on: push\n"},
			want:      Status{NoChecks, "skipped push.yml (push runs in the fork)"},
		},
		{
			name:          "pull_request_target is read from the default branch",
			pr:            func(pr *pullRequest) { pr.ChangedFiles = 1 },
			workflows:     map[string]string{"target.yml": "on:\n  pull_request_target:\n    paths: ['never/**']\n"},
			baseWorkflows: map[string]string{"target.yml": target},
			files:         []string{"target/x"},
			want:          Status{Running, "waiting for target.yml (pull_request_target) to start on c0ffeec0ffee"},
		},
		{
			name:          "pull_request_target added by the pull request",
			workflows:     map[string]string{"target.yml": target},
			baseWorkflows: map[string]string{},
			want:          Status{NoChecks, "skipped target.yml (no pull_request or push trigger)"},
		},
		{
			name:      "opened only, on the first push",
			workflows: map[string]string{"opened.yml": openedOnly},
			push:      newBranch,
			want:      Status{Running, "waiting for opened.yml (pull_request) to start on c0ffeec0ffee"},
		},
		{
			name:      "opened only, on a later push",
			workflows: map[string]string{"opened.yml": openedOnly},
			push:      forcePush,
			want:      Status{NoChecks, "skipped opened.yml (pull_request types)"},
		},
		{
			name:      "opened only, push not listed yet",
			workflows: map[string]string{"opened.yml": openedOnly},
			want:      Status{Running, "waiting for opened.yml (pull_request) to start on c0ffeec0ffee"},
		},
		{
			name:      "push paths miss a force push",
			workflows: map[string]string{"push.yml": pushPaths},
			push:      forcePush,
			trees:     map[string]string{oldOID: "a1 src/a.go\nb1 README.md", headOID: "a1 src/a.go\nb2 README.md"},
			want:      Status{NoChecks, "skipped push.yml (push paths)"},
		},
		{
			name:      "push paths match a force push",
			workflows: map[string]string{"push.yml": pushPaths},
			push:      forcePush,
			trees:     map[string]string{oldOID: "a1 src/a.go", headOID: "a2 src/a.go"},
			want:      Status{Running, "waiting for push.yml (push) to start on c0ffeec0ffee"},
		},
		{
			name:      "push paths on a new branch use the pull request's files",
			pr:        func(pr *pullRequest) { pr.ChangedFiles = 1 },
			workflows: map[string]string{"push.yml": pushPaths},
			files:     []string{"docs/a.md"},
			push:      newBranch,
			want:      Status{NoChecks, "skipped push.yml (push paths)"},
		},
		{
			name:      "push paths when a tree is too large",
			workflows: map[string]string{"push.yml": pushPaths},
			push:      forcePush,
			trees:     map[string]string{oldOID: "truncated", headOID: "a1 docs/a.md"},
			want:      Status{Running, "waiting for push.yml (push) to start on c0ffeec0ffee"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeGitHub{
				t: t, prs: []*pullRequest{newPR(tt.pr)}, workflows: tt.workflows, baseWorkflows: tt.baseWorkflows,
				disabled: tt.disabled, files: tt.files, push: tt.push, trees: tt.trees,
			}
			got, err := check(t, api)
			if err != nil {
				t.Fatalf("Wait() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, *got); diff != "" {
				t.Errorf("Wait() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCheck_Errors(t *testing.T) {
	ci := map[string]string{"ci.yml": ciWorkflow}
	for name, tt := range map[string]struct {
		api  *fakeGitHub
		want string
	}{
		"merge conflict":                 {&fakeGitHub{prs: []*pullRequest{newPR(func(pr *pullRequest) { pr.Mergeable = "CONFLICTING" })}}, "PR #42 has merge conflicts with main"},
		"branch moved":                   {&fakeGitHub{prs: []*pullRequest{newPR(func(pr *pullRequest) { pr.Head.OID = "e1sewhere123" })}, branchAt: "e1sewhere123"}, "the branch of PR #42 has moved"},
		"branch moved while the PR lags": {&fakeGitHub{prs: []*pullRequest{newPR(rollup("SUCCESS", ciDone))}, workflows: ci, branchAt: "e1sewhere123"}, "the branch of PR #42 has moved"},
	} {
		t.Run(name, func(t *testing.T) {
			tt.api.t = t
			if _, err := check(t, tt.api); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Wait() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestWait(t *testing.T) {
	running, done := newPR(rollup("PENDING", ciRunning)), newPR(rollup("SUCCESS", ciDone))
	api := &fakeGitHub{t: t, prs: []*pullRequest{running, running, done}, workflows: map[string]string{"ci.yml": ciWorkflow}}
	var out bytes.Buffer
	got, err := Wait(context.Background(), api, "github.com/owner/repo", "42", headOID, time.Millisecond, 0, &out)
	if err != nil || got.Verdict != Passed {
		t.Fatalf("Wait() = %v, %v, want passed", got, err)
	}
	// Unchanged details print once, and what cannot change is read once.
	if diff := cmp.Diff("Waiting for checks: running: CI\n", out.String()); diff != "" {
		t.Errorf("output mismatch (-want +got):\n%s", diff)
	}
	if n := len(slices.DeleteFunc(api.calls, func(c []string) bool { return !slices.Contains(c, "query="+workflowFilesQuery) })); n != 2 {
		t.Errorf("read workflow files %d times, want 2 (merge commit and base)", n)
	}

	api = &fakeGitHub{t: t, prs: []*pullRequest{running}, workflows: map[string]string{"ci.yml": ciWorkflow}}
	got, err = Wait(context.Background(), api, "github.com/owner/repo", "42", headOID, time.Millisecond, 20*time.Millisecond, io.Discard)
	if diff := cmp.Diff(Status{Running, "gave up after 20ms: running: CI"}, *got); err != nil || diff != "" {
		t.Errorf("Wait() with a timeout = %v (-want +got):\n%s", err, diff)
	}
}
