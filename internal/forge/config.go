package forge

import (
	"context"
	"fmt"
	"strings"

	"github.com/msuozzo/jj-forge/internal/jj"
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

// String returns the pipe-delimited string representation of the record.
func (r ReviewRecord) String() string {
	return strings.Join([]string{r.ChangeID, r.ForgeID, r.URL, string(r.Status)}, recordSep)
}

// ParseReviewRecord parses a pipe-delimited string into a ReviewRecord.
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
	DefaultReviewer string   `toml:"default-reviewer,omitempty"`
	Reviews         []string `toml:"reviews,omitempty"`
	CheckCommand    string   `toml:"check-command,omitempty"`
	Checks          []string `toml:"checks,omitempty"`
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
	client jj.Client
}

// NewConfigManager creates a new ConfigManager.
func NewConfigManager(client jj.Client) *ConfigManager {
	return &ConfigManager{client: client}
}

// getForgeConfig retrieves the entire forge config section.
func (m *ConfigManager) getForgeConfig() (*ForgeConfig, error) {
	output, err := m.client.Run(context.Background(), "config", "list", "--repo", "forge")
	if err != nil {
		return nil, err
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return &ForgeConfig{}, nil
	}
	var wrapper struct {
		ForgeConfig `toml:"forge,omitempty"`
	}
	if err := toml.Unmarshal([]byte(output), &wrapper); err != nil {
		return nil, fmt.Errorf("failed to parse forge config: %w", err)
	}
	return &wrapper.ForgeConfig, nil
}

