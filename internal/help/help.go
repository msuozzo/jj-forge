package help

import (
	"fmt"
	"io"
	"strings"

	"github.com/msuozzo/jj-forge/internal/ui"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Setup configures the root command to use a custom Clap-style help and usage
// renderer that matches jj's output format. The getUI function is called each
// time help is rendered, allowing the UI to be resolved lazily after flag parsing.
func Setup(rootCmd *cobra.Command, getUI func() *ui.UI) {
	rootCmd.SetUsageFunc(func(cmd *cobra.Command) error {
		return renderUsage(cmd, getUI())
	})
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		renderHelp(cmd, getUI())
	})
}

func renderHelp(cmd *cobra.Command, u *ui.UI) {
	out := cmd.OutOrStdout()

	// Print description
	desc := cmd.Long
	if desc == "" {
		desc = cmd.Short
	}
	if desc != "" {
		fmt.Fprintf(out, "%s\n\n", desc)
	}

	renderUsage(cmd, u)
}

func renderUsage(cmd *cobra.Command, u *ui.UI) error {
	out := cmd.OutOrStdout()

	// Usage line
	fmt.Fprintf(out, "%s %s", u.Styled("help_header", "Usage:"), " ")
	usageLine := buildUsageLine(cmd, u)
	fmt.Fprintln(out, usageLine)

	// Commands section
	if cmd.HasAvailableSubCommands() {
		fmt.Fprintln(out)
		fmt.Fprintln(out, u.Styled("help_header", "Commands:"))
		renderCommands(out, cmd, u)
	}

	// Options section (non-persistent local flags)
	localFlags := cmd.LocalNonPersistentFlags()
	if localFlags.HasFlags() {
		fmt.Fprintln(out)
		fmt.Fprintln(out, u.Styled("help_header", "Options:"))
		renderFlags(out, localFlags, u)
	}

	// Global Options section: inherited flags + persistent flags on this command
	// (persistent flags on root have no parent to inherit from, but are still global)
	globalFlags := cmd.InheritedFlags()
	cmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		if globalFlags.Lookup(f.Name) == nil {
			globalFlags.AddFlag(f)
		}
	})
	if globalFlags.HasFlags() {
		fmt.Fprintln(out)
		fmt.Fprintln(out, u.Styled("help_header", "Global Options:"))
		renderFlags(out, globalFlags, u)
	}

	// Footer
	if cmd.HasAvailableSubCommands() {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "Use \"%s [command] --help\" for more information about a command.\n", cmd.CommandPath())
	}

	return nil
}

func buildUsageLine(cmd *cobra.Command, u *ui.UI) string {
	var parts []string
	parts = append(parts, u.Styled("help_command", cmd.CommandPath()))

	if cmd.HasAvailableSubCommands() {
		parts = append(parts, u.Styled("help_placeholder", "[command]"))
	}

	if cmd.HasAvailableFlags() {
		parts = append(parts, u.Styled("help_placeholder", "[OPTIONS]"))
	}

	// Add args usage if defined
	if cmd.Use != "" {
		// Extract args portion from Use string (everything after the command name)
		useArgs := extractArgs(cmd.Use)
		if useArgs != "" {
			parts = append(parts, u.Styled("help_placeholder", useArgs))
		}
	}

	return strings.Join(parts, " ")
}

// extractArgs returns the argument portion of a cobra Use string.
// e.g. "upload [REVSET]" -> "[REVSET]", "clone <url> [path]" -> "<url> [path]"
func extractArgs(use string) string {
	idx := strings.IndexByte(use, ' ')
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(use[idx+1:])
}

func renderCommands(out io.Writer, cmd *cobra.Command, u *ui.UI) {
	// Find max command name length for alignment
	maxLen := 0
	for _, sub := range cmd.Commands() {
		if sub.IsAvailableCommand() || sub.Name() == "help" {
			if len(sub.Name()) > maxLen {
				maxLen = len(sub.Name())
			}
		}
	}

	for _, sub := range cmd.Commands() {
		if !sub.IsAvailableCommand() && sub.Name() != "help" {
			continue
		}
		name := u.Styled("help_command", sub.Name())
		padding := strings.Repeat(" ", maxLen-len(sub.Name())+4)
		fmt.Fprintf(out, "  %s%s%s\n", name, padding, sub.Short)
	}
}

// flagNameWidth returns the display width of the flag's name portion
// (e.g. "-r, --remote <REMOTE>" has width 22 with the leading 2-space indent).
func flagNameWidth(f *pflag.Flag) int {
	// "  " prefix
	w := 2
	if f.Shorthand != "" {
		w += len("-" + f.Shorthand + ",")
	} else {
		w += 3 // "   " (align with shorthand column)
	}
	w++ // space before --name
	w += len("--" + f.Name)
	if f.Value.Type() != "bool" {
		w += len(" <" + strings.ToUpper(f.Name) + ">")
	}
	return w
}

func renderFlags(out io.Writer, flags *pflag.FlagSet, u *ui.UI) {
	// Compute column width for alignment.
	maxWidth := 0
	flags.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		if w := flagNameWidth(f); w > maxWidth {
			maxWidth = w
		}
	})

	flags.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}

		// Build flag name portion: -s, --name <VALUE>
		var nameParts []string
		if f.Shorthand != "" {
			nameParts = append(nameParts, u.Styled("help_command", "-"+f.Shorthand)+",")
		} else {
			nameParts = append(nameParts, "   ")
		}

		longFlag := u.Styled("help_command", "--"+f.Name)
		nameParts = append(nameParts, longFlag)

		// Add value placeholder for non-bool flags
		if f.Value.Type() != "bool" {
			placeholder := "<" + strings.ToUpper(f.Name) + ">"
			nameParts = append(nameParts, u.Styled("help_placeholder", placeholder))
		}

		nameStr := strings.Join(nameParts, " ")
		w := flagNameWidth(f)

		desc := f.Usage
		if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "[]" {
			desc += fmt.Sprintf(" [default: %s]", f.DefValue)
		}

		if desc != "" {
			padding := strings.Repeat(" ", maxWidth-w+2)
			fmt.Fprintf(out, "  %s%s%s\n", nameStr, padding, desc)
		} else {
			fmt.Fprintf(out, "  %s\n", nameStr)
		}
	})
}
