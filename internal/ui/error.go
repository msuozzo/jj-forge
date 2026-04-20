package ui

import (
	"errors"
	"fmt"
)

// UserError is an error with a user-facing message and optional hint.
type UserError struct {
	Msg     string
	Details string
	Hint    string
	Source  error
}

func (e *UserError) Error() string {
	if e.Source != nil {
		return fmt.Sprintf("%s: %s", e.Msg, e.Source.Error())
	}
	return e.Msg
}

func (e *UserError) Unwrap() error {
	return e.Source
}

// PrintError writes a jj-style error message to the UI's writer.
// It walks the error chain to produce:
//
//	Error: <top-level message>
//	Caused by: <source>
//
// Or with multiple sources:
//
//	Error: <top-level message>
//	Caused by:
//	  1: <source 1>
//	  2: <source 2>
func (u *UI) PrintError(err error) {
	heading := u.Styled("error_heading", "Error: ")
	msg := u.Styled("error", err.Error())

	// Collect the error chain (excluding the top-level error itself)
	var sources []error
	current := errors.Unwrap(err)
	for current != nil {
		sources = append(sources, current)
		current = errors.Unwrap(current)
	}

	if len(sources) == 0 {
		fmt.Fprintf(u.w, "%s%s\n", heading, msg)
	} else {
		// Use the top-level error's own message (without the chain).
		// For wrapped errors like "foo: bar", we want just "foo" as the top message.
		topMsg := err.Error()
		firstSource := sources[0].Error()
		// Strip the ": <source>" suffix if the top-level error includes it
		if len(topMsg) > len(firstSource)+2 {
			candidate := topMsg[:len(topMsg)-len(firstSource)-2]
			if topMsg == candidate+": "+firstSource {
				topMsg = candidate
			}
		}
		fmt.Fprintf(u.w, "%s%s\n", heading, u.Styled("error", topMsg))

		if len(sources) == 1 {
			sourceHeading := u.Styled("error_source_heading", "Caused by: ")
			sourceText := u.Styled("error_source", sources[0].Error())
			fmt.Fprintf(u.w, "%s%s\n", sourceHeading, sourceText)
		} else {
			fmt.Fprintf(u.w, "%s\n", u.Styled("error_source_heading", "Caused by:"))
			for i, source := range sources {
				sourceText := u.Styled("error_source", source.Error())
				fmt.Fprintf(u.w, "  %d: %s\n", i+1, sourceText)
			}
		}
	}

	// Check for UserError details
	var userErr *UserError
	if errors.As(err, &userErr) {
		if userErr.Details != "" {
			fmt.Fprintf(u.w, "%s\n", u.Styled("error_details", userErr.Details))
		}
		if userErr.Hint != "" {
			hintHeading := u.Styled("hint_heading", "Hint: ")
			fmt.Fprintf(u.w, "%s%s\n", hintHeading, userErr.Hint)
		}
	}
}

// PrintWarning writes a jj-style warning message to the UI's writer.
func (u *UI) PrintWarning(format string, args ...any) {
	heading := u.Styled("warning_heading", "Warning: ")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(u.w, "%s%s\n", heading, msg)
}

// PrintHint writes a jj-style hint message to the UI's writer.
func (u *UI) PrintHint(format string, args ...any) {
	heading := u.Styled("hint_heading", "Hint: ")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(u.w, "%s%s\n", heading, msg)
}
