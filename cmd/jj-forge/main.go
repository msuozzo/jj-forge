package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"errors"
	"slices"

	jjforge "github.com/msuozzo/jj-forge"
	"github.com/msuozzo/jj-forge/internal/actionsstatus"
	"github.com/msuozzo/jj-forge/internal/change"
	"github.com/msuozzo/jj-forge/internal/check"
	cmdpkg "github.com/msuozzo/jj-forge/internal/cmd"
	"github.com/msuozzo/jj-forge/internal/detach"
	"github.com/msuozzo/jj-forge/internal/forge"
	"github.com/msuozzo/jj-forge/internal/forge/github"
	"github.com/msuozzo/jj-forge/internal/forge/ssm"
	"github.com/msuozzo/jj-forge/internal/forge/tangled"
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
	{"metaedit"},
	{"git", "push"},
	{"bookmark", "set"},
	{"bookmark", "delete"},
	{"abandon"},
	{"util", "exec"},
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

var ghConfirmOps = [][]string{
	{"pr", "create"},
	{"pr", "merge"},
	{"pr", "close"},
	{"pr", "edit"},
	{"repo", "fork"},
	{"repo", "create"},
	{"api", "--method"},
}

func newGHExecutor() cmdpkg.Executor {
	switch debugPrompt {
	case "all":
		return cmdpkg.NewPromptingExecutor(cmdpkg.DefaultExecutor, &cmdpkg.DefaultPrompter{}, nil)
	case "writes":
		return cmdpkg.NewPromptingExecutor(cmdpkg.DefaultExecutor, &cmdpkg.DefaultPrompter{}, ghConfirmOps)
	default:
		return cmdpkg.DefaultExecutor
	}
}

var tgConfirmOps = [][]string{
	{"pr", "create"},
	{"pr", "merge"},
	{"pr", "close"},
	{"pr", "edit"},
	{"pr", "update"},
	{"repo", "fork"},
	{"repo", "create"},
}

func newTGExecutor() cmdpkg.Executor {
	switch debugPrompt {
	case "all":
		return cmdpkg.NewPromptingExecutor(cmdpkg.DefaultExecutor, &cmdpkg.DefaultPrompter{}, nil)
	case "writes":
		return cmdpkg.NewPromptingExecutor(cmdpkg.DefaultExecutor, &cmdpkg.DefaultPrompter{}, tgConfirmOps)
	default:
		return cmdpkg.DefaultExecutor
	}
}

