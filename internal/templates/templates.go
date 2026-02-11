package templates

import (
	"context"
	"fmt"

	"github.com/msuozzo/jj-forge/internal/jj"
	toml "github.com/pelletier/go-toml/v2"
)

// Alias holds a single template-alias name and its value.
type Alias struct {
	Name  string
	Value string
}

// aliasOrder defines the deterministic iteration order for template-aliases,
// since go-toml/v2 unmarshals into maps which don't preserve TOML key order.
var aliasOrder = []string{
	"second_line(s)",
	"third_line(s)",
	"fourth_line(s)",
	"forge_change_exists(change_id)",
	"forge_change_uri(change_id)",
	"forge_change_name(change_id)",
	"forge_check_exists(change_id, commit_id)",
	"forge_check_matches(change_id, commit_id, verdict)",
	"format_forge_check(commit)",
	"format_forge_change(commit)",
}

type tomlFile struct {
	TemplateAliases map[string]string `toml:"template-aliases"`
}

// ParseTemplateAliases parses the [template-aliases] section from TOML data
// into an ordered slice of Alias pairs.
func ParseTemplateAliases(tomlData string) ([]Alias, error) {
	var f tomlFile
	if err := toml.Unmarshal([]byte(tomlData), &f); err != nil {
		return nil, fmt.Errorf("failed to parse templates TOML: %w", err)
	}
	if f.TemplateAliases == nil {
		return nil, fmt.Errorf("no [template-aliases] section found")
	}
	var aliases []Alias
	for _, name := range aliasOrder {
		value, ok := f.TemplateAliases[name]
		if !ok {
			return nil, fmt.Errorf("expected template-alias %q not found in TOML", name)
		}
		aliases = append(aliases, Alias{Name: name, Value: value})
	}
	return aliases, nil
}

// Apply sets each template-alias in the jj config using the given scope flag
// (e.g. "--repo" or "--user").
func Apply(ctx context.Context, jjClient jj.Client, scope string, aliases []Alias) error {
	for _, a := range aliases {
		key := fmt.Sprintf("template-aliases.\"%s\"", a.Name)
		if _, err := jjClient.Run(ctx, "config", "set", scope, key, a.Value); err != nil {
			return fmt.Errorf("failed to set %s: %w", a.Name, err)
		}
	}
	return nil
}
