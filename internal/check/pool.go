package check

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/msuozzo/jj-forge/internal/cmd"
)

const (
	stateFileName   = ".jj-forge-check-state"
	defaultPoolSize = 3
)

// WorkDir represents a single working directory in the pool.
type WorkDir struct {
	Path     string // absolute path to the working directory
	CommitID string // git commit currently materialized ("" if fresh)
}

// WorkPool manages a pool of persistent working directories for parallel
// check execution. Each directory materializes a git commit tree using the
// repo's backing git directory.
type WorkPool struct {
	gitDir  string
	dirs    []*WorkDir
	baseDir string // e.g., <jj-root>/.jj/forge/check-pool/
	runner  cmd.Executor
	ch      chan *WorkDir
}

// NewWorkPool creates a pool of working directories under baseDir.
// Existing directories with state files are reused; new directories are
// created as needed up to poolSize.
func NewWorkPool(gitDir, baseDir string, poolSize int, runner cmd.Executor) (*WorkPool, error) {
	if poolSize <= 0 {
		poolSize = defaultPoolSize
	}
	p := &WorkPool{
		gitDir:  gitDir,
		baseDir: baseDir,
		runner:  runner,
		ch:      make(chan *WorkDir, poolSize),
	}
	for i := range poolSize {
		dirPath := filepath.Join(baseDir, fmt.Sprintf("%d", i))
		if err := os.MkdirAll(dirPath, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create pool dir %s: %w", dirPath, err)
		}
		wd := &WorkDir{Path: dirPath}
		// Read existing state
		stateFile := filepath.Join(dirPath, stateFileName)
		if data, err := os.ReadFile(stateFile); err == nil {
			wd.CommitID = strings.TrimSpace(string(data))
		}
		p.dirs = append(p.dirs, wd)
		p.ch <- wd
	}
	return p, nil
}

// Acquire returns an available WorkDir from the pool, blocking until one
// is available or the context is cancelled.
func (p *WorkPool) Acquire(ctx context.Context) (*WorkDir, error) {
	select {
	case wd := <-p.ch:
		return wd, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Release returns a WorkDir to the pool.
func (p *WorkPool) Release(wd *WorkDir) {
	p.ch <- wd
}

// Materialize updates the working directory to match the given git commit.
// If the directory already has a commit materialized, it attempts an
// incremental update via `git diff | git apply`. On failure (or if fresh),
// it falls back to full materialization via `git archive | tar -x`.
func (p *WorkPool) Materialize(ctx context.Context, wd *WorkDir, commitID string) error {
	if wd.CommitID == commitID {
		return nil // already up to date
	}
	if wd.CommitID != "" {
		// Try incremental update
		if err := p.incrementalUpdate(ctx, wd, commitID); err == nil {
			return p.writeState(wd, commitID)
		}
		// Incremental failed, fall through to full materialization
	}
	if err := p.fullMaterialize(ctx, wd, commitID); err != nil {
		return err
	}
	return p.writeState(wd, commitID)
}

// incrementalUpdate applies the diff between the current and target commits.
func (p *WorkPool) incrementalUpdate(ctx context.Context, wd *WorkDir, commitID string) error {
	// Generate the diff
	diff, err := p.runner(ctx, cmd.Opts{},
		"git", "--git-dir", p.gitDir, "diff", wd.CommitID+".."+commitID)
	if err != nil {
		return fmt.Errorf("git diff failed: %w", err)
	}
	if diff == "" {
		return nil // no changes
	}
	// Apply the diff
	_, err = p.runner(ctx, cmd.Opts{
		WorkDir: wd.Path,
		Stdin:   strings.NewReader(diff),
	}, "git", "apply", "--whitespace=nowarn", "-")
	if err != nil {
		return fmt.Errorf("git apply failed: %w", err)
	}
	return nil
}

// fullMaterialize clears the directory and extracts the full tree.
func (p *WorkPool) fullMaterialize(ctx context.Context, wd *WorkDir, commitID string) error {
	// Remove all files except the state file
	entries, err := os.ReadDir(wd.Path)
	if err != nil {
		return fmt.Errorf("failed to read dir %s: %w", wd.Path, err)
	}
	for _, entry := range entries {
		if entry.Name() == stateFileName {
			continue
		}
		if err := os.RemoveAll(filepath.Join(wd.Path, entry.Name())); err != nil {
			return fmt.Errorf("failed to clean %s: %w", entry.Name(), err)
		}
	}
	// Extract the commit tree using git archive | tar -x
	archive, err := p.runner(ctx, cmd.Opts{},
		"git", "--git-dir", p.gitDir, "archive", "--format=tar", commitID)
	if err != nil {
		return fmt.Errorf("git archive failed: %w", err)
	}
	_, err = p.runner(ctx, cmd.Opts{
		WorkDir: wd.Path,
		Stdin:   strings.NewReader(archive),
	}, "tar", "-xf", "-")
	if err != nil {
		return fmt.Errorf("tar extract failed: %w", err)
	}
	return nil
}

// writeState records the current commit ID in the working directory.
func (p *WorkPool) writeState(wd *WorkDir, commitID string) error {
	stateFile := filepath.Join(wd.Path, stateFileName)
	if err := os.WriteFile(stateFile, []byte(commitID+"\n"), 0o644); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}
	wd.CommitID = commitID
	return nil
}
