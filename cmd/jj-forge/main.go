package main

import (
	"context"
	"fmt"
	"os"

	"github.com/msuozzo/jj-forge/internal/change"
	"github.com/msuozzo/jj-forge/internal/jj"
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

	// Change command group
	changeCmd := &cobra.Command{
		Use:   "change",
		Short: "Manage change content and lifecycle",
	}

	uploadCmd := &cobra.Command{
		Use:   "upload REVSET",
		Short: "Synchronize content and dependency structure to the remote",
		Long:  `Analyzes the stack, updates forge-parent trailers, and pushes to the remote.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not yet implemented")
		},
	}
	var submitRemote, submitBranch string
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

	changeCmd.AddCommand(uploadCmd)
	changeCmd.AddCommand(submitCmd)
	rootCmd.AddCommand(changeCmd)

	// Review command group
	reviewCmd := &cobra.Command{
		Use:   "review",
		Short: "Manage pull request reviews",
	}

	openCmd := &cobra.Command{
		Use:   "open [REV]",
		Short: "Create and assign a pull request",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not yet implemented")
		},
	}

	reviewSubmitCmd := &cobra.Command{
		Use:   "submit [REV]",
		Short: "Submit a pull request for merging through the forge",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not yet implemented")
		},
	}

	closeCmd := &cobra.Command{
		Use:   "close [REV]",
		Short: "Close a pull request",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not yet implemented")
		},
	}

	reviewCmd.AddCommand(openCmd)
	reviewCmd.AddCommand(reviewSubmitCmd)
	reviewCmd.AddCommand(closeCmd)
	rootCmd.AddCommand(reviewCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
