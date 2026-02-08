package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/msuozzo/jj-forge/internal/change"
	"github.com/msuozzo/jj-forge/internal/check"
	cmdpkg "github.com/msuozzo/jj-forge/internal/cmd"
	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/forge/github"
	"github.com/msuozzo/jj-forge/internal/help"
	"github.com/msuozzo/jj-forge/internal/jj"
	"github.com/msuozzo/jj-forge/internal/repoclone"
	"github.com/msuozzo/jj-forge/internal/review"
	"github.com/msuozzo/jj-forge/internal/ui"
	"github.com/spf13/cobra"
)

var (
	repoPath    string
	debugPrompt string
	colorFlag   string
)

var (
	stdoutUI *ui.UI
	stderrUI *ui.UI
)

var jjConfirmOps = [][]string{
	{"describe"},
	{"git", "push"},
	{"bookmark", "set"},
	{"bookmark", "delete"},
	{"abandon"},
	{"util", "exec"},
}

var ghConfirmOps = [][]string{
	{"pr", "create"},
	{"pr", "merge"},
	{"pr", "close"},
	{"repo", "fork"},
	{"repo", "create"},
	{"api", "--method"},
}

func newJJExecutor() cmdpkg.Executor {
	switch debugPrompt {
	case "all":
		return cmdpkg.NewPromptingExecutor(cmdpkg.DefaultExecutor, &cmdpkg.DefaultPrompter{}, nil)
	case "writes":
		return cmdpkg.NewPromptingExecutor(cmdpkg.DefaultExecutor, &cmdpkg.DefaultPrompter{}, jjConfirmOps)
	default:
		return cmdpkg.DefaultExecutor
	}
}

func newGHExecutor(gitDir string) cmdpkg.Executor {
	base := github.DefaultExecutor(gitDir)
	switch debugPrompt {
	case "all":
		return cmdpkg.NewPromptingExecutor(base, &cmdpkg.DefaultPrompter{}, nil)
	case "writes":
		return cmdpkg.NewPromptingExecutor(base, &cmdpkg.DefaultPrompter{}, ghConfirmOps)
	default:
		return base
	}
}

