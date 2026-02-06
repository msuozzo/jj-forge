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
		Use:   "check REVSET",
		Short: "Run the configured check command against the given revset",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			revset := args[0]
			client := jj.NewClient(repoPath)
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
		Use:   "upload REVSET",
		Short: "Synchronize content and dependency structure to the remote",
		Long:  `Analyzes the stack, updates forge-parent trailers, and pushes to the remote.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			revset := args[0]
			client := jj.NewClient(repoPath)
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
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rev := args[0]
			jjClient := jj.NewClient(repoPath)
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
		Use:   "merge REV",
		Short: "Merge a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rev := args[0]
			jjClient := jj.NewClient(repoPath)
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
			rev := "@"
			if len(args) > 0 {
				rev = args[0]
			}
			jjClient := jj.NewClient(repoPath)
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

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
