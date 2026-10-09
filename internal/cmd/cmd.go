package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Opts holds optional parameters for command execution.
type Opts struct {
	Stdin   io.Reader
	WorkDir string
	Env     []string // Additional env vars in "KEY=VALUE" format. Appended to os.Environ().
	// ProcessGroup runs the command in its own process group. Cancelling the
	// context signals the whole group rather than just the command, and
	// anything the command started is killed once it exits. Processes that
	// start their own session or group, as some build daemons do, escape.
	ProcessGroup bool
}

// processGroupWaitDelay is how long a cancelled process group gets to exit
// after SIGTERM, and how long an exited command's leftover processes may keep
// its output open, before they are killed.
const processGroupWaitDelay = 3 * time.Second

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

// Result holds the output of a successfully executed command.
type Result struct {
	Stdout string
	Stderr string
}

// Executor defines the function signature for running shell commands.
// The first element of args is the binary name.
type Executor func(ctx context.Context, opts Opts, args ...string) (*Result, error)

// DefaultExecutor is an Executor that runs args[0] as the binary with
// args[1:] as arguments, honoring Opts.Stdin, Opts.WorkDir, and Opts.Env.
func DefaultExecutor(ctx context.Context, opts Opts, args ...string) (*Result, error) {
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
	if opts.ProcessGroup {
		c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGTERM) }
		c.WaitDelay = processGroupWaitDelay
	}
	err := c.Run()
	if opts.ProcessGroup && c.Process != nil {
		// The command has exited. Kill whatever it left running.
		syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
	// ErrWaitDelay alone means the command succeeded but left processes
	// holding its output, which are now killed. The exit status decides.
	if err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		return nil, &ExecError{Args: args, Stderr: stderr.String(), Err: err}
	}
	return &Result{Stdout: stdout.String(), Stderr: stderr.String()}, nil
}
