package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Opts holds optional parameters for command execution.
type Opts struct {
	Stdin   io.Reader
	WorkDir string
	Env     []string // Additional env vars in "KEY=VALUE" format. Appended to os.Environ().
}

// ExecError represents a command that exited with a non-zero status.
type ExecError struct {
	Args   []string // The full command arguments.
	Stderr string   // Captured stderr output.
	Err    error    // Underlying error (typically *exec.ExitError).
}

func (e *ExecError) Error() string {
	return fmt.Sprintf("command failed: %s\nerror: %s\nstderr: %s",
		strings.Join(e.Args, " "), e.Err, e.Stderr)
}

func (e *ExecError) Unwrap() error { return e.Err }

// Executor defines the function signature for running shell commands.
// The first element of args is the binary name.
type Executor func(ctx context.Context, opts Opts, args ...string) (stdout string, err error)

// DefaultExecutor is an Executor that runs args[0] as the binary with
// args[1:] as arguments, honoring Opts.Stdin, Opts.WorkDir, and Opts.Env.
func DefaultExecutor(ctx context.Context, opts Opts, args ...string) (string, error) {
	c := exec.CommandContext(ctx, args[0], args[1:]...)
	if opts.WorkDir != "" {
		c.Dir = opts.WorkDir
	}
	if len(opts.Env) > 0 {
		c.Env = append(os.Environ(), opts.Env...)
	}
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	if opts.Stdin != nil {
		c.Stdin = opts.Stdin
	}
	if err := c.Run(); err != nil {
		return "", &ExecError{Args: args, Stderr: stderr.String(), Err: err}
	}
	return stdout.String(), nil
}