func main() {
	ctx := context.Background()

	rootCmd := &cobra.Command{
		Use:   "jj-forge",
		Short: "jj-forge is a translation layer between jj and code forges like GitHub",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			var mode ui.ColorMode
			switch colorFlag {
			case "always":
				mode = ui.ColorAlways
			case "never":
				mode = ui.ColorNever
			default:
				mode = ui.ColorAuto
			}
			stdoutUI = ui.New(os.Stdout, mode)
			stderrUI = ui.New(os.Stderr, mode)
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	rootCmd.PersistentFlags().StringVarP(&repoPath, "repo", "R", "", "Path to the repository")
	rootCmd.PersistentFlags().StringVar(&debugPrompt, "debug-prompt", "none", "Prompt before commands: none, writes, all")
	rootCmd.PersistentFlags().StringVar(&colorFlag, "color", "auto", "When to use colors (always, never, auto)")

	// Set up help renderer with lazy UI resolution so --color flag takes effect
	help.Setup(rootCmd, func() *ui.UI {
		var mode ui.ColorMode
		switch colorFlag {
		case "always":
			mode = ui.ColorAlways
		case "never":
			mode = ui.ColorNever
		default:
			mode = ui.ColorAuto
		}
		return ui.New(os.Stdout, mode)
	})

	// Check command
	var checkForce bool
	checkCmd := &cobra.Command{
		Use:   "check [REVSET]",
		Short: "Run the configured check command against the given revset",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := jj.NewClientWithExecutor(repoPath, newJJExecutor())
			var revset string
			if len(args) > 0 {
				revset = args[0]
			} else {
				var err error
				revset, err = resolveDefaultRev(ctx, client)
				if err != nil {
					return err
				}
			}
			configMgr := forge.NewConfigManager(client)
			return check.Run(ctx, client, configMgr, revset, checkForce, newJJExecutor())
		},
	}
	checkCmd.Flags().BoolVar(&checkForce, "force", false, "Re-run checks even if cached verdicts are passing")
	rootCmd.AddCommand(checkCmd)

	// Change command group
	changeCmd := &cobra.Command{
		Use:   "change",
		Short: "Manage change content and lifecycle",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	var uploadRemote string
	var uploadSkipCheck bool
	uploadCmd := &cobra.Command{
		Use:   "upload [REVSET]",
		Short: "Synchronize content and dependency structure to the remote",
		Long:  `Analyzes the stack, updates forge-parent trailers, and pushes to the remote.`,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := jj.NewClientWithExecutor(repoPath, newJJExecutor())
			var revset string
			if len(args) > 0 {
				revset = args[0]
			} else {
				var err error
				revset, err = resolveDefaultRev(ctx, client)
				if err != nil {
					return err
				}
			}
			if !uploadSkipCheck {
				configMgr := forge.NewConfigManager(client)
				if err := check.Run(ctx, client, configMgr, revset, false, newJJExecutor()); err != nil {
					return err
				}
			}
			result, err := change.Upload(ctx, client, revset, uploadRemote, stdoutUI)
			if err != nil {
				return err
			}

			// Print summary
			if result.Pushed > 0 || result.TrailersUpdated > 0 {
				fmt.Fprintf(stdoutUI, "Pushed %d change(s), updated %d trailer(s)\n", result.Pushed, result.TrailersUpdated)
			}
			if result.Skipped > 0 {
				fmt.Fprintf(stdoutUI, "Skipped %d change(s) (empty: %d, anonymous: %d, synced: %d)\n",
					result.Skipped, result.SkippedEmpty, result.SkippedAnonymous, result.SkippedSynced)
			}
			return nil
		},
	}
	uploadCmd.Flags().StringVar(&uploadRemote, "remote", "og", "Remote to push to")
	uploadCmd.Flags().BoolVar(&uploadSkipCheck, "skip-check", false, "Skip the configured check command")

	var submitRemote, submitBranch string
	var submitSkipCheck bool
	submitCmd := &cobra.Command{
		Use:   "submit REVSET",
		Short: "Land changes directly to main without PR review",
		Long: `Submit lands commits directly by fast-forwarding the target branch.

This is suitable for solo projects or develop-on-main workflows where
PR-based review is not required. For team workflows with code review,
use 'review open' and 'review submit' instead.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			revset := args[0]

			client := jj.NewClientWithExecutor(repoPath, newJJExecutor())
			if !submitSkipCheck {
				configMgr := forge.NewConfigManager(client)
				if err := check.Run(ctx, client, configMgr, revset, false, newJJExecutor()); err != nil {
					return err
				}
			}
			result, err := change.Submit(ctx, client, revset, submitRemote, submitBranch, stdoutUI)
			if err != nil {
				return err
			}

			fmt.Fprintf(stdoutUI, "Submitted %d change(s)\n", result.Submitted)
			return nil
		},
	}
	submitCmd.Flags().StringVar(&submitRemote, "remote", "og", "Remote to push to")
	submitCmd.Flags().StringVar(&submitBranch, "branch", "main", "Target branch to fast-forward")
	submitCmd.Flags().BoolVar(&submitSkipCheck, "skip-check", false, "Skip the configured check command")

	changeCmd.AddCommand(uploadCmd)
	changeCmd.AddCommand(submitCmd)
	rootCmd.AddCommand(changeCmd)

	// Review command group
	reviewCmd := &cobra.Command{
		Use:   "review",
		Short: "Manage pull request reviews",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	var openReviewers []string
	var openUpstreamRemote, openForkRemote string
	openCmd := &cobra.Command{
		Use:   "open [REV]",
		Short: "Create and assign a pull request",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			jjClient := jj.NewClientWithExecutor(repoPath, newJJExecutor())
			var rev string
			if len(args) > 0 {
				rev = args[0]
			} else {
				var err error
				rev, err = resolveDefaultRev(ctx, jjClient)
				if err != nil {
					return err
				}
			}
			configMgr := forge.NewConfigManager(jjClient)
			// Create GitHub client
			// TODO: Detect and select another forge if not github hosted
			gitDir, err := jjClient.GitDir(ctx)
			if err != nil {
				return fmt.Errorf("failed to get git directory: %w", err)
			}
			githubClient := github.NewClientWithExecutor(gitDir, newGHExecutor(gitDir))
			// Get reviewers (flag or config default)
			reviewers := openReviewers
			if len(reviewers) == 0 {
				defaultReviewer, err := configMgr.GetDefaultReviewer()
				if err != nil {
					return fmt.Errorf("failed to get default reviewer: %w", err)
				}
				if defaultReviewer != "" {
					reviewers = []string{defaultReviewer}
				}
			}
			// Execute open command
			result, err := review.Open(ctx, jjClient, githubClient, configMgr, review.OpenParams{
				Rev:            rev,
				Reviewers:      reviewers,
				UpstreamRemote: openUpstreamRemote,
				ForkRemote:     openForkRemote,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(stdoutUI, "Created review %s for change %s\n",
				stdoutUI.Styled("review_number", fmt.Sprintf("#%d", result.Number)),
				stdoutUI.Styled("change_id", result.ChangeID))
			fmt.Fprintf(stdoutUI, "URL: %s\n", stdoutUI.Styled("url", result.URL))
			return nil
		},
	}
	openCmd.Flags().StringSliceVar(&openReviewers, "reviewer", nil, "GitHub usernames to assign as reviewers")
	openCmd.Flags().StringVar(&openUpstreamRemote, "upstream-remote", "up", "Remote to create PR against")
	openCmd.Flags().StringVar(&openForkRemote, "fork-remote", "og", "Remote where the branch is pushed")

	var mergeUpstreamRemote, mergeForkRemote string
	var mergeNoCleanup, mergeSkipCheck bool
	mergeCmd := &cobra.Command{
		Use:   "merge [REV]",
		Short: "Merge a pull request",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			jjClient := jj.NewClientWithExecutor(repoPath, newJJExecutor())
			var rev string
			if len(args) > 0 {
				rev = args[0]
			} else {
				var err error
				rev, err = resolveDefaultRev(ctx, jjClient)
				if err != nil {
					return err
				}
			}
			configMgr := forge.NewConfigManager(jjClient)
			if !mergeSkipCheck {
				if err := check.Run(ctx, jjClient, configMgr, rev, false, newJJExecutor()); err != nil {
					return err
				}
			}
			// Create GitHub client
			gitDir, err := jjClient.GitDir(ctx)
			if err != nil {
				return fmt.Errorf("failed to get git directory: %w", err)
			}
			githubClient := github.NewClientWithExecutor(gitDir, newGHExecutor(gitDir))
			// Execute merge command
			result, err := review.Merge(ctx, jjClient, githubClient, configMgr, review.MergeParams{
				Rev:            rev,
				ForkRemote:     mergeForkRemote,
				UpstreamRemote: mergeUpstreamRemote,
				NoCleanup:      mergeNoCleanup,
				UI:             stdoutUI,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(stdoutUI, "Merged review %s for change %s\n",
				stdoutUI.Styled("review_number", fmt.Sprintf("#%d", result.Number)),
				stdoutUI.Styled("change_id", result.ChangeID))
			return nil
		},
	}
	mergeCmd.Flags().StringVar(&mergeForkRemote, "fork-remote", "og", "Remote of fork")
	mergeCmd.Flags().StringVar(&mergeUpstreamRemote, "upstream-remote", "up", "Remote of upstream")
	mergeCmd.Flags().BoolVar(&mergeNoCleanup, "no-cleanup", false, "Skip local cleanup after merge")
	mergeCmd.Flags().BoolVar(&mergeSkipCheck, "skip-check", false, "Skip the configured check command")

	var closeForkRemote, closeUpstreamRemote string
	var closeForce, closeNoCleanup bool
	closeCmd := &cobra.Command{
		Use:   "close [REV]",
		Short: "Close a pull request and abandon the change",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			jjClient := jj.NewClientWithExecutor(repoPath, newJJExecutor())
			var rev string
			if len(args) > 0 {
				rev = args[0]
			} else {
				var err error
				rev, err = resolveDefaultRev(ctx, jjClient)
				if err != nil {
					return err
				}
			}
			configMgr := forge.NewConfigManager(jjClient)
			// Create GitHub client
			gitDir, err := jjClient.GitDir(ctx)
			if err != nil {
				return fmt.Errorf("failed to get git directory: %w", err)
			}
			githubClient := github.NewClientWithExecutor(gitDir, newGHExecutor(gitDir))
			// Execute close command
			result, err := review.Close(ctx, jjClient, githubClient, configMgr, review.CloseParams{
				Rev:            rev,
				ForkRemote:     closeForkRemote,
				UpstreamRemote: closeUpstreamRemote,
				Force:          closeForce,
				NoCleanup:      closeNoCleanup,
				UI:             stdoutUI,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(stdoutUI, "Closed review %s and abandoned change %s\n",
				stdoutUI.Styled("review_number", fmt.Sprintf("#%d", result.Number)),
				stdoutUI.Styled("change_id", result.ChangeID))
			return nil
		},
	}
	closeCmd.Flags().StringVar(&closeForkRemote, "fork-remote", "og", "Remote to use")
	closeCmd.Flags().StringVar(&closeUpstreamRemote, "upstream-remote", "up", "Remote of upstream")
	closeCmd.Flags().BoolVar(&closeForce, "force", false, "Skip confirmation prompt")
	closeCmd.Flags().BoolVar(&closeNoCleanup, "no-cleanup", false, "Skip local cleanup after close")

	var importUpstreamRemote string
	var importAll bool
	importCmd := &cobra.Command{
		Use:   "import [REV]",
		Short: "Find and import pull requests for revisions",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			jjClient := jj.NewClientWithExecutor(repoPath, newJJExecutor())
			var rev string
			if len(args) > 0 {
				rev = args[0]
			} else if !importAll {
				var err error
				rev, err = resolveDefaultRev(ctx, jjClient)
				if err != nil {
					return err
				}
			}
			if rev != "" && importAll {
				return fmt.Errorf("rev and --all are mutually exclusive")
			}
			configMgr := forge.NewConfigManager(jjClient)
			// Create GitHub client
			gitDir, err := jjClient.GitDir(ctx)
			if err != nil {
				return fmt.Errorf("failed to get git directory: %w", err)
			}
			githubClient := github.NewClientWithExecutor(gitDir, newGHExecutor(gitDir))
			result, err := review.Import(ctx, jjClient, githubClient, configMgr, review.ImportParams{
				Rev:            rev,
				UpstreamRemote: importUpstreamRemote,
				All:            importAll,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(stdoutUI, "Imported %d new review(s), updated %d existing review(s)\n", result.Added, result.Updated)
			return nil
		},
	}
	importCmd.Flags().StringVar(&importUpstreamRemote, "upstream-remote", "up", "Remote to search for PRs")
	importCmd.Flags().BoolVar(&importAll, "all", false, "Check all mutable revisions")

	reviewCmd.AddCommand(importCmd)
	reviewCmd.AddCommand(openCmd)
	reviewCmd.AddCommand(mergeCmd)
	reviewCmd.AddCommand(closeCmd)
	rootCmd.AddCommand(reviewCmd)

	// Repo command group
	repoCmd := &cobra.Command{
		Use:   "repo",
		Short: "Repository setup and configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	var cloneForkRemote, cloneUpstreamRemote string
	var cloneUseHTTPS, cloneNoFork bool
	cloneCmd := &cobra.Command{
		Use:   "clone <url> [path]",
		Short: "Clone repository with intelligent workflow detection",
		Long: `Clone and configure a repository with automatic workflow detection.

Workflow is determined by repository ownership:
  - Your non-fork repos: Develop-on-main workflow
  - Your fork repos: PR-based workflow
  - External repos: Creates fork, then PR-based workflow

The command will:
  - Analyze repository ownership and fork status
  - Clone or create the repository
  - Configure appropriate remotes (og/up)
  - Set up workflow preferences

Examples:
  jj-forge repo clone git@github.com:me/my-project.git
  jj-forge repo clone https://github.com/external/project.git
  jj-forge repo clone git@github.com:owner/repo.git custom-dir`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			url := args[0]
			var path string
			if len(args) > 1 {
				path = args[1]
			}
			params := repoclone.Params{
				URL:            url,
				Path:           path,
				ForkRemote:     cloneForkRemote,
				UpstreamRemote: cloneUpstreamRemote,
				UseHTTPS:       cloneUseHTTPS,
				NoFork:         cloneNoFork,
			}
			var runner *repoclone.Runner
			if debugPrompt != "none" {
				ghClient := repoclone.NewGitHubClientWithExecutor(newGHExecutor(""))
				runner = repoclone.NewRunnerWithDeps(ghClient, newJJExecutor(), &cmdpkg.DefaultPrompter{}, &repoclone.DefaultPrinter{})
			} else {
				runner = repoclone.NewRunner()
			}
			_, err := runner.Run(ctx, params)
			return err
		},
	}
	cloneCmd.Flags().StringVar(&cloneForkRemote, "fork-remote", "og", "Name for fork/personal remote")
	cloneCmd.Flags().StringVar(&cloneUpstreamRemote, "upstream-remote", "up", "Name for upstream remote")
	cloneCmd.Flags().BoolVar(&cloneUseHTTPS, "https", false, "Use HTTPS instead of SSH for remotes")
	cloneCmd.Flags().BoolVar(&cloneNoFork, "no-fork", false, "Don't create fork for external repos (fail instead)")

	var rulesetUpstreamRemote string
	setupRulesetCmd := &cobra.Command{
		Use:   "setup-ruleset",
		Short: "Add a GitHub ruleset to prevent merging forge-parent commits",
		RunE: func(cmd *cobra.Command, args []string) error {
			jjClient := jj.NewClientWithExecutor(repoPath, newJJExecutor())
			// Create GitHub client
			gitDir, err := jjClient.GitDir(ctx)
			if err != nil {
				return fmt.Errorf("failed to get git directory: %w", err)
			}
			githubClient := github.NewClientWithExecutor(gitDir, newGHExecutor(gitDir))
			// Get upstream URL
			upstreamURL, err := jjClient.RemoteURL(ctx, rulesetUpstreamRemote)
			if err != nil {
				return fmt.Errorf("failed to get upstream URL: %w", err)
			}
			// Execute setup-ruleset command
			err = githubClient.SetupRuleset(ctx, upstreamURL)
			if err != nil {
				return err
			}
			fmt.Fprintf(stdoutUI, "Successfully added ruleset to %s\n", stdoutUI.Styled("url", upstreamURL))
			return nil
		},
	}
	setupRulesetCmd.Flags().StringVar(&rulesetUpstreamRemote, "upstream-remote", "up", "Remote to target")

	repoCmd.AddCommand(cloneCmd)
	repoCmd.AddCommand(setupRulesetCmd)
	rootCmd.AddCommand(repoCmd)

	if err := rootCmd.Execute(); err != nil {
		if stderrUI == nil {
			stderrUI = ui.New(os.Stderr, ui.ColorAuto)
		}
		if !printUnknownCommandError(stderrUI, err) {
			stderrUI.PrintError(err)
		}
		os.Exit(1)
	}
}

// printUnknownCommandError detects Cobra's "unknown command" errors and prints
// them in jj's style. Returns true if the error was handled.
func printUnknownCommandError(u *ui.UI, err error) bool {
	msg := err.Error()
	// Cobra format: `unknown command "X" for "Y"` optionally followed by
	// `\n\nDid you mean this?\n\t<suggestion>`
	if !strings.HasPrefix(msg, "unknown command \"") {
		return false
	}
	// Extract the subcommand name between the first pair of quotes.
	start := len("unknown command \"")
	end := strings.Index(msg[start:], "\"")
	if end < 0 {
		return false
	}
	sub := msg[start : start+end]

	// Extract the command path between the second pair of quotes.
	rest := msg[start+end+len("\" for \""):]
	cmdEnd := strings.Index(rest, "\"")
	if cmdEnd < 0 {
		return false
	}
	cmdPath := rest[:cmdEnd]

	heading := u.Styled("error_heading", "Error: ")
	errMsg := u.Styled("error", fmt.Sprintf("unrecognized subcommand '%s'", sub))
	fmt.Fprintf(u, "%s%s\n", heading, errMsg)
	// Preserve "Did you mean" suggestions.
	if i := strings.Index(msg, "\n\nDid you mean"); i >= 0 {
		fmt.Fprintf(u, "%s", msg[i+1:strings.LastIndex(msg, "\n")+1])
	}
	fmt.Fprintf(u, "\n%s %s %s %s\n\nFor more information, try '%s'.\n",
		u.Styled("help_header", "Usage:"),
		u.Styled("help_command", cmdPath),
		u.Styled("help_placeholder", "[OPTIONS]"),
		u.Styled("help_placeholder", "<COMMAND>"),
		u.Styled("help_command", "--help"))
	return true
}