// getForge returns a forge client and the resolved remote URL for the
// repository, auto-detecting the forge type from the upstream remote URL.
func getForge(ctx context.Context, jjClient jj.Client, upstreamRemote string) (forge.Forge, string, error) {
	url, err := jjClient.RemoteURL(ctx, upstreamRemote)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get remote URL for %s: %w", upstreamRemote, err)
	}
	configMgr := forge.NewConfigManager(jjClient)
	hosts, err := configMgr.GetHosts()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get host configuration: %w", err)
	}
	forgeType, err := forge.DetectForge(ctx, url, forge.DefaultHTTPClient(), hosts)
	if err != nil {
		return nil, "", &ui.UserError{
			Msg: fmt.Sprintf("could not determine forge for remote %s: %s", upstreamRemote, url),
		}
	}
	switch forgeType {
	case forge.ForgeTypeSSM:
		client, err := ssm.NewClientFromURL(ctx, url, cmdpkg.DefaultExecutor)
		return client, url, err
	case forge.ForgeTypeTangled:
		gitDir, err := jjClient.GitDir(ctx)
		if err != nil {
			return nil, "", fmt.Errorf("failed to get git directory: %w", err)
		}
		tgCmd, err := configMgr.GetToolCommand("tg")
		if err != nil {
			return nil, "", fmt.Errorf("failed to get tg command: %w", err)
		}
		client := tangled.NewClient(gitDir, upstreamRemote, jjClient, newTGExecutor()).WithTGCommand(tgCmd)
		return client, url, nil
	case forge.ForgeTypeGitHub:
		gitDir, err := jjClient.GitDir(ctx)
		if err != nil {
			return nil, "", fmt.Errorf("failed to get git directory: %w", err)
		}
		configMgr := forge.NewConfigManager(jjClient)
		ghCmd, err := configMgr.GetToolCommand("gh")
		if err != nil {
			return nil, "", fmt.Errorf("failed to get gh command: %w", err)
		}
		ghClient := github.NewClientWithExecutor(gitDir, newGHExecutor())
		if ghCmd != "" {
			ghClient.WithGHCommand(ghCmd)
		}
		return ghClient, url, nil
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
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	rootCmd := &cobra.Command{
		Use:   "jj-forge",
		Short: "jj-forge is a translation layer between jj and code forges like GitHub and Tangled",
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
			configMgr := forge.NewConfigManager(client)
			if !uploadSkipCheck {
				if err := check.Run(ctx, client, configMgr, revset, false, newJJExecutor(), stdoutUI); err != nil {
					return err
				}
			}
			remotes, err := forge.ResolveRemotes(ctx, client, configMgr, uploadRemote, "")
			if err != nil {
				return err
			}
			result, err := change.Upload(ctx, client, revset, remotes.Fork, stdoutUI)
			if err != nil {
				return err
			}

			// Print summary
			if result.Pushed > 0 || result.TrailersUpdated > 0 {
				fmt.Fprintf(stdoutUI, "Pushed %d change(s), updated %d trailer(s)\n", result.Pushed, result.TrailersUpdated)
			}
			if result.Skipped > 0 {
				fmt.Fprintf(stdoutUI, "Skipped %d change(s) (empty: %d, anonymous: %d, immutable: %d, synced: %d)\n",
					result.Skipped, result.SkippedEmpty, result.SkippedAnonymous, result.SkippedImmutable, result.SkippedSynced)
			}
			return nil
		},
	}
	uploadCmd.Flags().StringVar(&uploadRemote, "remote", "", "Remote to push to (default: git.push, else the only remote, else og)")
	uploadCmd.Flags().BoolVar(&uploadSkipCheck, "skip-check", false, "Skip the configured check command")

	var submitRemote, submitBranch string
	var submitSkipCheck bool
	submitCmd := &cobra.Command{
		Use:   "submit [REVSET]",
		Short: "Land changes directly to the trunk branch without PR review",
		Long: `Submit lands commits directly by fast-forwarding the target branch.

The target defaults to the bookmark the trunk() revset alias names (e.g.
master@og, as set by 'repo clone' and 'jj git clone'). When trunk() is not
a plain <branch>@<remote>, it is main on git.push's remote, else main@og.
With only --remote, the branch comes from trunk() when it is on that
remote, and is main otherwise.

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
	submitCmd.Flags().StringVar(&submitRemote, "remote", "", "Remote to push to (default: the trunk() remote, else git.push, else og)")
	submitCmd.Flags().StringVar(&submitBranch, "branch", "", "Target branch to fast-forward (default: the trunk() branch, else main)")
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
			remotes, err := forge.ResolveRemotes(ctx, jjClient, configMgr, openForkRemote, openUpstreamRemote)
			if err != nil {
				return err
			}
			// Detect forge type and adjust remotes before pushing
			forgeClient, upstreamRemoteURL, err := getForge(ctx, jjClient, remotes.Upstream)
			if err != nil {
				return err
			}
			// For forges without fork support, use upstream as fork remote
			if !forgeClient.SupportsForks() {
				remotes.Fork = remotes.Upstream
			}
			// Phase 3: Push
			// If no trailers were updated, commit IDs haven't changed — reuse resolved revs.
			var preResolved []*jj.Rev
			if trailerResult.TrailersUpdated == 0 {
				preResolved = trailerResult.Revs
			}
			pushResult, err := change.Push(ctx, jjClient, revset, remotes.Fork, stdoutUI, preResolved)
			if err != nil {
				return err
			}
			// Re-submit content for changes that already have open reviews on
			// forges that snapshot patches (no-op for branch-tracking forges).
			synced, err := review.SyncReviews(ctx, forgeClient, configMgr, upstreamRemoteURL, pushResult.PushedIDs, nil)
			if err != nil {
				return err
			}
			if synced > 0 {
				fmt.Fprintf(stdoutUI, "Synced content for %d existing review(s)\n", synced)
			}
			// Print upload summary
			skipped := trailerResult.SkippedEmpty + trailerResult.SkippedAnonymous + trailerResult.SkippedImmutable + pushResult.SkippedSynced
			if pushResult.Pushed > 0 || trailerResult.TrailersUpdated > 0 {
				fmt.Fprintf(stdoutUI, "Pushed %d change(s), updated %d trailer(s)\n", pushResult.Pushed, trailerResult.TrailersUpdated)
			}
			if skipped > 0 {
				fmt.Fprintf(stdoutUI, "Skipped %d change(s) (empty: %d, anonymous: %d, immutable: %d, synced: %d)\n",
					skipped, trailerResult.SkippedEmpty, trailerResult.SkippedAnonymous, trailerResult.SkippedImmutable, pushResult.SkippedSynced)
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
					UpstreamRemote:    remotes.Upstream,
					UpstreamRemoteURL: upstreamRemoteURL,
					ForkRemote:        remotes.Fork,
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
					stdoutUI.Styled("review_number", "#"+result.ID),
					stdoutUI.Styled("change_id", result.ChangeID))
				fmt.Fprintf(stdoutUI, "URL: %s\n", stdoutUI.Styled("url", result.URL))
				opened++
			}
			fmt.Fprintf(stdoutUI, "Opened %d review(s), skipped %d\n", opened, skipped)
			// Update PR descriptions with parent/child links
			if opened > 0 {
				prsUpdated, err := review.UpdatePRLinks(ctx, jjClient, forgeClient, configMgr, review.UpdatePRLinksParams{
					Revset:            revset,
					UpstreamRemote:    remotes.Upstream,
					UpstreamRemoteURL: upstreamRemoteURL,
				})
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
	openCmd.Flags().StringVar(&openUpstreamRemote, "upstream-remote", "", "Remote to create PR against (default: the trunk() remote, else the fork remote)")
	openCmd.Flags().StringVar(&openForkRemote, "fork-remote", "", "Remote where the branch is pushed (default: git.push, else the only remote, else og)")
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
			remotes, err := forge.ResolveRemotes(ctx, jjClient, configMgr, mergeForkRemote, mergeUpstreamRemote)
			if err != nil {
				return err
			}
			forgeClient, upstreamRemoteURL, err := getForge(ctx, jjClient, remotes.Upstream)
			if err != nil {
				return err
			}
			if !forgeClient.SupportsForks() {
				remotes.Fork = remotes.Upstream
			}
			// Execute merge command
			mergeParams := review.MergeParams{
				Rev:               rev,
				ForkRemote:        remotes.Fork,
				UpstreamRemote:    remotes.Upstream,
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
					ForkRemote:        remotes.Fork,
					UpstreamRemote:    remotes.Upstream,
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
				stdoutUI.Styled("review_number", "#"+result.ID),
				stdoutUI.Styled("change_id", result.ChangeID))
			return nil
		},
	}
	mergeCmd.Flags().StringVar(&mergeForkRemote, "fork-remote", "", "Remote of fork (default: git.push, else the only remote, else og)")
	mergeCmd.Flags().StringVar(&mergeUpstreamRemote, "upstream-remote", "", "Remote of upstream (default: the trunk() remote, else the fork remote)")
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
			remotes, err := forge.ResolveRemotes(ctx, jjClient, configMgr, closeForkRemote, closeUpstreamRemote)
			if err != nil {
				return err
			}
			forgeClient, upstreamRemoteURL, err := getForge(ctx, jjClient, remotes.Upstream)
			if err != nil {
				return err
			}
			if !forgeClient.SupportsForks() {
				remotes.Fork = remotes.Upstream
			}
			// Execute close command
			result, err := review.Close(ctx, jjClient, forgeClient, configMgr, review.CloseParams{
				Rev:               rev,
				ForkRemote:        remotes.Fork,
				UpstreamRemote:    remotes.Upstream,
				UpstreamRemoteURL: upstreamRemoteURL,
				Force:             closeForce,
				NoCleanup:         closeNoCleanup,
				UI:                stdoutUI,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(stdoutUI, "Closed review %s and abandoned change %s\n",
				stdoutUI.Styled("review_number", "#"+result.ID),
				stdoutUI.Styled("change_id", result.ChangeID))
			return nil
		},
	}
	closeCmd.Flags().StringVar(&closeForkRemote, "fork-remote", "", "Remote to use (default: git.push, else the only remote, else og)")
	closeCmd.Flags().StringVar(&closeUpstreamRemote, "upstream-remote", "", "Remote of upstream (default: the trunk() remote, else the fork remote)")
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
			remotes, err := forge.ResolveRemotes(ctx, jjClient, configMgr, "", importUpstreamRemote)
			if err != nil {
				return err
			}
			forgeClient, upstreamRemoteURL, err := getForge(ctx, jjClient, remotes.Upstream)
			if err != nil {
				return err
			}
			result, err := review.Import(ctx, jjClient, forgeClient, configMgr, review.ImportParams{
				Revset:            revset,
				UpstreamRemote:    remotes.Upstream,
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
	importCmd.Flags().StringVar(&importUpstreamRemote, "upstream-remote", "", "Remote to search for PRs (default: the trunk() remote, else the fork remote)")
	importCmd.Flags().BoolVar(&importAll, "all", false, "Check all mutable revisions")

	var updateUpstreamRemote, updateForkRemote string
	var updateSkipCheck bool
	updateCmd := &cobra.Command{
		Use:   "update [REVSET]",
		Short: "Upload content and update PR descriptions with parent/child links",
		Long: `Upload content and update PR descriptions with parent/child links.

On Tangled, where a pull request holds a snapshot of the patch rather than
tracking its branch, update also submits a new round for each open review
whose branch it pushed. Pushing by other means leaves those reviews stale.`,
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
			remotes, err := forge.ResolveRemotes(ctx, jjClient, configMgr, updateForkRemote, updateUpstreamRemote)
			if err != nil {
				return err
			}
			forgeClient, upstreamRemoteURL, err := getForge(ctx, jjClient, remotes.Upstream)
			if err != nil {
				return err
			}
			if !forgeClient.SupportsForks() {
				remotes.Fork = remotes.Upstream
			}
			result, err := review.Update(ctx, jjClient, forgeClient, configMgr, review.UpdateParams{
				Revset:            revset,
				ForkRemote:        remotes.Fork,
				UpstreamRemote:    remotes.Upstream,
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
				fmt.Fprintf(stdoutUI, "Skipped %d change(s) (empty: %d, anonymous: %d, immutable: %d, synced: %d)\n",
					ur.Skipped, ur.SkippedEmpty, ur.SkippedAnonymous, ur.SkippedImmutable, ur.SkippedSynced)
			}
			if result.ReviewsSynced > 0 {
				fmt.Fprintf(stdoutUI, "Synced content for %d review(s)\n", result.ReviewsSynced)
			}
			if result.PRsUpdated > 0 {
				fmt.Fprintf(stdoutUI, "Updated %d PR description(s)\n", result.PRsUpdated)
			}
			return nil
		},
	}
	updateCmd.Flags().StringVar(&updateForkRemote, "fork-remote", "", "Remote where the branch is pushed (default: git.push, else the only remote, else og)")
	updateCmd.Flags().StringVar(&updateUpstreamRemote, "upstream-remote", "", "Remote to update PRs on (default: the trunk() remote, else the fork remote)")
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
	var cloneTrackBranches []string
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

Tangled repositories (tangled.org) are detected automatically. Ownership is
checked against the tg login. Repositories you do not own are configured for
branch-based PRs in the same repository (fork-based PRs are not supported).

Examples:
  jj-forge repo clone git@github.com:me/my-project.git
  jj-forge repo clone https://github.com/external/project.git
  jj-forge repo clone git@github.com:owner/repo.git custom-dir
  jj-forge repo clone git@tangled.org:me.example.com/my-project`,
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
				TrackBranches:  cloneTrackBranches,
			}
			jjClientForConfig := jj.NewClientWithExecutor("", newJJExecutor())
			configMgr := forge.NewConfigManager(jjClientForConfig)
			hosts, _ := configMgr.GetHosts()
			// Dispatch to SSM clone flow for SSM URLs
			forgeType, _ := forge.DetectForge(ctx, url, forge.DefaultHTTPClient(), hosts)
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
			// Dispatch to Tangled clone flow for Tangled URLs
			if forgeType == forge.ForgeTypeTangled {
				tgCmd, _ := configMgr.GetToolCommand("tg")
				cli := tangled.NewCLI(newTGExecutor()).WithCommand(tgCmd)
				runner := repoclone.NewTangledRunnerWithDeps(cli, cmdpkg.DefaultExecutor, newJJExecutor(), stdoutUI)
				_, err := runner.Run(ctx, params)
				return err
			}
			ghCmd, _ := configMgr.GetToolCommand("gh")
			ghClient := repoclone.NewGitHubClientWithExecutor(newGHExecutor())
			if ghCmd != "" {
				ghClient.WithGHCommand(ghCmd)
			}
			runner := repoclone.NewRunnerWithDeps(ghClient, newJJExecutor(), &cmdpkg.DefaultPrompter{}, stdoutUI)
			_, err := runner.Run(ctx, params)
			return err
		},
	}
	cloneCmd.Flags().StringVar(&cloneForkRemote, "fork-remote", "og", "Name for fork/personal remote")
	cloneCmd.Flags().StringVar(&cloneUpstreamRemote, "upstream-remote", "up", "Name for upstream remote")
	cloneCmd.Flags().BoolVar(&cloneUseHTTPS, "https", false, "Use HTTPS instead of SSH for remotes")
	cloneCmd.Flags().BoolVar(&cloneNoFork, "no-fork", false, "Don't create fork for external repos (fail instead)")
	cloneCmd.Flags().StringArrayVar(&cloneTrackBranches, "track-branches", []string{"push-*"}, "Glob patterns for branches to track from fork remote (repeatable)")

	var rulesetUpstreamRemote string
	setupRulesetCmd := &cobra.Command{
		Use:               "setup-ruleset",
		Short:             "Add a GitHub ruleset to prevent merging forge-parent commits (GitHub only)",
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			jjClient := jj.NewClientWithExecutor(repoPath, newJJExecutor())
			remotes, err := forge.ResolveRemotes(ctx, jjClient, forge.NewConfigManager(jjClient), "", rulesetUpstreamRemote)
			if err != nil {
				return err
			}
			forgeClient, upstreamURL, err := getForge(ctx, jjClient, remotes.Upstream)
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
	setupRulesetCmd.Flags().StringVar(&rulesetUpstreamRemote, "upstream-remote", "", "Remote to target (default: the trunk() remote, else the fork remote)")

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

	// Util command group
	utilCmd := &cobra.Command{
		Use:   "util",
		Short: "Building blocks for scripts and check commands",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	// Exit statuses of util actions-status, besides 0 when the checks passed
	// or none run.
	const (
		actionsStatusFailed      = 1
		actionsStatusError       = 2
		actionsStatusPending     = 75 // EX_TEMPFAIL
		actionsStatusInterrupted = 130
	)
	var actionsStatusForkRemote, actionsStatusUpstreamRemote string
	var actionsStatusWait bool
	var actionsStatusTimeout time.Duration
	actionsStatusCmd := &cobra.Command{
		Use:   "actions-status [REV]",
		Short: "Report whether GitHub Actions passed on a change's pushed commit",
		Long: `Report whether GitHub Actions passed on the pushed commit of a change's pull request.

Exit status:
  0    the workflows and any other checks passed, or no workflow runs for the
       push and no other check reports
  1    a workflow or other check failed
  2    the checks could not be read, for example because the change is not
       pushed, the pull request has merge conflicts or its branch has moved
  75   the checks have not finished, which includes the time before GitHub
       registers the latest push
  130  interrupted

GitHub records nothing for a push that every workflow's filters skip, so the
workflow files are evaluated to tell which runs the push starts: their
pull_request, pull_request_target and push triggers, activity types, branch
and path filters, and skip instructions such as [skip ci] in the commit
message. The checks have not finished until each of those runs has started.
When in doubt, a workflow is expected to run.

With --wait, polls until the checks pass, fail or are found not to run, for
at most --timeout.`,
		Args:              withExitCode(actionsStatusError, cobra.MaximumNArgs(1)),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: withExitCode(actionsStatusError, func(cmd *cobra.Command, args []string) error {
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
			remotes, err := forge.ResolveRemotes(ctx, jjClient, configMgr, actionsStatusForkRemote, actionsStatusUpstreamRemote)
			if err != nil {
				return err
			}
			forgeClient, upstreamRemoteURL, err := getForge(ctx, jjClient, remotes.Upstream)
			if err != nil {
				return err
			}
			gh, ok := forgeClient.(*github.Client)
			if !ok {
				return &ui.UserError{Msg: "actions-status only works with GitHub"}
			}
			switch {
			case actionsStatusTimeout < 0:
				return &ui.UserError{Msg: "--timeout must not be negative"}
			case cmd.Flags().Changed("timeout") && !actionsStatusWait:
				return &ui.UserError{Msg: "--timeout only applies with --wait"}
			}
			pushed, err := review.FindPushedReview(ctx, jjClient, forgeClient, configMgr, rev, remotes.Fork)
			if err != nil {
				return err
			}
			var poll time.Duration
			if actionsStatusWait {
				poll = 15 * time.Second
			}
			status, err := actionsstatus.Wait(ctx, gh, upstreamRemoteURL, pushed.ReviewID, pushed.CommitID, poll, actionsStatusTimeout, stderrUI)
			if errors.Is(err, context.Canceled) {
				return &ui.ExitError{Err: errors.New("interrupted while waiting for checks"), Code: actionsStatusInterrupted}
			}
			if err != nil {
				return err
			}
			switch status.Verdict {
			case actionsstatus.Failed:
				return &ui.UserError{Msg: "checks failed", Details: status.Detail, ExitCode: actionsStatusFailed}
			case actionsstatus.Running:
				return &ui.UserError{Msg: "checks have not finished", Details: status.Detail, ExitCode: actionsStatusPending}
			}
			msg := "Checks passed"
			if status.Verdict == actionsstatus.NoChecks {
				msg = "No checks run on this push"
			}
			if status.Detail != "" {
				msg += ": " + status.Detail
			}
			fmt.Fprintln(stdoutUI, msg)
			return nil
		}),
	}
	actionsStatusCmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &ui.ExitError{Err: err, Code: actionsStatusError}
	})
	actionsStatusCmd.Flags().StringVar(&actionsStatusForkRemote, "fork-remote", "", "Remote where the branch is pushed (default: git.push, else the only remote, else og)")
	actionsStatusCmd.Flags().StringVar(&actionsStatusUpstreamRemote, "upstream-remote", "", "Remote the review is on (default: the trunk() remote, else the fork remote)")
	actionsStatusCmd.Flags().BoolVar(&actionsStatusWait, "wait", false, "Poll until the checks pass, fail or are found not to run")
	actionsStatusCmd.Flags().DurationVar(&actionsStatusTimeout, "timeout", 15*time.Minute, "With --wait, give up after this long (0 waits forever)")

	utilCmd.AddCommand(actionsStatusCmd)
	rootCmd.AddCommand(utilCmd)

	if err := rootCmd.Execute(); err != nil {
		if stderrUI == nil {
			stderrUI = ui.New(os.Stderr, ui.ColorAuto)
		}
		if !printUnknownCommandError(stderrUI, err) {
			stderrUI.PrintError(err)
		}
		var exitErr *ui.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		var userErr *ui.UserError
		if errors.As(err, &userErr) && userErr.ExitCode != 0 {
			os.Exit(userErr.ExitCode)
		}
		os.Exit(1)
	}
}

// withExitCode wraps a cobra RunE or Args function so that its errors exit
// with code, unless they carry an exit status of their own.
func withExitCode(code int, f func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		err := f(cmd, args)
		var exitErr *ui.ExitError
		var userErr *ui.UserError
		if err == nil || errors.As(err, &exitErr) || (errors.As(err, &userErr) && userErr.ExitCode != 0) {
			return err
		}
		return &ui.ExitError{Err: err, Code: code}
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
