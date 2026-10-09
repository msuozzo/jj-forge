package forge

import (
	"cmp"
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/msuozzo/jj-forge/internal/jj"
	"github.com/msuozzo/jj-forge/internal/ui"
	"github.com/pelletier/go-toml/v2"
)

// recordSep is the character separating each entry in the ReviewRecord
// NOTE: The current jj templating logic does not make string manipulation
// easy but has some workable APIs for line-based consumption.
// Using a newline here makes templating much easier.
const recordSep = "\n"

// ReviewRecord represents a mapping between a jj change and a forge review (PR).
type ReviewRecord struct {
	ChangeID string
	ForgeID  string
	URL      string
	Status   ReviewState
}

// String returns the newline-delimited string representation of the record.
func (r ReviewRecord) String() string {
	return strings.Join([]string{r.ChangeID, r.ForgeID, r.URL, string(r.Status)}, recordSep)
}

// ParseReviewRecord parses a newline-delimited string into a ReviewRecord.
func ParseReviewRecord(s string) (ReviewRecord, error) {
	parts := strings.Split(s, recordSep)
	if len(parts) != 4 {
		return ReviewRecord{}, fmt.Errorf("invalid review record format: %q", s)
	}
	return ReviewRecord{
		ChangeID: parts[0],
		ForgeID:  parts[1],
		URL:      parts[2],
		Status:   ReviewState(parts[3]),
	}, nil
}

// ForgeConfig represents the [forge] section of the jj config.
type ForgeConfig struct {
	DefaultReviewer       string            `toml:"default-reviewer,omitempty"`
	DefaultRemote         string            `toml:"default-remote,omitempty"`
	DefaultUpstreamRemote string            `toml:"default-upstream-remote,omitempty"`
	Reviews               []string          `toml:"reviews,omitempty"`
	CheckCommand          string            `toml:"check-command,omitempty"`
	Checks                []string          `toml:"checks,omitempty"`
	Tools                 map[string]string `toml:"tools,omitempty"`
	Hosts                 map[string]string `toml:"hosts,omitempty"`
}

// Check verdict values.
const (
	CheckVerdictPass    = "pass"
	CheckVerdictFail    = "fail"
	CheckVerdictRunning = "running"
)

// CheckVerdict represents the result of running a check command on a change.
type CheckVerdict struct {
	ChangeID string // jj change ID
	Verdict  string // CheckVerdictPass or CheckVerdictFail
	CommitID string // commit ID at time of check (detects staleness)
}

// checkVerdictSep is the separator for serialized check verdicts.
const checkVerdictSep = "\n"

// String returns the serialized string representation of the verdict.
func (v CheckVerdict) String() string {
	return strings.Join([]string{v.ChangeID, v.Verdict, v.CommitID}, checkVerdictSep)
}

// ParseCheckVerdict parses a serialized string into a CheckVerdict.
func ParseCheckVerdict(s string) (CheckVerdict, error) {
	parts := strings.Split(s, checkVerdictSep)
	if len(parts) != 3 {
		return CheckVerdict{}, fmt.Errorf("invalid check verdict format: %q", s)
	}
	return CheckVerdict{
		ChangeID: parts[0],
		Verdict:  parts[1],
		CommitID: parts[2],
	}, nil
}

// ConfigManager handles reading and writing jj-forge configuration.
type ConfigManager struct {
	client     jj.Client
	configPath string // repo config file, looked up on first update
}

// NewConfigManager creates a new ConfigManager.
func NewConfigManager(client jj.Client) *ConfigManager {
	return &ConfigManager{client: client}
}

// getForgeConfig reads the forge config section. It is read fresh every
// time, since other jj-forge processes may have changed it.
func (m *ConfigManager) getForgeConfig() (*ForgeConfig, error) {
	result, err := m.client.Run(context.Background(), "config", "list", "forge")
	if err != nil {
		return nil, err
	}
	output := strings.TrimSpace(result.Stdout)
	if output == "" {
		return &ForgeConfig{}, nil
	}
	var wrapper struct {
		ForgeConfig `toml:"forge,omitempty"`
	}
	if err := toml.Unmarshal([]byte(output), &wrapper); err != nil {
		if key := nonStringKey(output); key != "" {
			return nil, &ui.UserError{
				Msg:  fmt.Sprintf("forge.%s must be a string", key),
				Hint: fmt.Sprintf("jj config set reads a value like true or 1 as a TOML boolean or number. Quote it to store a string: jj config set --repo forge.%s '\"true\"'", key),
			}
		}
		return nil, fmt.Errorf("failed to parse forge config: %w", err)
	}
	return &wrapper.ForgeConfig, nil
}

// nonStringKey returns the first string setting in the forge config that holds
// some other type, or "" if there is none. go-toml's error doesn't name it.
func nonStringKey(output string) string {
	var raw struct {
		Forge map[string]any `toml:"forge"`
	}
	if toml.Unmarshal([]byte(output), &raw) != nil {
		return ""
	}
	fields := reflect.TypeFor[ForgeConfig]()
	for i := range fields.NumField() {
		f := fields.Field(i)
		if f.Type.Kind() != reflect.String {
			continue
		}
		key, _, _ := strings.Cut(f.Tag.Get("toml"), ",")
		if v, ok := raw.Forge[key]; ok {
			if _, isString := v.(string); !isString {
				return key
			}
		}
	}
	return ""
}

