// Package ui provides terminal styling for jj-forge, matching the color scheme
// and error formatting of jj (https://jj-vcs.github.io/jj/).
//
// # Color scheme
//
// The color scheme is hardcoded in [Styles] as a flat map of label names to
// [Style] values. Labels are intentionally flat strings ("error_heading",
// "change_id") rather than hierarchical -- every needed combination is its own
// key. The defaults are derived from jj's built-in colors.toml
// (cli/src/config/colors.toml in the jj repo).
//
// # Styled output
//
// [UI] wraps an [io.Writer] and gates color output based on a [ColorMode]:
//
//   - [ColorAuto]: emit ANSI codes only when writing to a terminal and NO_COLOR
//     is unset.
//   - [ColorAlways] / [ColorNever]: unconditional.
//
// Use [UI.Styled] to wrap a string with the ANSI codes for a given label:
//
//	fmt.Fprintf(u, "Pushing %s to %s...\n",
//	    u.Styled("change_id", rev.ID),
//	    u.Styled("remote", remote))
//
// UI also implements [io.Writer] as a plain pass-through, so it can be used
// directly with fmt.Fprintf.
//
// # Structured messages
//
// [UI.PrintError], [UI.PrintWarning], and [UI.PrintHint] format messages in
// jj's style:
//
//	Error: <message>           (red bold heading, bold message)
//	Caused by: <source>        (bold heading, default source text)
//	Warning: <message>         (yellow bold heading)
//	Hint: <message>            (cyan bold heading)
//
// [PrintError] walks the error chain via [errors.Unwrap] to produce numbered
// "Caused by:" lines when there are multiple sources. [UserError] can carry an
// optional Hint that is appended automatically.
package ui
