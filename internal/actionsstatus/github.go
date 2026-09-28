package actionsstatus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/msuozzo/jj-forge/internal/forge"
)

// pullRequestQuery reads a pull request's head and its checks in one request,
// so the checks always belong to the head it reports. pullRequestJQ flattens
// the response into a pullRequest.
const (
	pullRequestQuery = `query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    defaultBranchRef { target { oid } }
    pullRequest(number: $number) {
      createdAt baseRefName headRefName isCrossRepository mergeable changedFiles
      headRepository { nameWithOwner }
      potentialMergeCommit { oid parents(first: 2) { nodes { oid } } }
      commits(last: 1) { nodes { commit {
        oid message statusCheckRollup { state }
        checkSuites(first: 100) { nodes { status conclusion workflowRun { event file { path } workflow { name } } } }
      } } }
    }
  }
}`
	pullRequestJQ = `.data.repository as $r | $r.pullRequest | {
  createdAt, baseRefName, headRefName, isCrossRepository, mergeable, changedFiles,
  defaultBranchOID: $r.defaultBranchRef.target.oid,
  headRepo: .headRepository.nameWithOwner,
  mergeCommit: .potentialMergeCommit.oid,
  mergeParents: [.potentialMergeCommit.parents.nodes[]?.oid],
  head: (.commits.nodes[0].commit // {} | {oid, message, rollupState: .statusCheckRollup.state,
    checkSuites: [.checkSuites.nodes[]? | {status, conclusion, workflow: .workflowRun.workflow.name,
      path: .workflowRun.file.path, event: .workflowRun.event}]})}`
)

// workflowFilesQuery reads the workflow files in a commit, and workflowFilesJQ
// flattens them into workflowFiles. A file GitHub cannot show as text gets an
// empty text, which fails to parse.
const (
	workflowFilesQuery = `query($owner: String!, $name: String!, $expr: String!) {
  repository(owner: $owner, name: $name) {
    object(expression: $expr) { ... on Tree { entries { name type object { ... on Blob { text } } } } }
  }
}`
	workflowFilesJQ = `[.data.repository.object.entries[]? | select(.type == "blob" and (.name | test("\\.ya?ml$")))
  | {path: (".github/workflows/" + .name), text: (.object.text // "")}]`
)

// pullRequest is a pull request and the checks on its head commit.
type pullRequest struct {
	CreatedAt         time.Time
	BaseRefName       string
	HeadRefName       string
	HeadRepo          string // owner/name of the repository holding the head branch
	IsCrossRepository bool
	Mergeable         string // MERGEABLE, CONFLICTING or UNKNOWN
	ChangedFiles      int
	DefaultBranchOID  string   // Commit the repository's default branch points to
	MergeCommit       string   // GitHub's test merge commit, or "" until it has tried the merge
	MergeParents      []string // Parents of MergeCommit: the base and the head it merged
	Head              struct {
		OID, Message string
		RollupState  string // Combined state of all checks and statuses, or "" until one reports
		CheckSuites  []checkSuite
	}
}

// checkSuite is a check suite on a commit.
type checkSuite struct {
	Status, Conclusion string
	Workflow           string // Name of the Actions workflow, or "" for other apps' suites
	Path               string // Workflow file, e.g. .github/workflows/ci.yml
	Event              string // Event that started the workflow run, e.g. pull_request
}

// workflowFile is a workflow definition read from the repository.
type workflowFile struct {
	Path, Text string
}

// reader reads a repository through gh api. Within one wait, everything but
// the pull request itself is read once.
type reader struct {
	gh    GitHub
	host  string
	repo  string // owner/name
	cache map[string]string
}

func newReader(gh GitHub, repoURI string) (*reader, error) {
	normalizedURI, err := forge.NormalizeRepoURL(repoURI)
	if err != nil {
		return nil, fmt.Errorf("invalid repository URI: %w", err)
	}
	u, err := url.Parse(normalizedURI)
	if err != nil {
		return nil, fmt.Errorf("invalid repository URI: %w", err)
	}
	if strings.Count(strings.Trim(u.Path, "/"), "/") != 1 {
		return nil, fmt.Errorf("invalid repository URI: %s", normalizedURI)
	}
	return &reader{gh: gh, host: u.Host, repo: strings.Trim(u.Path, "/"), cache: map[string]string{}}, nil
}

// api runs a gh api request. Unless fresh, a non-empty result is remembered.
func (r *reader) api(ctx context.Context, fresh bool, args ...string) (string, error) {
	key := strings.Join(args, "\x00")
	if out, ok := r.cache[key]; ok && !fresh {
		return out, nil
	}
	out, err := r.gh.API(ctx, append([]string{"--hostname", r.host}, args...)...)
	if err == nil && !fresh && strings.TrimSpace(out) != "" {
		r.cache[key] = out
	}
	return out, err
}

// graphql runs a GraphQL query on the repository, with flags for the other
// variables.
func (r *reader) graphql(ctx context.Context, fresh bool, query, jq string, vars ...string) (string, error) {
	owner, name, _ := strings.Cut(r.repo, "/")
	args := append([]string{"graphql", "-f", "owner=" + owner, "-f", "name=" + name}, vars...)
	return r.api(ctx, fresh, append(args, "-f", "query="+query, "--jq", jq)...)
}

