package ui

// Styles maps flat labels to their corresponding styles, hardcoded from
// jj's default color scheme (cli/src/config/colors.toml).
var Styles = map[string]Style{
	// Error/warning/hint (matching jj's error formatting labels)
	"error_heading":        {Fg: Red, Bold: true},
	"error":                {Bold: true},
	"error_source_heading": {Bold: true},
	"error_source":         {},
	"warning_heading":      {Fg: Yellow, Bold: true},
	"hint_heading":         {Fg: Cyan, Bold: true},

	// Data types (matching jj's commit output labels)
	"change_id": {Fg: Magenta},
	"commit_id": {Fg: Blue},
	"bookmark":  {Fg: Magenta},
	"remote":    {Fg: Green},
	"timestamp": {Fg: Cyan},

	// jj-forge-specific
	"review_number": {Fg: Magenta},
	"url":           {Fg: Cyan},

	// Help text (matching Clap's color scheme)
	"help_header":      {Fg: Yellow, Bold: true},
	"help_command":     {Fg: Green, Bold: true},
	"help_placeholder": {Fg: Green},

	// Task status indicators
	"task_pass":    {Fg: Green},
	"task_fail":    {Fg: Red},
	"task_running": {Fg: Yellow},
	"task_pending": {Dim: true},
	"task_skipped": {Dim: true},
}
