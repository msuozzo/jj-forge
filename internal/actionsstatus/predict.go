package actionsstatus

import (
	"context"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// expectation is a workflow run that a push is predicted to start.
type expectation struct {
	path  string // Workflow file, e.g. .github/workflows/ci.yml
	event string // Event of the run, or "" when any event will do
}

func (e expectation) String() string {
	if e.event == "" {
		return path.Base(e.path)
	}
	return fmt.Sprintf("%s (%s)", path.Base(e.path), e.event)
}

// startedBy reports whether s is a run that e expects.
func (e expectation) startedBy(s checkSuite) bool {
	return s.Path == e.path && (e.event == "" || s.Event == e.event)
}

// predict returns the workflow runs expected for the pull request's head, or
// why there are none. GitHub records nothing when its trigger filters skip a
// push, so the workflow files are evaluated the way GitHub evaluates them for
// a pull request that was opened or synchronized. Anything uncertain counts
// as a run that will start, so that callers never skip CI that is starting.
//
// GitHub reads pull_request workflows from the test merge commit, push
// workflows from the head (approximated by the merge commit), and
// pull_request_target workflows from the default branch. Path filters see the
// pull request's changed files, except for push workflows, which see the
// push's own diff: against the branch's previous commit, or for a new branch
// against the parent of its oldest new commit, which the pull request's files
// cover. The head must already be merged into pr.MergeCommit.
func (r *reader) predict(ctx context.Context, number string, pr *pullRequest) ([]expectation, string, error) {
	triggers := map[string][]trigger{} // By workflow file
	for i, commit := range []string{pr.MergeCommit, pr.DefaultBranchOID} {
		files, err := r.workflowFiles(ctx, commit)
		if err != nil {
			return nil, "", err
		}
		fromBase := i == 1
		for _, f := range files {
			t, err := parseTriggers(f.Text)
			if err != nil {
				t = []trigger{{}} // Unknown, so it runs for any event.
			}
			t = slices.DeleteFunc(t, func(t trigger) bool { return t.Event != "" && (t.Event == "pull_request_target") != fromBase })
			if !fromBase || len(t) > 0 {
				triggers[f.Path] = append(triggers[f.Path], t...)
			}
		}
	}
	disabled, err := r.disabledWorkflows(ctx)
	if err != nil {
		return nil, "", err
	}
	p, err := r.headPush(ctx, pr.HeadRepo, pr.HeadRefName, pr.Head.OID)
	if err != nil {
		return nil, "", err
	}
	c := prContext{
		BaseBranch: pr.BaseRefName,
		HeadBranch: pr.HeadRefName,
		SameRepo:   !pr.IsCrossRepository,
		SkipCI:     skipCIInstruction(pr.Head.Message),
	}
	switch {
	case p == nil:
	case p.at.Before(pr.CreatedAt):
		c.Action = "opened"
	case p.at.After(pr.CreatedAt):
		c.Action = "synchronize"
	}
	prFiles := func() ([]string, error) {
		if pr.ChangedFiles > maxFilteredFiles {
			return nil, nil
		}
		return r.changedFiles(ctx, number)
	}
	if usesPaths(triggers, "pull_request", "pull_request_target") {
		if c.Files, err = prFiles(); err != nil {
			return nil, "", err
		}
	}
	if usesPaths(triggers, "push") && c.SameRepo && p != nil {
		if p.kind == "branch_creation" {
			c.PushFiles, err = prFiles()
		} else if c.PushFiles, err = r.treeDiff(ctx, p.before, pr.Head.OID); len(c.PushFiles) > maxFilteredFiles {
			c.PushFiles = nil
		}
		if err != nil {
			return nil, "", err
		}
	}

	var expected []expectation
	var skipped []string
	for _, w := range slices.Sorted(maps.Keys(triggers)) {
		if slices.Contains(disabled, w) {
			skipped = append(skipped, path.Base(w)+" (disabled)")
			continue
		}
		events, reason := workflowEvents(triggers[w], c)
		for _, e := range events {
			expected = append(expected, expectation{path: w, event: e})
		}
		if len(events) == 0 {
			skipped = append(skipped, fmt.Sprintf("%s (%s)", path.Base(w), reason))
		}
	}
	switch {
	case len(expected) > 0:
		return expected, "", nil
	case len(skipped) == 0:
		return nil, "no Actions workflows", nil
	}
	return nil, "skipped " + strings.Join(skipped, ", "), nil
}

// usesPaths reports whether a trigger for one of events has a path filter.
func usesPaths(triggers map[string][]trigger, events ...string) bool {
	for _, ts := range triggers {
		for _, t := range ts {
			_, paths := t.Filters["paths"]
			_, ignore := t.Filters["paths-ignore"]
			if (paths || ignore) && slices.Contains(events, t.Event) {
				return true
			}
		}
	}
	return false
}

// prContext is what a workflow's triggers are evaluated against.
type prContext struct {
	BaseBranch string   // Branch the pull request targets
	HeadBranch string   // Branch the pull request is from
	SameRepo   bool     // The head branch is in the base repository, so pushes to it run push workflows there
	SkipCI     string   // Skip instruction in the head commit's message, if any
	Action     string   // opened or synchronize, or "" when unknown
	Files      []string // The pull request's changed files with old names of renamed files, or nil when unknown
	PushFiles  []string // Files the push to the head branch changed, or nil when unknown
}

// maxFilteredFiles is the number of changed files GitHub evaluates path
// filters against.
const maxFilteredFiles = 300

// skipCIMarkers are the commit message strings that stop push and
// pull_request workflows from running.
var skipCIMarkers = []string{"[skip ci]", "[ci skip]", "[no ci]", "[skip actions]", "[actions skip]"}

// skipCIInstruction returns the skip instruction in a commit message, or "".
func skipCIInstruction(message string) string {
	for _, m := range skipCIMarkers {
		if strings.Contains(message, m) {
			return m
		}
	}
	// A skip-checks trailer must follow two empty lines at the end.
	trimmed := strings.TrimRight(message, "\n")
	for _, t := range []string{"skip-checks:true", "skip-checks: true"} {
		if strings.HasSuffix(trimmed, "\n\n\n"+t) {
			return t
		}
	}
	return ""
}

// trigger is one event under a workflow's on: key.
type trigger struct {
	Event   string
	Filters map[string][]string // types, branches, branches-ignore, paths, paths-ignore, tags, tags-ignore
}

// parseTriggers reads the on: key of a workflow file. The key may hold an
// event name, a list of event names or a map of events to their filters.
func parseTriggers(content string) ([]trigger, error) {
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return nil, err
	}
	switch on := doc["on"].(type) {
	case string:
		return []trigger{{Event: on}}, nil
	case []any:
		var out []trigger
		for _, e := range on {
			event, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("unsupported event list entry")
			}
			out = append(out, trigger{Event: event})
		}
		return out, nil
	case map[string]any:
		var out []trigger
		for _, event := range slices.Sorted(maps.Keys(on)) {
			t := trigger{Event: event, Filters: map[string][]string{}}
			cfg, ok := on[event].(map[string]any)
			switch {
			case !relevantEvent(event) || on[event] == nil:
				// Other events, such as workflow_call with its inputs, can hold
				// any shape, and a relevant event with no filters has none to read.
			case !ok:
				return nil, fmt.Errorf("unsupported configuration for %s", event)
			default:
				for name, v := range cfg {
					switch v := v.(type) {
					case nil: // An empty filter, like a missing one, filters nothing.
					case string:
						t.Filters[name] = []string{v}
					case []any:
						for _, val := range v {
							s, ok := val.(string)
							if !ok {
								return nil, fmt.Errorf("unsupported filter %s.%s", event, name)
							}
							t.Filters[name] = append(t.Filters[name], s)
						}
					default:
						return nil, fmt.Errorf("unsupported filter %s.%s", event, name)
					}
				}
			}
			out = append(out, t)
		}
		return out, nil
	case nil:
		return nil, fmt.Errorf("workflow has no on: key")
	}
	return nil, fmt.Errorf("unsupported on: value")
}

