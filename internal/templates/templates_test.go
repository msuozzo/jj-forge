package templates

import (
	"testing"

	jjforge "github.com/msuozzo/jj-forge"
)

func TestParseTemplateAliases(t *testing.T) {
	aliases, err := ParseTemplateAliases(jjforge.TemplatesTOML)
	if err != nil {
		t.Fatalf("ParseTemplateAliases() error: %v", err)
	}
	if got := len(aliases); got != 10 {
		t.Errorf("ParseTemplateAliases() returned %d aliases, want 10", got)
	}

	expectedNames := []string{
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
	for i, expected := range expectedNames {
		if aliases[i].Name != expected {
			t.Errorf("alias[%d].Name = %q, want %q", i, aliases[i].Name, expected)
		}
	}

	// Verify values are non-empty
	for _, a := range aliases {
		if a.Value == "" {
			t.Errorf("alias %q has empty value", a.Name)
		}
	}
}