// GetReviewRecords retrieves all forge review records from the config.
func (m *ConfigManager) GetReviewRecords() ([]ReviewRecord, error) {
	cfg, err := m.getForgeConfig()
	if err != nil {
		return nil, err
	}
	var records []ReviewRecord
	for _, s := range cfg.Reviews {
		rec, err := ParseReviewRecord(s)
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	return records, nil
}

// AddReviewRecord adds or updates a forge review record in the config.
func (m *ConfigManager) AddReviewRecord(rec ReviewRecord) error {
	records, err := m.GetReviewRecords()
	if err != nil {
		return err
	}
	found := false
	for i, r := range records {
		if r.ChangeID == rec.ChangeID {
			records[i] = rec
			found = true
			break
		}
	}
	if !found {
		records = append(records, rec)
	}
	return m.SaveRecords(records)
}

// RemoveReviewRecord removes a forge review record from the config by ChangeID.
func (m *ConfigManager) RemoveReviewRecord(changeID string) error {
	records, err := m.GetReviewRecords()
	if err != nil {
		return err
	}
	var nextRecords []ReviewRecord
	for _, r := range records {
		if r.ChangeID != changeID {
			nextRecords = append(nextRecords, r)
		}
	}
	if len(nextRecords) == len(records) {
		return nil // Not found, nothing to do
	}
	return m.SaveRecords(nextRecords)
}

// SaveRecords saves the list of review records to the config.
func (m *ConfigManager) SaveRecords(records []ReviewRecord) error {
	// Convert records to strings
	var reviewsRaw []string
	for _, r := range records {
		reviewsRaw = append(reviewsRaw, r.String())
	}
	// Marshal as TOML array
	var wrapper struct {
		Reviews []string `toml:"reviews"`
	}
	wrapper.Reviews = reviewsRaw
	tomlBytes, err := toml.Marshal(wrapper)
	if err != nil {
		return err
	}
	// Extract just the array value part from "reviews = [...]"
	tomlStr := string(tomlBytes)
	// Find the array part
	startIdx := strings.Index(tomlStr, "[")
	if startIdx == -1 {
		return fmt.Errorf("unexpected TOML format")
	}
	arrayValue := strings.TrimSpace(tomlStr[startIdx:])
	// Use jj config set to write the value
	_, err = m.client.Run(context.Background(), "config", "set", "--repo", "forge.reviews", arrayValue)
	return err
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

// GetCheckCommand retrieves the configured check command.
// Returns an empty string if no check command is configured.
func (m *ConfigManager) GetCheckCommand() (string, error) {
	cfg, err := m.getForgeConfig()
	if err != nil {
		return "", err
	}
	return cfg.CheckCommand, nil
}

// GetCheckVerdicts retrieves all stored check verdicts from the config.
func (m *ConfigManager) GetCheckVerdicts() ([]CheckVerdict, error) {
	cfg, err := m.getForgeConfig()
	if err != nil {
		return nil, err
	}
	var verdicts []CheckVerdict
	for _, s := range cfg.Checks {
		v, err := ParseCheckVerdict(s)
		if err != nil {
			return nil, err
		}
		verdicts = append(verdicts, v)
	}
	return verdicts, nil
}

// SetCheckVerdict adds or updates a check verdict in the config (upsert by ChangeID).
func (m *ConfigManager) SetCheckVerdict(v CheckVerdict) error {
	verdicts, err := m.GetCheckVerdicts()
	if err != nil {
		return err
	}
	found := false
	for i, existing := range verdicts {
		if existing.ChangeID == v.ChangeID {
			verdicts[i] = v
			found = true
			break
		}
	}
	if !found {
		verdicts = append(verdicts, v)
	}
	return m.saveVerdicts(verdicts)
}

// SetCheckVerdicts adds or updates multiple check verdicts in one batch (single read + single write).
func (m *ConfigManager) SetCheckVerdicts(updates []CheckVerdict) error {
	verdicts, err := m.GetCheckVerdicts()
	if err != nil {
		return err
	}
	for _, u := range updates {
		found := false
		for i, existing := range verdicts {
			if existing.ChangeID == u.ChangeID {
				verdicts[i] = u
				found = true
				break
			}
		}
		if !found {
			verdicts = append(verdicts, u)
		}
	}
	return m.saveVerdicts(verdicts)
}

// RemoveCheckVerdicts removes check verdicts for the given change IDs.
// It is a no-op if none of the change IDs are found.
func (m *ConfigManager) RemoveCheckVerdicts(changeIDs []string) error {
	verdicts, err := m.GetCheckVerdicts()
	if err != nil {
		return err
	}
	removeSet := make(map[string]bool, len(changeIDs))
	for _, id := range changeIDs {
		removeSet[id] = true
	}
	var nextVerdicts []CheckVerdict
	for _, v := range verdicts {
		if !removeSet[v.ChangeID] {
			nextVerdicts = append(nextVerdicts, v)
		}
	}
	if len(nextVerdicts) == len(verdicts) {
		return nil // Nothing to remove
	}
	return m.saveVerdicts(nextVerdicts)
}

// GetCheckVerdictByChangeID finds a check verdict by change ID.
// Returns nil if no verdict is found.
func (m *ConfigManager) GetCheckVerdictByChangeID(changeID string) (*CheckVerdict, error) {
	verdicts, err := m.GetCheckVerdicts()
	if err != nil {
		return nil, err
	}
	for _, v := range verdicts {
		if v.ChangeID == changeID {
			return &v, nil
		}
	}
	return nil, nil
}

func (m *ConfigManager) saveVerdicts(verdicts []CheckVerdict) error {
	var checksRaw []string
	for _, v := range verdicts {
		checksRaw = append(checksRaw, v.String())
	}
	var wrapper struct {
		Checks []string `toml:"checks"`
	}
	wrapper.Checks = checksRaw
	tomlBytes, err := toml.Marshal(wrapper)
	if err != nil {
		return err
	}
	tomlStr := string(tomlBytes)
	startIdx := strings.Index(tomlStr, "[")
	if startIdx == -1 {
		return fmt.Errorf("unexpected TOML format")
	}
	arrayValue := strings.TrimSpace(tomlStr[startIdx:])
	_, err = m.client.Run(context.Background(), "config", "set", "--repo", "forge.checks", arrayValue)
	return err
}
