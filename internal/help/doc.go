// Package help provides a custom Cobra help renderer that mimics the layout and
// color scheme of Clap (https://docs.rs/clap), the argument parser used by jj.
//
// # Layout differences from Cobra's default
//
// The renderer replaces several Cobra conventions with Clap equivalents:
//
//   - "Flags:" becomes "Options:"
//   - "Global Flags:" becomes "Global Options:"
//   - "Available Commands:" becomes "Commands:"
//   - Go type names in flag usage (e.g. "string") are replaced with
//     UPPER_CASE placeholders (e.g. <REMOTE>). Bool flags omit the placeholder.
//   - Default values are shown inline: [default: og]
//   - Flags are rendered Clap-style with the description on the same line and
//     aligned with the other flags.
//
// # Color scheme
//
// Colors are applied using labels from [ui.Styles]:
//
//   - help_header (yellow bold): section headers like "Usage:", "Commands:",
//     "Options:", "Global Options:"
//   - help_command (green bold): command names and flag names
//   - help_placeholder (green): value placeholders like <REMOTE>, [command],
//     [OPTIONS]
//
// This matches the colors Clap uses by default, which is what users see in
// jj --help output.
//
// # Usage
//
// Call [Setup] once on the root [cobra.Command] before Execute:
//
//	help.Setup(rootCmd, func() *ui.UI { return myUI })
//
// The function argument is called lazily so that --color flag parsing takes
// effect before the UI is constructed.
package help
