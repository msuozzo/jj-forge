# jj-forge

A translation layer between [Jujutsu (jj)](https://github.com/jj-vcs/jj) and code forges like GitHub.

## Highlights

- **Reviews with clickable links** -- Review references like GitHub PRs can be added
  directly to the `jj log` output and include clickable terminal hyperlinks.

- **Local, parallel checks** -- Run a configurable check command against multiple
  changes in parallel, with result caching and `jj log` integration. Supports
  `--detach` for asynchronous background execution. Checks run automatically
  before review operations like `open` and `merge`.

- **Stacked merge protection** -- Configures ruleset to prevent accidental
  merges of stacked reviews.

- **Forge-independent model** -- Commands and APIs not specific to a forge.
  While GitHub is the first supported platform, the underlying design is
  generic.

- **Familiar jj UX** -- Uses revsets to target changes and jj's clap-style cli
  look and feel.

## Prerequisites

- [jj](https://github.com/jj-vcs/jj) (Jujutsu VCS)
- [Go](https://go.dev/) 1.24+
- [gh](https://cli.github.com/) (GitHub CLI, authenticated) -- required for the GitHub forge

## Installation

Install the latest version and use as a jj subcommand (`jj forge ...`):

    go install github.com/msuozzo/jj-forge/cmd/jj-forge@latest
    jj config set --user aliases.forge '["util", "exec", "--", "jj-forge"]'

Or, to skip the install step and always run the latest version:

    jj config set --user aliases.forge '["util", "exec", "--", "go", "run", "github.com/msuozzo/jj-forge/cmd/jj-forge@latest"]'

## Getting started

### Clone a repository

`repo clone` analyzes repository ownership and configures remotes and workflow
automatically:

    jj forge repo clone https://github.com/owner/repo.git

Workflow is detected based on ownership:

- **Your non-fork repo** -- develop-on-main workflow
- **Your fork** -- PR-based workflow
- **External repo** -- forks automatically, then PR-based workflow

### Develop-on-main workflow

    jj describe -m "my change"
    jj forge change submit             # fast-forward main

### PR-based workflow

    jj describe -m "my change"
    jj forge review open               # upload and create pull request
    # ... edits happen ...
    jj forge review update             # push content and update PR descriptions
    # ... review completed ...
    jj forge review merge              # merge pull request

## Checks

Configure a check command (any shell command) to run against your changes:

    jj config set --repo forge.check-command "go test ./..."

Run checks manually:

    jj forge change check         # check the current change
    jj forge change check ::@-    # check a range of changes

Checks run in parallel: Each change is materialized into a persistent pool
of working directories (default: 3 workers) using the backing git store.
Results are cached by change ID and commit ID, so subsequent execution will
reuse the last result.

### Background execution

Use `--detach` to run checks in the background:

    jj forge change check --detach

### Automatic checks

When a check command is configured, `review open`, `review update`,
`change submit`, and `review merge` run checks before proceeding. Pass
`--skip-check` to bypass.

## Templates

jj-forge stores review and check data in the jj repo config. You can display
this data in `jj log` using template-aliases.

Set all template-aliases automatically:

    jj forge repo setup-templates        # set in repo config
    jj forge repo setup-templates --user # set in user config

Or, copy [`examples/templates.toml`](examples/templates.toml) into your jj
config manually (`jj config edit --user`). The key formatters are:

```toml
"format_forge_check(commit)" = '''
if(!immutable && forge_check_exists(change_id, commit_id),
  if(forge_check_matches(change_id, commit_id, "pass"), label("hint", "ci/√"),
  if(forge_check_matches(change_id, commit_id, "running"), label("description placeholder", "ci/~"),
  if(forge_check_matches(change_id, commit_id, "fail"), label("error heading", "ci/X")))))
'''
"format_forge_change(commit)" = '''
if(!immutable && forge_change_exists(change_id), label("error", hyperlink(forge_change_uri(change_id), forge_change_name(change_id))))
'''
```

Then include them in your log template:

```diff
 "format_short_commit_header(commit)" = '''
 separate(" ",
   format_short_change_id_with_change_offset(commit),
+  format_forge_change(commit),
+  format_forge_check(commit),
   format_short_signature(commit.author()),
   format_timestamp(commit_timestamp(commit)),
   commit.bookmarks(),
   commit.tags(),
   commit.working_copies(),
   format_short_commit_id(commit.commit_id()),
   format_commit_labels(commit),
   if(config("ui.show-cryptographic-signatures").as_boolean(),
     format_short_cryptographic_signature(commit.signature())
   ),
 )
 '''
```

This adds:

- **`format_forge_change`** -- a clickable hyperlink to the PR (e.g., `pr/42`)
- **`format_forge_check`** -- check status indicator: `ci/√` (pass), `ci/~` (running), `ci/X` (fail)

> **Note:** `watch` does not support hyperlinking in output. Consider using
> [`hwatch`](https://github.com/blacknon/hwatch) which supports OSC 52
> passthrough.

## Stacked Reviews

jj-forge tracks parent-child relationships between changes using a
`forge-parent` trailer in commit descriptions. When you upload a stack of
changes, `review open` and `review update` automatically add or update these
trailers.

To prevent accidentally merging commits that still contain `forge-parent`
trailers, you can add a GitHub branch ruleset:

    jj forge repo setup-ruleset

### Dependent Review Links

When working with stacked reviews, jj-forge will manage **Parents** and
**Children** links in descriptions so reviewers can navigate between related
reviews. For example, the middle PR in a three-PR stack would have:

```
> Parents: [#1](https://github.com/owner/repo/pull/1)
> Children: [#3](https://github.com/owner/repo/pull/3)
```

`review open` will initialize them, `review update` will keep them synchronized
across modifications, and `review merge` will clean them up prior to
finalization.

## Commands

| Command                   | Description                                                  |
| ------------------------- | ------------------------------------------------------------ |
| `change check [REVSET]`   | Run the configured check command against changes             |
| `change submit REVSET`    | Land changes directly by fast-forwarding the target branch   |
| `review open [REVSET]`    | Create a pull request                                        |
| `review update [REVSET]`  | Upload content and update PR descriptions with links         |
| `review merge [REV]`      | Merge a pull request                                         |
| `review close [REV]`      | Close a pull request and abandon the change                  |
| `review import [REV]`     | Find and import existing pull requests                       |
| `repo clone <url> [path]` | Clone repository with automatic workflow detection           |
| `repo setup-ruleset`      | Add a GitHub ruleset to prevent merging forge-parent commits |
| `repo setup-templates`    | Set template-aliases in jj config for forge visualization    |

Run `jj forge <command> --help` for flags and details.

## Configuration

| Key                      | Description                                 |
| ------------------------ | ------------------------------------------- |
| `forge.check-command`    | Shell command to run for checks             |
| `forge.default-reviewer` | Default reviewer username for `review open` |
| `forge.hosts`            | Map of custom git hosts to forge types      |
| `forge.tools`            | Map of forge CLI tool command overrides     |

Set values with:

    jj config set --repo forge.check-command "go test ./..."
    jj config set --repo forge.default-reviewer "username"
    jj config set --user 'forge.hosts."github.example.com"' "github"
    jj config set --repo forge.tools.gh "custom-gh"
