package help

import (
	"bytes"
	"strings"
	"testing"

	"github.com/msuozzo/jj-forge/internal/ui"
	"github.com/spf13/cobra"
)

func newTestRoot(u *ui.UI) *cobra.Command {
	root := &cobra.Command{
		Use:   "myapp",
		Short: "A test application",
	}
	Setup(root, func() *ui.UI { return u })
	return root
}

func TestHelp_BasicCommand(t *testing.T) {
	var buf bytes.Buffer
	u := ui.New(&buf, ui.ColorNever)

	root := newTestRoot(u)
	sub := &cobra.Command{
		Use:   "sub",
		Short: "A subcommand",
		RunE:  func(cmd *cobra.Command, args []string) error { return nil },
	}
	root.AddCommand(sub)
	root.PersistentFlags().String("config", "", "Config file path")
	root.SetOut(&buf)

	root.SetArgs([]string{"--help"})
	root.Execute()

	got := buf.String()

	// Check for Usage: header
	if !strings.Contains(got, "Usage:") {
		t.Errorf("missing Usage: header in:\n%s", got)
	}

	// Check for Commands: header (not "Available Commands:")
	if !strings.Contains(got, "Commands:") {
		t.Errorf("missing Commands: header in:\n%s", got)
	}
	if strings.Contains(got, "Available Commands:") {
		t.Errorf("should not contain 'Available Commands:' in:\n%s", got)
	}

	// Check for Global Options: (not "Global Flags:")
	if !strings.Contains(got, "Global Options:") {
		t.Errorf("missing 'Global Options:' header in:\n%s", got)
	}
	if strings.Contains(got, "Global Flags:") {
		t.Errorf("should not contain 'Global Flags:' in:\n%s", got)
	}

	// Check subcommand is listed
	if !strings.Contains(got, "sub") {
		t.Errorf("missing subcommand 'sub' in:\n%s", got)
	}

	// Check footer
	if !strings.Contains(got, "Use \"myapp [command] --help\"") {
		t.Errorf("missing footer in:\n%s", got)
	}
}

func TestHelp_FlagRendering(t *testing.T) {
	var buf bytes.Buffer
	u := ui.New(&buf, ui.ColorNever)

	root := newTestRoot(u)
	cmd := &cobra.Command{
		Use:   "upload [REVSET]",
		Short: "Upload changes",
		RunE:  func(cmd *cobra.Command, args []string) error { return nil },
	}
	cmd.Flags().StringP("remote", "r", "og", "Remote to push to")
	cmd.Flags().Bool("skip-check", false, "Skip checks")
	root.AddCommand(cmd)
	root.SetOut(&buf)

	root.SetArgs([]string{"upload", "--help"})
	root.Execute()

	got := buf.String()

	// Check Options: header
	if !strings.Contains(got, "Options:") {
		t.Errorf("missing Options: header in:\n%s", got)
	}

	// Check string flag has placeholder
	if !strings.Contains(got, "<REMOTE>") {
		t.Errorf("missing <REMOTE> placeholder in:\n%s", got)
	}

	// Check default value shown
	if !strings.Contains(got, "[default: og]") {
		t.Errorf("missing default value in:\n%s", got)
	}
}

func TestHelp_BoolFlagNoPlaceholder(t *testing.T) {
	var buf bytes.Buffer
	u := ui.New(&buf, ui.ColorNever)

	root := newTestRoot(u)
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Test command",
		RunE:  func(cmd *cobra.Command, args []string) error { return nil },
	}
	cmd.Flags().Bool("verbose", false, "Enable verbose output")
	root.AddCommand(cmd)
	root.SetOut(&buf)

	root.SetArgs([]string{"test", "--help"})
	root.Execute()

	got := buf.String()
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "--verbose") && strings.Contains(line, "<") {
			t.Errorf("bool flag should not have placeholder: %s", line)
		}
	}
}

func TestHelp_WithColor(t *testing.T) {
	var buf bytes.Buffer
	u := ui.New(&buf, ui.ColorAlways)

	root := newTestRoot(u)
	sub := &cobra.Command{
		Use:   "sub",
		Short: "A subcommand",
		RunE:  func(cmd *cobra.Command, args []string) error { return nil },
	}
	root.AddCommand(sub)
	root.SetOut(&buf)

	root.SetArgs([]string{"--help"})
	root.Execute()

	got := buf.String()

	// Check headers have yellow bold ANSI codes
	yellowBold := "\x1b[1;33m"
	if !strings.Contains(got, yellowBold+"Usage:"+"\x1b[0m") {
		t.Errorf("Usage: should be yellow bold in:\n%q", got)
	}
	if !strings.Contains(got, yellowBold+"Commands:"+"\x1b[0m") {
		t.Errorf("Commands: should be yellow bold in:\n%q", got)
	}

	// Check command names have green bold ANSI codes
	greenBold := "\x1b[1;32m"
	if !strings.Contains(got, greenBold+"sub"+"\x1b[0m") {
		t.Errorf("sub command should be green bold in:\n%q", got)
	}
}

func TestHelp_UsageLine(t *testing.T) {
	var buf bytes.Buffer
	u := ui.New(&buf, ui.ColorNever)

	root := newTestRoot(u)
	cmd := &cobra.Command{
		Use:   "clone <url> [path]",
		Short: "Clone a repo",
		RunE:  func(cmd *cobra.Command, args []string) error { return nil },
	}
	root.AddCommand(cmd)
	root.SetOut(&buf)

	root.SetArgs([]string{"clone", "--help"})
	root.Execute()

	got := buf.String()

	// Check usage line includes args
	if !strings.Contains(got, "<url> [path]") {
		t.Errorf("missing args in usage line:\n%s", got)
	}
}

func TestExtractArgs(t *testing.T) {
	tests := []struct {
		use  string
		want string
	}{
		{"upload", ""},
		{"upload [REVSET]", "[REVSET]"},
		{"clone <url> [path]", "<url> [path]"},
		{"submit REVSET", "REVSET"},
	}
	for _, tt := range tests {
		if got := extractArgs(tt.use); got != tt.want {
			t.Errorf("extractArgs(%q) = %q, want %q", tt.use, got, tt.want)
		}
	}
}