// relevantEvent reports whether a push to a pull request can trigger event.
func relevantEvent(event string) bool {
	return event == "pull_request" || event == "pull_request_target" || event == "push"
}

// workflowEvents returns the events of a workflow's triggers that run for the
// push. When none does, reason says why.
func workflowEvents(triggers []trigger, c prContext) (events []string, reason string) {
	var reasons []string
	for _, t := range triggers {
		ok, why := triggerRuns(t, c)
		switch {
		case ok && !slices.Contains(events, t.Event):
			events = append(events, t.Event)
		case !ok && why != "" && !slices.Contains(reasons, why):
			reasons = append(reasons, why)
		}
	}
	if len(events) == 0 && len(reasons) == 0 {
		return nil, "no pull_request or push trigger"
	}
	return events, strings.Join(reasons, ", ")
}

// triggerRuns evaluates one trigger for a push to a pull request. An
// unknown trigger, with no event, runs.
func triggerRuns(t trigger, c prContext) (bool, string) {
	switch t.Event {
	case "":
		return true, ""
	case "pull_request", "pull_request_target":
		if t.Event == "pull_request" && c.SkipCI != "" {
			return false, c.SkipCI + " in the commit message"
		}
		if types, ok := t.Filters["types"]; ok {
			matched := slices.Contains(types, c.Action)
			if c.Action == "" {
				matched = slices.Contains(types, "opened") || slices.Contains(types, "synchronize")
			}
			if !matched {
				return false, t.Event + " types"
			}
		}
		if ok, why := refFilter(t, "branches", c.BaseBranch); !ok {
			return false, why
		}
		return pathFilter(t, c.Files)
	case "push":
		if !c.SameRepo {
			return false, "push runs in the fork"
		}
		if c.SkipCI != "" {
			return false, c.SkipCI + " in the commit message"
		}
		_, branches := t.Filters["branches"]
		_, branchesIgnore := t.Filters["branches-ignore"]
		_, tags := t.Filters["tags"]
		_, tagsIgnore := t.Filters["tags-ignore"]
		if (tags || tagsIgnore) && !branches && !branchesIgnore {
			return false, "push on tags only"
		}
		if ok, why := refFilter(t, "branches", c.HeadBranch); !ok {
			return false, why
		}
		return pathFilter(t, c.PushFiles)
	}
	return false, ""
}

