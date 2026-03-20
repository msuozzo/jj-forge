package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
)

// Prompter handles user confirmation prompts.
type Prompter interface {
	Confirm(prompt string, defaultYes bool) (bool, error)
}

// DefaultPrompter implements Prompter using stdin/stdout.
type DefaultPrompter struct{}

// Confirm asks the user a yes/no question.
// When defaultYes is true, pressing Enter accepts; otherwise it declines.
func (p *DefaultPrompter) Confirm(prompt string, defaultYes bool) (bool, error) {
	suffix := " [Y/n]: "
	if !defaultYes {
		suffix = " [y/N]: "
	}
	fmt.Print(prompt + suffix)

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	response = strings.TrimSpace(strings.ToLower(response))

	if response == "" {
		return defaultYes, nil
	}
	return response == "y" || response == "yes", nil
}

// Choose asks the user to select from a list of options.
func (p *DefaultPrompter) Choose(prompt string, options []string, defaultIndex int) (int, error) {
	fmt.Println(prompt)
	for i, opt := range options {
		marker := "  "
		if i == defaultIndex {
			marker = "> "
		}
		fmt.Printf("%s%d. %s\n", marker, i+1, opt)
	}
	fmt.Print("Choice: ")

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return 0, err
	}
	response = strings.TrimSpace(response)

	if response == "" {
		return defaultIndex, nil
	}

	var choice int
	if _, err := fmt.Sscanf(response, "%d", &choice); err != nil || choice < 1 || choice > len(options) {
		return 0, fmt.Errorf("invalid choice: %s", response)
	}
	return choice - 1, nil
}

// NewPromptingExecutor wraps an Executor to prompt the user before executing
// commands that match any of the given confirm patterns. Each pattern is an
// arg-prefix matched against args[1:] (after stripping any leading -R <path>).
func NewPromptingExecutor(inner Executor, prompter Prompter, confirmOps [][]string) Executor {
	var mu sync.Mutex
	return func(ctx context.Context, opts Opts, args ...string) (*Result, error) {
		if len(args) > 0 && matchesOp(args[1:], confirmOps) {
			mu.Lock()
			// NOTE: Hard-code with a default accept.
			confirmed, err := prompter.Confirm(strings.Join(args, " "), true)
			mu.Unlock()
			if err != nil {
				return nil, err
			}
			if !confirmed {
				return nil, fmt.Errorf("command aborted by user")
			}
		}
		return inner(ctx, opts, args...)
	}
}

// matchesOp checks whether args (after stripping a leading -R <path>) match
// any of the given prefix patterns.
func matchesOp(args []string, patterns [][]string) bool {
	if patterns == nil {
		return true
	}
	// Strip leading -R <path> if present.
	if len(args) >= 2 && args[0] == "-R" {
		args = args[2:]
	}
	for _, pattern := range patterns {
		if len(args) >= len(pattern) {
			match := true
			for i, p := range pattern {
				if args[i] != p {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}
	return false
}
