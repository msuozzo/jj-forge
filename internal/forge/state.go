package forge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/msuozzo/jj-forge/internal/filelock"
	"github.com/pelletier/go-toml/v2"
)

// State is the review records and check verdicts jj-forge keeps in the repo
// config, where jj log templates read them.
type State struct {
	Reviews []ReviewRecord
	Checks  []CheckVerdict
}

// SetReview adds rec, replacing any record for the same change.
func (s *State) SetReview(rec ReviewRecord) {
	if i := slices.IndexFunc(s.Reviews, func(r ReviewRecord) bool { return r.ChangeID == rec.ChangeID }); i != -1 {
		s.Reviews[i] = rec
		return
	}
	s.Reviews = append(s.Reviews, rec)
}

// RemoveReviews removes the records for the given changes.
func (s *State) RemoveReviews(changeIDs ...string) {
	s.Reviews = slices.DeleteFunc(s.Reviews, func(r ReviewRecord) bool { return slices.Contains(changeIDs, r.ChangeID) })
}

// SetCheck adds v, replacing any verdict for the same change.
func (s *State) SetCheck(v CheckVerdict) {
	if i := slices.IndexFunc(s.Checks, func(c CheckVerdict) bool { return c.ChangeID == v.ChangeID }); i != -1 {
		s.Checks[i] = v
		return
	}
	s.Checks = append(s.Checks, v)
}

// Check returns the verdict for a change, or nil if there is none.
func (s *State) Check(changeID string) *CheckVerdict {
	if i := slices.IndexFunc(s.Checks, func(c CheckVerdict) bool { return c.ChangeID == changeID }); i != -1 {
		return &s.Checks[i]
	}
	return nil
}

// RemoveChecks removes the verdicts for the given changes.
func (s *State) RemoveChecks(changeIDs ...string) {
	s.Checks = slices.DeleteFunc(s.Checks, func(c CheckVerdict) bool { return slices.Contains(changeIDs, c.ChangeID) })
}

func parseState(cfg *ForgeConfig) (*State, error) {
	s := &State{}
	for _, raw := range cfg.Reviews {
		rec, err := ParseReviewRecord(raw)
		if err != nil {
			return nil, err
		}
		s.Reviews = append(s.Reviews, rec)
	}
	for _, raw := range cfg.Checks {
		v, err := ParseCheckVerdict(raw)
		if err != nil {
			return nil, err
		}
		s.Checks = append(s.Checks, v)
	}
	return s, nil
}

func (s *State) rawReviews() []string {
	var out []string
	for _, r := range s.Reviews {
		out = append(out, r.String())
	}
	return out
}

func (s *State) rawChecks() []string {
	var out []string
	for _, v := range s.Checks {
		out = append(out, v.String())
	}
	return out
}

// configLockTimeout bounds the wait for another jj-forge process's config
// update, which normally takes well under a second.
const configLockTimeout = 30 * time.Second

// Update applies fn to a fresh read of the state and writes back the parts fn
// changed. Concurrent jj-forge processes each update in turn, so none loses
// another's changes, and readers like jj log never see a partly written file.
// fn must not call other ConfigManager methods that write.
func (m *ConfigManager) Update(fn func(*State) error) error {
	ctx := context.Background()
	path, err := m.repoConfigPath(ctx)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating repo config directory: %w", err)
	}
	lockCtx, cancel := context.WithTimeout(ctx, configLockTimeout)
	defer cancel()
	lock, err := filelock.Acquire(lockCtx, filepath.Join(filepath.Dir(path), "jj-forge.lock"), 10*time.Millisecond, nil)
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("timed out waiting for another jj-forge process to update %s", path)
	}
	if err != nil {
		return err
	}
	defer lock.Unlock()

	cfg, err := m.getForgeConfig()
	if err != nil {
		return err
	}
	state, err := parseState(cfg)
	if err != nil {
		return err
	}
	if err := fn(state); err != nil {
		return err
	}
	var sets [][2]string
	for _, field := range []struct {
		key           string
		before, after []string
	}{
		{"forge.reviews", cfg.Reviews, state.rawReviews()},
		{"forge.checks", cfg.Checks, state.rawChecks()},
	} {
		if slices.Equal(field.before, field.after) {
			continue
		}
		value, err := tomlStringArray(field.after)
		if err != nil {
			return err
		}
		sets = append(sets, [2]string{field.key, value})
	}
	if len(sets) == 0 {
		return nil
	}
	return m.replaceRepoConfig(ctx, path, sets)
}

// repoConfigPath returns the repo config file, following a symlink so that
// replacing the file keeps the link.
func (m *ConfigManager) repoConfigPath(ctx context.Context) (string, error) {
	if m.configPath != "" {
		return m.configPath, nil
	}
	result, err := m.client.Run(ctx, "config", "path", "--repo")
	if err != nil {
		return "", fmt.Errorf("finding the repo config file: %w", err)
	}
	path := strings.TrimSpace(result.Stdout)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	m.configPath = path
	return path, nil
}

// replaceRepoConfig has jj apply each key and value to a copy of the repo
// config file, then renames the copy over the original. jj config set
// --repo rewrites the file in place, so a reader could see it half written
// and two writers could leave it corrupt.
func (m *ConfigManager) replaceRepoConfig(ctx context.Context, path string, sets [][2]string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading repo config: %w", err)
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	// The name is fixed because the config lock is held. A copy left by a
	// crash is overwritten here.
	tmp := path + ".jj-forge-tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return fmt.Errorf("copying repo config: %w", err)
	}
	defer os.Remove(tmp)
	if err := os.Chmod(tmp, mode); err != nil {
		return fmt.Errorf("copying repo config: %w", err)
	}
	for _, kv := range sets {
		// --file only accepts files jj loads, hence --config-file.
		if _, err := m.client.Run(ctx, "--config-file", tmp, "config", "set", "--file", tmp, kv[0], kv[1]); err != nil {
			return err
		}
	}
	if f, err := os.Open(tmp); err == nil {
		f.Sync()
		f.Close()
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replacing repo config: %w", err)
	}
	return nil
}

// tomlStringArray formats values as a TOML array literal.
func tomlStringArray(values []string) (string, error) {
	if len(values) == 0 {
		return "[]", nil
	}
	var wrapper struct {
		V []string `toml:"v"`
	}
	wrapper.V = values
	b, err := toml.Marshal(wrapper)
	if err != nil {
		return "", err
	}
	s := string(b)
	i := strings.Index(s, "[")
	if i == -1 {
		return "", fmt.Errorf("unexpected TOML format")
	}
	return strings.TrimSpace(s[i:]), nil
}
