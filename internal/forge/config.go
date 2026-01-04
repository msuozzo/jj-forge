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
	Status   string
}

// String returns the pipe-delimited string representation of the record.
func (r ReviewRecord) String() string {
	return strings.Join([]string{r.ChangeID, r.ForgeID, r.URL, r.Status}, recordSep)
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
		Status:   parts[3],
	}, nil
}

// ForgeConfig represents the [forge] section of the jj config.
type ForgeConfig struct {
	DefaultReviewer string   `toml:"default-reviewer,omitempty"`
	Reviews         []string `toml:"reviews,omitempty"`
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
	return m.saveRecords(records)
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
	return m.saveRecords(nextRecords)
}

func (m *ConfigManager) saveRecords(records []ReviewRecord) error {
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

// GetDefaultReviewer retrieves the default reviewer from the config.
// Returns an empty string if no default reviewer is configured.
func (m *ConfigManager) GetDefaultReviewer() (string, error) {
	cfg, err := m.getForgeConfig()
	if err != nil {
		return "", err
	}
	return cfg.DefaultReviewer, nil
}
