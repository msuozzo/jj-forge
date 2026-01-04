// Package jjtest provides test utilities for testing code that interacts with jj.
package jjtest

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/msuozzo/jj-forge/internal/jj"
)

// Commit defines the properties for a commit in the fake repo.
type Commit struct {
	ID              string
	Parents         []string
	Description     string
	IsMutable       bool
	IsConflicted    bool
	IsEmpty         bool
	RemoteBookmarks []string // e.g., ["og/push-abc123"]
}

// FakeRepo holds the state of a fake jj repository.
type FakeRepo struct {
	Commits map[string]*Commit
	Root    string
}

// NewFakeRepo creates a repo with an immutable root commit.
func NewFakeRepo() *FakeRepo {
	root := &Commit{
		ID:        "root",
		IsMutable: false,
		Parents:   []string{},
	}
	return &FakeRepo{
		Commits: map[string]*Commit{"root": root},
		Root:    "/fake/repo",
	}
}

// AddCommits adds commits to the fake repo.
func (r *FakeRepo) AddCommits(commits ...Commit) {
	for _, c := range commits {
		commit := c // copy
		if commit.Parents == nil {
			commit.Parents = []string{}
		}
		r.Commits[commit.ID] = &commit
	}
}

// Call represents an expected call to the jj executor.
type Call struct {
	// Args are the expected arguments (excluding "jj" and "-R repo").
	Args []string
	// SideEffect optionally modifies repo state before generating output.
	SideEffect func(*FakeRepo)
	// Output generates stdout based on repo state. If nil, returns "".
	Output func(*FakeRepo) string
	// Err is the error to return.
	Err error
}

// Scenario implements an executor that validates calls against expected sequence.
type Scenario struct {
	T     *testing.T
	Repo  *FakeRepo
	Calls []Call
	idx   int
}

// NewScenario creates a scenario for testing.
func NewScenario(t *testing.T, repo *FakeRepo, calls ...Call) *Scenario {
	return &Scenario{
		T:     t,
		Repo:  repo,
		Calls: calls,
	}
}

// Executor returns an executor function for use with jj.NewClientWithExecutor.
func (s *Scenario) Executor() jj.Executor {
	return func(ctx context.Context, args ...string) (string, error) {
		s.T.Helper()

		// Strip -R flag if present
		cmdArgs := args
		if len(args) > 1 && args[0] == "-R" {
			cmdArgs = args[2:]
		}

		if s.idx >= len(s.Calls) {
			s.T.Fatalf("unexpected call: jj %v", cmdArgs)
		}

		call := s.Calls[s.idx]
		s.idx++

		if !slices.Equal(call.Args, cmdArgs) {
			s.T.Fatalf("arg mismatch at call %d:\nwant: %v\ngot:  %v", s.idx, call.Args, cmdArgs)
		}

		if call.SideEffect != nil {
			call.SideEffect(s.Repo)
		}

		var stdout string
		if call.Output != nil {
			stdout = call.Output(s.Repo)
		}

		return stdout, call.Err
	}
}

// Verify checks that all expected calls were made.
func (s *Scenario) Verify() {
	s.T.Helper()
	if s.idx < len(s.Calls) {
		s.T.Fatalf("expected call not made: %v", s.Calls[s.idx].Args)
	}
}

// Client returns a jj.Client configured with this scenario's executor.
func (s *Scenario) Client() jj.Client {
	return jj.NewClientWithExecutor(s.Repo.Root, s.Executor())
}

// LogOutput generates output in the format expected by jj.Client.Revs().
func LogOutput(ids ...string) func(*FakeRepo) string {
	return func(r *FakeRepo) string {
		var lines []string
		for _, id := range ids {
			c, ok := r.Commits[id]
			if !ok {
				panic(fmt.Sprintf("test setup error: commit %s missing from fake repo", id))
			}
			descJSON, _ := json.Marshal(c.Description)
			// Format: ID conflict divergent mutable empty parents remote_bookmarks description
			line := fmt.Sprintf("%s %v false %v %v %s %s %s",
				c.ID,
				c.IsConflicted,
				c.IsMutable,
				c.IsEmpty,
				strings.Join(c.Parents, ","),
				strings.Join(c.RemoteBookmarks, ","),
				string(descJSON),
			)
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n")
	}
}

// RootOutput returns the repo root path.
func RootOutput() func(*FakeRepo) string {
	return func(r *FakeRepo) string {
		return r.Root + "\n"
	}
}

// EmptyOutput returns empty string.
func EmptyOutput() func(*FakeRepo) string {
	return func(r *FakeRepo) string {
		return ""
	}
}

// UpdateDescription is a side effect that updates a commit's description.
func UpdateDescription(id, newDesc string) func(*FakeRepo) {
	return func(r *FakeRepo) {
		if c, ok := r.Commits[id]; ok {
			c.Description = newDesc
		}
	}
}
