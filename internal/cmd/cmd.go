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
		return "", fmt.Errorf("command failed: %s\nerror: %w\nstderr: %s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String(), nil
}