// refFilter applies the <kind> and <kind>-ignore filters of t to ref.
func refFilter(t trigger, kind, ref string) (bool, string) {
	include, hasInclude := t.Filters[kind]
	exclude, hasExclude := t.Filters[kind+"-ignore"]
	switch {
	case hasInclude && hasExclude:
		return true, "" // Invalid on GitHub, so assume it runs.
	case hasInclude:
		if f, err := compileFilter(include); err == nil && !matches(f, ref) {
			return false, t.Event + " " + kind
		}
	case hasExclude:
		if f, err := compileFilter(exclude); err == nil && matches(f, ref) {
			return false, t.Event + " " + kind + "-ignore"
		}
	}
	return true, ""
}

// pathFilter applies the paths and paths-ignore filters of t to the changed
// files, which are nil when unknown.
func pathFilter(t trigger, files []string) (bool, string) {
	include, hasInclude := t.Filters["paths"]
	exclude, hasExclude := t.Filters["paths-ignore"]
	switch {
	case hasInclude == hasExclude || files == nil:
		return true, "" // No filter, an invalid pair, or unknown files
	case hasInclude:
		f, err := compileFilter(include)
		if err != nil || slices.ContainsFunc(files, func(file string) bool { return matches(f, file) }) {
			return true, ""
		}
		return false, t.Event + " paths"
	default:
		f, err := compileFilter(exclude)
		if err != nil || slices.ContainsFunc(files, func(file string) bool { return !matches(f, file) }) {
			return true, ""
		}
		return false, t.Event + " paths-ignore"
	}
}