func (r *reader) pullRequest(ctx context.Context, number string) (*pullRequest, error) {
	out, err := r.graphql(ctx, true, pullRequestQuery, pullRequestJQ, "-F", "number="+number)
	if err != nil {
		return nil, fmt.Errorf("failed to read checks for PR #%s: %w", number, err)
	}
	var pr pullRequest
	if err := json.Unmarshal([]byte(out), &pr); err != nil {
		return nil, fmt.Errorf("failed to parse checks for PR #%s: %w", number, err)
	}
	if pr.Head.OID == "" {
		return nil, fmt.Errorf("PR #%s has no commits", number)
	}
	if pr.HeadRepo == "" {
		pr.HeadRepo = r.repo
	}
	return &pr, nil
}

// workflowFiles reads the files in .github/workflows at a commit.
func (r *reader) workflowFiles(ctx context.Context, commit string) ([]workflowFile, error) {
	out, err := r.graphql(ctx, false, workflowFilesQuery, workflowFilesJQ, "-f", "expr="+commit+":.github/workflows")
	if err != nil {
		return nil, fmt.Errorf("failed to read workflow files at %.12s: %w", commit, err)
	}
	var files []workflowFile
	if err := json.Unmarshal([]byte(out), &files); err != nil {
		return nil, fmt.Errorf("failed to parse workflow files at %.12s: %w", commit, err)
	}
	return files, nil
}

// lines runs a gh api request whose --jq prints one value per line.
func (r *reader) lines(ctx context.Context, args ...string) ([]string, error) {
	out, err := r.api(ctx, false, args...)
	return slices.DeleteFunc(strings.Split(out, "\n"), func(s string) bool { return s == "" }), err
}

// disabledWorkflows returns the paths of workflows that GitHub will not run.
// Workflows it does not list, such as ones a pull request adds, are enabled.
func (r *reader) disabledWorkflows(ctx context.Context) ([]string, error) {
	paths, err := r.lines(ctx, "--paginate", "repos/"+r.repo+"/actions/workflows?per_page=100",
		"--jq", `.workflows[] | select(.state | startswith("disabled")) | .path`)
	if err != nil {
		return nil, fmt.Errorf("failed to list workflows for %s: %w", r.repo, err)
	}
	return paths, nil
}

// changedFiles lists the files a pull request changes, with the old names of
// renamed files.
func (r *reader) changedFiles(ctx context.Context, number string) ([]string, error) {
	files, err := r.lines(ctx, "--paginate", "repos/"+r.repo+"/pulls/"+number+"/files?per_page=100",
		"--jq", `.[] | .filename, (.previous_filename // empty)`)
	if err != nil {
		return nil, fmt.Errorf("failed to list changed files for PR #%s: %w", number, err)
	}
	return files, nil
}

// branchAt returns the commit a branch points to on GitHub, read from git
// rather than from the pull request, which can lag.
func (r *reader) branchAt(ctx context.Context, repo, branch string) (string, error) {
	out, err := r.api(ctx, true, "repos/"+repo+"/git/ref/heads/"+branch, "--jq", ".object.sha")
	if err != nil {
		return "", fmt.Errorf("failed to read branch %s of %s: %w", branch, repo, err)
	}
	return strings.TrimSpace(out), nil
}

// push is a push to a branch, as GitHub lists it in the repository activity.
type push struct {
	kind   string // branch_creation, push or force_push
	before string // The branch's previous commit
	at     time.Time
}

// headPush finds the push that moved a branch to head, or returns nil when
// GitHub does not list it (yet).
func (r *reader) headPush(ctx context.Context, repo, branch, head string) (*push, error) {
	out, err := r.api(ctx, false, "repos/"+repo+"/activity?ref="+url.QueryEscape("refs/heads/"+branch)+"&per_page=30",
		"--jq", fmt.Sprintf(`first(.[] | select(.after == %q)) | [.activity_type, .before, .timestamp] | @tsv`, head))
	if err != nil {
		return nil, fmt.Errorf("failed to read pushes to %s of %s: %w", branch, repo, err)
	}
	f := strings.Split(strings.TrimSpace(out), "\t")
	if len(f) != 3 {
		return nil, nil
	}
	at, err := time.Parse(time.RFC3339, f[2])
	if err != nil {
		return nil, nil
	}
	return &push{kind: f[0], before: f[1], at: at}, nil
}

// treeDiff lists the paths that differ between two commits, which is how
// GitHub compares a push to an existing branch (a two-dot diff). It returns
// nil when GitHub truncates a tree listing.
func (r *reader) treeDiff(ctx context.Context, from, to string) ([]string, error) {
	var trees [2]map[string]string
	for i, commit := range []string{from, to} {
		lines, err := r.lines(ctx, "repos/"+r.repo+"/git/trees/"+commit+"?recursive=1",
			"--jq", `if .truncated then "truncated" else (.tree[] | select(.type != "tree") | .sha + " " + .path) end`)
		if err != nil {
			return nil, fmt.Errorf("failed to read the files of %.12s: %w", commit, err)
		}
		if slices.Contains(lines, "truncated") {
			return nil, nil
		}
		trees[i] = map[string]string{}
		for _, l := range lines {
			id, p, _ := strings.Cut(l, " ")
			trees[i][p] = id
		}
	}
	diff := []string{}
	for p, id := range trees[0] {
		if trees[1][p] != id {
			diff = append(diff, p)
		}
	}
	for p := range trees[1] {
		if _, ok := trees[0][p]; !ok {
			diff = append(diff, p)
		}
	}
	return diff, nil
}
