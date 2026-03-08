package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"errors"
	"slices"

	jjforge "github.com/msuozzo/jj-forge"
	"github.com/msuozzo/jj-forge/internal/change"
	"github.com/msuozzo/jj-forge/internal/check"
	cmdpkg "github.com/msuozzo/jj-forge/internal/cmd"
	"github.com/msuozzo/jj-forge/internal/detach"
	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/forge/github"
	"github.com/msuozzo/jj-forge/internal/forge/ssm"
	"github.com/msuozzo/jj-forge/internal/help"
	"github.com/msuozzo/jj-forge/internal/jj"
	"github.com/msuozzo/jj-forge/internal/repoclone"
	"github.com/msuozzo/jj-forge/internal/review"
	"github.com/msuozzo/jj-forge/internal/templates"
	"github.com/msuozzo/jj-forge/internal/ui"
	"github.com/spf13/cobra"
)

var (
	repoPath    string
	debugPrompt string
	colorFlag   string
	detached    bool
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
	{"pr", "edit"},
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

// getForge returns a forge client and the resolved remote URL for the
// repository, auto-detecting the forge type from the upstream remote URL.
func getForge(ctx context.Context, jjClient jj.Client, upstreamRemote string) (forge.Forge, string, error) {
	url, err := jjClient.RemoteURL(ctx, upstreamRemote)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get remote URL for %s: %w", upstreamRemote, err)
	}
	forgeType, err := forge.DetectForge(ctx, url, forge.DefaultHTTPClient())
	if err != nil {
		return nil, "", &ui.UserError{
			Msg: fmt.Sprintf("could not determine forge for remote %s: %s", upstreamRemote, url),
		}
	}
	switch forgeType {
	case forge.ForgeTypeSSM:
		client, err := ssm.NewClientFromURL(ctx, url, cmdpkg.DefaultExecutor)
		return client, url, err
	case forge.ForgeTypeGitHub:
		gitDir, err := jjClient.GitDir(ctx)
		if err != nil {
			return nil, "", fmt.Errorf("failed to get git directory: %w", err)
		}
		return github.NewClientWithExecutor(gitDir, newGHExecutor(gitDir)), url, nil
	case forge.ForgeTypeGitLab:
		return nil, "", &ui.UserError{
			Msg: "GitLab is not yet supported",
		}
	default:
		return nil, "", &ui.UserError{
			Msg: fmt.Sprintf("could not determine forge type for remote %s: %s", upstreamRemote, url),
		}
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
	rootCmd.CompletionOptions.SetDefaultShellCompDirective(cobra.ShellCompDirectiveNoFileComp)

	rootCmd.PersistentFlags().StringVarP(&repoPath, "repo", "R", "", "Path to the repository")
	rootCmd.PersistentFlags().StringVar(&debugPrompt, "debug-prompt", "none", "Prompt before commands: none, writes, all")
	rootCmd.PersistentFlags().StringVar(&colorFlag, "color", "auto", "When to use colors (always, never, auto)")
	rootCmd.PersistentFlags().BoolVar(&detached, "_detached", false, "Internal: indicates this process was re-exec'd in detached mode")
	rootCmd.PersistentFlags().MarkHidden("_detached")
	rootCmd.MarkPersistentFlagDirname("repo")
	rootCmd.RegisterFlagCompletionFunc("color", cobra.FixedCompletions([]string{"always", "never", "auto"}, cobra.ShellCompDirectiveNoFileComp))
	rootCmd.RegisterFlagCompletionFunc("debug-prompt", cobra.FixedCompletions([]string{"none", "writes", "all"}, cobra.ShellCompDirectiveNoFileComp))

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

	// Change command group
	changeCmd := &cobra.Command{
		Use:   "change",
		Short: "Manage change content and lifecycle",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	// Check command
	var checkForce bool
	var checkDetach bool
	checkCmd := &cobra.Command{
		Use:               "check [REVSET]",
		Short:             "Run the configured check command against the given revset",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := jj.NewClientWithExecutor(repoPath, newJJExecutor())
			repoRoot, err := client.Root(ctx)
			if err != nil {
				return fmt.Errorf("failed to get repo root: %w", err)
			}
			forgeDir, err := forge.Dir(repoRoot)
			if err != nil {
				return err
			}
			proc := detach.New("check", forgeDir, detach.FlagReplace("--detach", "--_detached"))
			if checkDetach {
				pid, err := proc.Start(os.Args)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "jj-forge change check running in background (pid %d), logging to %s\n", pid, proc.LogPath())
				return nil
			}
			if detached {
				defer proc.Cleanup()
			}
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
			checkCommand, err := configMgr.GetCheckCommand()
			if err != nil {
				return fmt.Errorf("failed to get check command: %w", err)
			}
			if checkCommand == "" {
				return &ui.UserError{
					Msg:  "no check command configured",
					Hint: "Run 'jj config set --repo forge.check-command \"<command>\"' to configure one.",
				}
			}
			return check.Run(ctx, client, configMgr, revset, checkForce, newJJExecutor(), stdoutUI)
		},
	}
	checkCmd.Flags().BoolVar(&checkForce, "force", false, "Re-run checks even if cached verdicts are passing")
	checkCmd.Flags().BoolVar(&checkDetach, "detach", false, "Run in the background")

	var uploadRemote string
	var uploadSkipCheck bool
	uploadCmd := &cobra.Command{
		Use:               "upload [REVSET]",
		Short:             "Synchronize content and dependency structure to the remote",
		Long:              `Analyzes the stack, updates forge-parent trailers, and pushes to the remote.`,
		Deprecated:        "use 'review open', 'review update', or 'change submit' instead",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := jj.NewClientWithExecutor(repoPath, newJJExecutor())
			var revset string
			if len(args) > 0 {
				revset = args[0]
			} else {
				var err error
				revset, err = resolveDefaultStackRevset(ctx, client)
				if err != nil {
					return err
				}
			}
			if !uploadSkipCheck {
				configMgr := forge.NewConfigManager(client)
				if err := check.Run(ctx, client, configMgr, revset, false, newJJExecutor(), stdoutUI); err != nil {
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
		Use:   "submit [REVSET]",
		Short: "Land changes directly to main without PR review",
		Long: `Submit lands commits directly by fast-forwarding the target branch.

This is suitable for solo projects or develop-on-main workflows where
PR-based review is not required. For team workflows with code review,
use 'review open' and 'review submit' instead.`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := jj.NewClientWithExecutor(repoPath, newJJExecutor())
			var revset string
			if len(args) > 0 {
				revset = args[0]
			} else {
				var err error
				revset, err = resolveDefaultStackRevset(ctx, client)
				if err != nil {
					return err
				}
			}
			configMgr := forge.NewConfigManager(client)
			if !submitSkipCheck {
				if err := check.Run(ctx, client, configMgr, revset, false, newJJExecutor(), stdoutUI); err != nil {
					return err
				}
			}
			result, err := change.Submit(ctx, client, configMgr, revset, submitRemote, submitBranch, stdoutUI)
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

	changeCmd.AddCommand(checkCmd)
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
	var openSkipCheck bool
	openCmd := &cobra.Command{
		Use:               "open [REVSET]",
		Short:             "Create and assign a pull request",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			jjClient := jj.NewClientWithExecutor(repoPath, newJJExecutor())
			var revset string
			if len(args) > 0 {
				revset = args[0]
			} else {
				var err error
				revset, err = resolveDefaultStackRevset(ctx, jjClient)
				if err != nil {
					return err
				}
			}
			configMgr := forge.NewConfigManager(jjClient)
			// Phase 1: Update trailers
			trailerResult, err := change.UpdateTrailers(ctx, jjClient, revset, stdoutUI)
			if err != nil {
				return err
			}
			// Phase 2: Run checks (after trailers updated, before push)
			if !openSkipCheck {
				if err := check.Run(ctx, jjClient, configMgr, revset, false, newJJExecutor(), stdoutUI); err != nil {
					return err
				}
			}
			// Phase 3: Push
			// If no trailers were updated, commit IDs haven't changed — reuse resolved revs.
			var preResolved []*jj.Rev
			if trailerResult.TrailersUpdated == 0 {
				preResolved = trailerResult.Revs
			}
			pushResult, err := change.Push(ctx, jjClient, revset, openForkRemote, stdoutUI, preResolved)
			if err != nil {
				return err
			}
			// Print upload summary
			skipped := trailerResult.SkippedEmpty + trailerResult.SkippedAnonymous + pushResult.SkippedSynced
			if pushResult.Pushed > 0 || trailerResult.TrailersUpdated > 0 {
				fmt.Fprintf(stdoutUI, "Pushed %d change(s), updated %d trailer(s)\n", pushResult.Pushed, trailerResult.TrailersUpdated)
			}
			if skipped > 0 {
				fmt.Fprintf(stdoutUI, "Skipped %d change(s) (empty: %d, anonymous: %d, synced: %d)\n",
					skipped, trailerResult.SkippedEmpty, trailerResult.SkippedAnonymous, pushResult.SkippedSynced)
			}
			// Use resolved revs from trailer phase if trailers weren't updated;
			// otherwise re-resolve since commit IDs changed.
			var revs []*jj.Rev
			if trailerResult.TrailersUpdated == 0 {
				revs = make([]*jj.Rev, len(trailerResult.Revs))
				copy(revs, trailerResult.Revs)
				slices.Reverse(revs) // parent-first (topological) order
			} else {
				revs, err = jjClient.Revs(ctx, revset)
				if err != nil {
					return fmt.Errorf("failed to resolve revset: %w", err)
				}
				slices.Reverse(revs) // parent-first (topological) order
			}
			forgeClient, upstreamRemoteURL, err := getForge(ctx, jjClient, openUpstreamRemote)
			if err != nil {
				return err
			}
			// For forges without fork support, use upstream as fork remote
			if !forgeClient.SupportsForks() {
				openForkRemote = openUpstreamRemote
			}
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
			// Open reviews for each revision
			opened, skipped := 0, 0
			for _, rev := range revs {
				if rev.IsEmpty || strings.TrimSpace(rev.Description) == "" {
					skipped++
					continue
				}
				result, err := review.Open(ctx, jjClient, forgeClient, configMgr, review.OpenParams{
					Rev:               rev.ID,
					Reviewers:         reviewers,
					UpstreamRemote:    openUpstreamRemote,
					UpstreamRemoteURL: upstreamRemoteURL,
					ForkRemote:        openForkRemote,
				})
				if err != nil {
					if errors.Is(err, review.ErrReviewAlreadyExists) {
						fmt.Fprintf(stdoutUI, "Skipping change %s: %s\n",
							stdoutUI.Styled("change_id", rev.ID), err)
						skipped++
						continue
					}
					return err
				}
				fmt.Fprintf(stdoutUI, "Created review %s for change %s\n",
					stdoutUI.Styled("review_number", fmt.Sprintf("#%d", result.Number)),
					stdoutUI.Styled("change_id", result.ChangeID))
				fmt.Fprintf(stdoutUI, "URL: %s\n", stdoutUI.Styled("url", result.URL))
				opened++
			}
			fmt.Fprintf(stdoutUI, "Opened %d review(s), skipped %d\n", opened, skipped)
			// Update PR descriptions with parent/child links
			if opened > 0 {
				prsUpdated, err := review.UpdatePRLinks(ctx, jjClient, forgeClient, configMgr, revset, openUpstreamRemote, upstreamRemoteURL)
				if err != nil {
					stdoutUI.PrintWarning("failed to update PR links: %v", err)
				} else if prsUpdated > 0 {
					fmt.Fprintf(stdoutUI, "Updated %d PR description(s) with links\n", prsUpdated)
				}
			}
			return nil
		},
	}
	openCmd.Flags().StringSliceVar(&openReviewers, "reviewer", nil, "Usernames to assign as reviewers")
	openCmd.Flags().StringVar(&openUpstreamRemote, "upstream-remote", "up", "Remote to create PR against")
	openCmd.Flags().StringVar(&openForkRemote, "fork-remote", "og", "Remote where the branch is pushed")
	openCmd.Flags().BoolVar(&openSkipCheck, "skip-check", false, "Skip the configured check command")

	var mergeUpstreamRemote, mergeForkRemote string
	var mergeNoCleanup, mergeSkipCheck bool
	mergeCmd := &cobra.Command{
		Use:               "merge [REV]",
		Short:             "Merge a pull request",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: cobra.NoFileCompletions,
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
				if err := check.Run(ctx, jjClient, configMgr, rev, false, newJJExecutor(), stdoutUI); err != nil {
					return err
				}
			}
			forgeClient, upstreamRemoteURL, err := getForge(ctx, jjClient, mergeUpstreamRemote)
			if err != nil {
				return err
			}
			if !forgeClient.SupportsForks() {
				mergeForkRemote = mergeUpstreamRemote
			}
			// Execute merge command
			mergeParams := review.MergeParams{
				Rev:               rev,
				ForkRemote:        mergeForkRemote,
				UpstreamRemote:    mergeUpstreamRemote,
				UpstreamRemoteURL: upstreamRemoteURL,
				NoCleanup:         mergeNoCleanup,
				UI:                stdoutUI,
			}
			result, err := review.Merge(ctx, jjClient, forgeClient, configMgr, mergeParams)
			if errors.Is(err, review.ErrNotUploaded) {
				prompter := &cmdpkg.DefaultPrompter{}
				confirmed, promptErr := prompter.Confirm("Change has unpushed modifications. Run update before merging?", true)
				if promptErr != nil {
					return promptErr
				}
				if !confirmed {
					return err
				}
				if _, updateErr := review.Update(ctx, jjClient, forgeClient, configMgr, review.UpdateParams{
					Revset:            rev,
					ForkRemote:        mergeForkRemote,
					UpstreamRemote:    mergeUpstreamRemote,
					UpstreamRemoteURL: upstreamRemoteURL,
					UI:                stdoutUI,
				}); updateErr != nil {
					return updateErr
				}
				result, err = review.Merge(ctx, jjClient, forgeClient, configMgr, mergeParams)
			}
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
		Use:               "close [REV]",
		Short:             "Close a pull request and abandon the change",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: cobra.NoFileCompletions,
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
			forgeClient, upstreamRemoteURL, err := getForge(ctx, jjClient, closeUpstreamRemote)
			if err != nil {
				return err
			}
			if !forgeClient.SupportsForks() {
				closeForkRemote = closeUpstreamRemote
			}
			// Execute close command
			result, err := review.Close(ctx, jjClient, forgeClient, configMgr, review.CloseParams{
				Rev:               rev,
				ForkRemote:        closeForkRemote,
				UpstreamRemote:    closeUpstreamRemote,
				UpstreamRemoteURL: upstreamRemoteURL,
				Force:             closeForce,
				NoCleanup:         closeNoCleanup,
				UI:                stdoutUI,
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
		Use:               "import [REVSET]",
		Short:             "Find and import pull requests for revisions",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			jjClient := jj.NewClientWithExecutor(repoPath, newJJExecutor())
			var revset string
			if len(args) > 0 {
				revset = args[0]
			} else if !importAll {
				var err error
				revset, err = resolveDefaultRev(ctx, jjClient)
				if err != nil {
					return err
				}
			}
			if revset != "" && importAll {
				return fmt.Errorf("revset and --all are mutually exclusive")
			}
			configMgr := forge.NewConfigManager(jjClient)
			forgeClient, upstreamRemoteURL, err := getForge(ctx, jjClient, importUpstreamRemote)
			if err != nil {
				return err
			}
			result, err := review.Import(ctx, jjClient, forgeClient, configMgr, review.ImportParams{
				Revset:            revset,
				UpstreamRemote:    importUpstreamRemote,
				UpstreamRemoteURL: upstreamRemoteURL,
				All:               importAll,
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

	var updateUpstreamRemote, updateForkRemote string
	var updateSkipCheck bool
	updateCmd := &cobra.Command{
		Use:               "update [REVSET]",
		Short:             "Upload content and update PR descriptions with parent/child links",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			jjClient := jj.NewClientWithExecutor(repoPath, newJJExecutor())
			var revset string
			if len(args) > 0 {
				revset = args[0]
			} else {
				var err error
				revset, err = resolveDefaultStackRevset(ctx, jjClient)
				if err != nil {
					return err
				}
			}
			configMgr := forge.NewConfigManager(jjClient)
			var checkFn func() error
			if !updateSkipCheck {
				checkFn = func() error {
					return check.Run(ctx, jjClient, configMgr, revset, false, newJJExecutor(), stdoutUI)
				}
			}
			forgeClient, upstreamRemoteURL, err := getForge(ctx, jjClient, updateUpstreamRemote)
			if err != nil {
				return err
			}
			if !forgeClient.SupportsForks() {
				updateForkRemote = updateUpstreamRemote
			}
			result, err := review.Update(ctx, jjClient, forgeClient, configMgr, review.UpdateParams{
				Revset:            revset,
				ForkRemote:        updateForkRemote,
				UpstreamRemote:    updateUpstreamRemote,
				UpstreamRemoteURL: upstreamRemoteURL,
				UI:                stdoutUI,
				CheckFn:           checkFn,
			})
			if err != nil {
				return err
			}
			// Print summary
			ur := result.UploadResult
			if ur.Pushed > 0 || ur.TrailersUpdated > 0 {
				fmt.Fprintf(stdoutUI, "Pushed %d change(s), updated %d trailer(s)\n", ur.Pushed, ur.TrailersUpdated)
			}
			if ur.Skipped > 0 {
				fmt.Fprintf(stdoutUI, "Skipped %d change(s) (empty: %d, anonymous: %d, synced: %d)\n",
					ur.Skipped, ur.SkippedEmpty, ur.SkippedAnonymous, ur.SkippedSynced)
			}
			if result.PRsUpdated > 0 {
				fmt.Fprintf(stdoutUI, "Updated %d PR description(s)\n", result.PRsUpdated)
			}
			return nil
		},
	}
	updateCmd.Flags().StringVar(&updateForkRemote, "fork-remote", "og", "Remote where the branch is pushed")
	updateCmd.Flags().StringVar(&updateUpstreamRemote, "upstream-remote", "up", "Remote to update PRs on")
	updateCmd.Flags().BoolVar(&updateSkipCheck, "skip-check", false, "Skip the configured check command")

	reviewCmd.AddCommand(importCmd)
	reviewCmd.AddCommand(openCmd)
	reviewCmd.AddCommand(updateCmd)
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
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
			if len(args) == 1 {
				return nil, cobra.ShellCompDirectiveFilterDirs
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
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
			// Dispatch to SSM clone flow for SSM URLs
			forgeType, _ := forge.DetectForge(ctx, url, forge.DefaultHTTPClient())
			if forgeType == forge.ForgeTypeSSM {
				var ssmRunner *repoclone.SSMRunner
				if debugPrompt != "none" {
					ssmRunner = repoclone.NewSSMRunnerWithDeps(newJJExecutor(), stdoutUI)
				} else {
					ssmRunner = repoclone.NewSSMRunner(stdoutUI)
				}
				_, err := ssmRunner.Run(ctx, params)
				return err
			}
			var runner *repoclone.Runner
			if debugPrompt != "none" {
				ghClient := repoclone.NewGitHubClientWithExecutor(newGHExecutor(""))
				runner = repoclone.NewRunnerWithDeps(ghClient, newJJExecutor(), &cmdpkg.DefaultPrompter{}, stdoutUI)
			} else {
				runner = repoclone.NewRunner(stdoutUI)
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
		Use:               "setup-ruleset",
		Short:             "Add a GitHub ruleset to prevent merging forge-parent commits",
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			jjClient := jj.NewClientWithExecutor(repoPath, newJJExecutor())
			forgeClient, upstreamURL, err := getForge(ctx, jjClient, rulesetUpstreamRemote)
			if err != nil {
				return err
			}
			// Execute setup-ruleset command
			err = forgeClient.SetupRuleset(ctx, upstreamURL)
			if err != nil {
				return err
			}
			fmt.Fprintf(stdoutUI, "Successfully added ruleset to %s\n", stdoutUI.Styled("url", upstreamURL))
			return nil
		},
	}
	setupRulesetCmd.Flags().StringVar(&rulesetUpstreamRemote, "upstream-remote", "up", "Remote to target")

	var setupTemplatesUser bool
	setupTemplatesCmd := &cobra.Command{
		Use:               "setup-templates",
		Short:             "Set template-aliases in jj config for forge visualization",
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			aliases, err := templates.ParseTemplateAliases(jjforge.TemplatesTOML)
			if err != nil {
				return err
			}
			scope := "--repo"
			if setupTemplatesUser {
				scope = "--user"
			}
			jjClient := jj.NewClientWithExecutor(repoPath, newJJExecutor())
			if err := templates.Apply(ctx, jjClient, scope, aliases); err != nil {
				return err
			}
			fmt.Fprintf(stdoutUI, "Set %d template-alias(es) in %s config\n", len(aliases), scope[2:])
			return nil
		},
	}
	setupTemplatesCmd.Flags().BoolVar(&setupTemplatesUser, "user", false, "Set in user config instead of repo config")

	repoCmd.AddCommand(cloneCmd)
	repoCmd.AddCommand(setupRulesetCmd)
	repoCmd.AddCommand(setupTemplatesCmd)
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
