package main

import (
	"context"
	"fmt"
	"os"

	"github.com/msuozzo/jj-forge/internal/change"
	"github.com/msuozzo/jj-forge/internal/check"
	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/forge/github"
	"github.com/msuozzo/jj-forge/internal/jj"
	"github.com/msuozzo/jj-forge/internal/repoclone"
	"github.com/msuozzo/jj-forge/internal/review"
	"github.com/spf13/cobra"
)

var (
	repoPath string
)

func main() {
	ctx := context.Background()

	rootCmd := &cobra.Command{
		Use:   "jj-forge",
		Short: "jj-forge is a translation layer between jj and code forges like GitHub",
	}

	rootCmd.PersistentFlags().StringVarP(&repoPath, "repo", "R", "", "Path to the repository")

	// Check command
	checkCmd := &cobra.Command{
		Use:   "check [REVSET]",
		Short: "Run the configured check command against the given revset",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := jj.NewClient(repoPath)
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
			return check.Run(ctx, client, configMgr, revset, true, check.DefaultRunner)
		},
	}
	rootCmd.AddCommand(checkCmd)

	// Change command group
	changeCmd := &cobra.Command{
		Use:   "change",
		Short: "Manage change content and lifecycle",
	}

	var uploadRemote string
	var uploadSkipCheck bool
	uploadCmd := &cobra.Command{
		Use:   "upload [REVSET]",
		Short: "Synchronize content and dependency structure to the remote",
		Long:  `Analyzes the stack, updates forge-parent trailers, and pushes to the remote.`,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := jj.NewClient(repoPath)
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
				if err := check.Run(ctx, client, configMgr, revset, false, check.DefaultRunner); err != nil {
					return err
				}
			}
			result, err := change.Upload(ctx, client, revset, uploadRemote)
			if err != nil {
				return err
			}

			// Print summary
			if result.Pushed > 0 || result.TrailersUpdated > 0 {
				fmt.Printf("Pushed %d change(s), updated %d trailer(s)\n", result.Pushed, result.TrailersUpdated)
			}
			if result.Skipped > 0 {
				fmt.Printf("Skipped %d change(s) (empty: %d, anonymous: %d, synced: %d)\n",
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

			client := jj.NewClient(repoPath)
			if !submitSkipCheck {
				configMgr := forge.NewConfigManager(client)
				if err := check.Run(ctx, client, configMgr, revset, false, check.DefaultRunner); err != nil {
					return err
				}
			}
			result, err := change.Submit(ctx, client, revset, submitRemote, submitBranch)
			if err != nil {
				return err
			}

			fmt.Printf("Submitted %d change(s)\n", result.Submitted)
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
	}

	var openReviewers []string
	var openUpstreamRemote, openForkRemote string
	openCmd := &cobra.Command{
		Use:   "open [REV]",
		Short: "Create and assign a pull request",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			jjClient := jj.NewClient(repoPath)
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
			githubClient := github.NewClient(gitDir)
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
			fmt.Printf("Created review #%d for change %s\n", result.Number, result.ChangeID)
			fmt.Printf("URL: %s\n", result.URL)
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
			jjClient := jj.NewClient(repoPath)
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
				if err := check.Run(ctx, jjClient, configMgr, rev, false, check.DefaultRunner); err != nil {
					return err
				}
			}
			// Create GitHub client
			gitDir, err := jjClient.GitDir(ctx)
			if err != nil {
				return fmt.Errorf("failed to get git directory: %w", err)
			}
			githubClient := github.NewClient(gitDir)
			// Execute merge command
			result, err := review.Merge(ctx, jjClient, githubClient, configMgr, review.MergeParams{
				Rev:            rev,
				ForkRemote:     mergeForkRemote,
				UpstreamRemote: mergeUpstreamRemote,
				NoCleanup:      mergeNoCleanup,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Merged review #%d for change %s\n", result.Number, result.ChangeID)
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
			jjClient := jj.NewClient(repoPath)
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
			githubClient := github.NewClient(gitDir)
			// Execute close command
			result, err := review.Close(ctx, jjClient, githubClient, configMgr, review.CloseParams{
				Rev:            rev,
				ForkRemote:     closeForkRemote,
				UpstreamRemote: closeUpstreamRemote,
				Force:          closeForce,
				NoCleanup:      closeNoCleanup,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Closed review #%d and abandoned change %s\n", result.Number, result.ChangeID)
			return nil
		},
	}
	closeCmd.Flags().StringVar(&closeForkRemote, "fork-remote", "og", "Remote to use")
	closeCmd.Flags().StringVar(&closeUpstreamRemote, "upstream-remote", "up", "Remote of upstream")
	closeCmd.Flags().BoolVar(&closeForce, "force", false, "Skip confirmation prompt")
	closeCmd.Flags().BoolVar(&closeNoCleanup, "no-cleanup", false, "Skip local cleanup after close")

	reviewCmd.AddCommand(openCmd)
	reviewCmd.AddCommand(mergeCmd)
	reviewCmd.AddCommand(closeCmd)
	rootCmd.AddCommand(reviewCmd)

	// Repo command group
	repoCmd := &cobra.Command{
		Use:   "repo",
		Short: "Repository setup and configuration",
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
			_, err := repoclone.Run(ctx, repoclone.Params{
				URL:            url,
				Path:           path,
				ForkRemote:     cloneForkRemote,
				UpstreamRemote: cloneUpstreamRemote,
				UseHTTPS:       cloneUseHTTPS,
				NoFork:         cloneNoFork,
			})
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
			jjClient := jj.NewClient(repoPath)
			// Create GitHub client
			gitDir, err := jjClient.GitDir(ctx)
			if err != nil {
				return fmt.Errorf("failed to get git directory: %w", err)
			}
			githubClient := github.NewClient(gitDir)
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
			fmt.Printf("Successfully added ruleset to %s\n", upstreamURL)
			return nil
		},
	}
	setupRulesetCmd.Flags().StringVar(&rulesetUpstreamRemote, "upstream-remote", "up", "Remote to target")

	repoCmd.AddCommand(cloneCmd)
	repoCmd.AddCommand(setupRulesetCmd)
	rootCmd.AddCommand(repoCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