// readState reads the review records and check verdicts.
func (m *ConfigManager) readState() (*State, error) {
	cfg, err := m.getForgeConfig()
	if err != nil {
		return nil, err
	}
	return parseState(cfg)
}

// GetReviewRecords retrieves all forge review records from the config.
func (m *ConfigManager) GetReviewRecords() ([]ReviewRecord, error) {
	state, err := m.readState()
	if err != nil {
		return nil, err
	}
	return state.Reviews, nil
}

// AddReviewRecord adds or updates a forge review record in the config.
func (m *ConfigManager) AddReviewRecord(rec ReviewRecord) error {
	return m.Update(func(s *State) error {
		s.SetReview(rec)
		return nil
	})
}

// RemoveReviewRecord removes a forge review record from the config by ChangeID.
func (m *ConfigManager) RemoveReviewRecord(changeID string) error {
	return m.Update(func(s *State) error {
		s.RemoveReviews(changeID)
		return nil
	})
}

// GetReviewByChangeID finds a review record by change ID.
// Returns nil if no record is found.
func (m *ConfigManager) GetReviewByChangeID(changeID string) (*ReviewRecord, error) {
	records, err := m.GetReviewRecords()
	if err != nil {
		return nil, err
	}
	for _, r := range records {
		if r.ChangeID == changeID {
			return &r, nil
		}
	}
	return nil, nil
}

// GetDefaultReviewer retrieves the default reviewer from the config.
// Returns an empty string if no default reviewer is configured.
func (m *ConfigManager) GetDefaultReviewer() (string, error) {
	cfg, err := m.getForgeConfig()
	if err != nil {
		return "", err
	}
	return cfg.DefaultReviewer, nil
}

// GetDefaultRemote returns forge.default-remote, or DefaultRemote
// when it is unset.
func (m *ConfigManager) GetDefaultRemote() (string, error) {
	cfg, err := m.getForgeConfig()
	if err != nil {
		return "", err
	}
	return cmp.Or(cfg.DefaultRemote, DefaultRemote), nil
}

// GetDefaultUpstreamRemote returns forge.default-upstream-remote, or
// DefaultUpstreamRemote when it is unset.
func (m *ConfigManager) GetDefaultUpstreamRemote() (string, error) {
	cfg, err := m.getForgeConfig()
	if err != nil {
		return "", err
	}
	return cmp.Or(cfg.DefaultUpstreamRemote, DefaultUpstreamRemote), nil
}

// GetHosts retrieves the configured host overrides map from the config.
// Returns nil if no host overrides are configured.
func (m *ConfigManager) GetHosts() (map[string]string, error) {
	cfg, err := m.getForgeConfig()
	if err != nil {
		return nil, err
	}
	return cfg.Hosts, nil
}

// GetCheckCommand retrieves the configured check command.
// Returns an empty string if no check command is configured.
func (m *ConfigManager) GetCheckCommand() (string, error) {
	cfg, err := m.getForgeConfig()
	if err != nil {
		return "", err
	}
	return cfg.CheckCommand, nil
}

// GetToolCommand retrieves the configured command for a specific tool (e.g. "gh", "gcloud").
// Returns the tool's name itself if no command is configured, serving as the default command binary name.
func (m *ConfigManager) GetToolCommand(name string) (string, error) {
	cfg, err := m.getForgeConfig()
	if err != nil {
		return "", err
	}
	if cfg.Tools != nil {
		if cmd, ok := cfg.Tools[name]; ok && cmd != "" {
			return cmd, nil
		}
	}
	return name, nil
}

// GetCheckVerdicts retrieves all stored check verdicts from the config.
func (m *ConfigManager) GetCheckVerdicts() ([]CheckVerdict, error) {
	state, err := m.readState()
	if err != nil {
		return nil, err
	}
	return state.Checks, nil
}

// SetCheckVerdict adds or updates a check verdict in the config (upsert by ChangeID).
func (m *ConfigManager) SetCheckVerdict(v CheckVerdict) error {
	return m.SetCheckVerdicts([]CheckVerdict{v})
}

// SetCheckVerdicts adds or updates multiple check verdicts in one update.
func (m *ConfigManager) SetCheckVerdicts(updates []CheckVerdict) error {
	return m.Update(func(s *State) error {
		for _, v := range updates {
			s.SetCheck(v)
		}
		return nil
	})
}

// RemoveCheckVerdicts removes check verdicts for the given change IDs.
// It is a no-op if none of the change IDs are found.
func (m *ConfigManager) RemoveCheckVerdicts(changeIDs []string) error {
	return m.Update(func(s *State) error {
		s.RemoveChecks(changeIDs...)
		return nil
	})
}

// GetCheckVerdictByChangeID finds a check verdict by change ID.
// Returns nil if no verdict is found.
func (m *ConfigManager) GetCheckVerdictByChangeID(changeID string) (*CheckVerdict, error) {
	state, err := m.readState()
	if err != nil {
		return nil, err
	}
	return state.Check(changeID), nil
}

// Get returns the value of a jj config key, and "" if it is unset.
func (m *ConfigManager) Get(key string) (string, error) {
	result, err := m.client.Run(context.Background(), "config", "get", key)
	if err != nil {
		if strings.Contains(err.Error(), "Value not found") {
			return "", nil
		}
		return "", fmt.Errorf("reading jj config %s: %w", key, err)
	}
	return strings.TrimSpace(result.Stdout), nil
}
