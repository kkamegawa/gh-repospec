# Design: gh-repobootstrapper

## Purpose

`gh-repobootstrapper` is a precompiled GitHub CLI extension that converges a
GitHub.com repository on a declarative bootstrap file. Version 1 targets
personal repositories and Free, Pro, and Team organizations. GitHub Enterprise
Server is rejected; Enterprise Cloud is not a supported target. The effective
host is resolved with go-gh's `auth.DefaultHost`, which mirrors the GitHub CLI's
own precedence (`GH_HOST`, then a single configured host, then github.com) and
is rejected with `auth.IsEnterprise` before any request is made; a raw `GH_HOST`
check alone misses a login against a single Enterprise Server host with
`GH_HOST` unset.

The extension uses the authenticated GitHub CLI session. It never stores or
prints a GitHub token. Repository changes are declarative and additive: fields,
files, and rulesets omitted from configuration are not removed.

The executable entry point in `cmd/gh-repobootstrapper` only wires standard
streams into the command implementation in `internal/cli`. Product packages
are covered by the root `go test ./...` and `go vet ./...` commands; the
generated APM dependency tree is isolated by `apm_modules/go.mod`.

## Configuration contract

The public document contract is `schemaVersion: 1`, described by
[`schema/repobootstrapper.schema.json`](../schema/repobootstrapper.schema.json).
YAML and JSON are accepted. Parsing is strict: unknown fields, duplicate mapping
keys, unsupported enum values, and multiple documents are rejected. Environment
variables are not interpolated.

The top-level sections are `repository`, `seed`, `actions`, `rulesets`,
`security`, `agents`, `labels`, `milestones`, and `commands`. Repository
visibility is only changed when `allowVisibilityChange` is true. A seed selects exactly one template,
directory, or ZIP source. Existing divergent files require their
repository-relative path in `seed.overwrite`.

Secrets have a `sourceEnv`, not a value. The default `updatePolicy` is
`createOnly`; `always` explicitly rotates it. Commands use a program and argv
array. Shell programs are rejected, paths are repository-relative, and timeout
uses Go duration syntax such as `5m`.

`repository` also carries the settings that describe the repository rather
than its contents: `homepage`, `archived`, the `features` toggles for issues,
projects, wiki, and discussions, the merge commit title and message formats
(`mergeCommitTitle`, `mergeCommitMessage`, `squashMergeCommitTitle`,
`squashMergeCommitMessage`), and `releaseImmutability`. Archiving is
reversible: `archived: false` unarchives a repository.

`labels` and `milestones` are additive collections, consistent with the rest
of the contract. Labels take a `name`, a `color` as six hexadecimal digits
without a leading `#`, and an optional `description`. Milestones take a
`title`, an optional `description`, a `state` of `open` or `closed`, and an
optional `dueOn` date in `YYYY-MM-DD` form. Names and titles are unique within
their list and identify the resource on GitHub. Labels and milestones that
exist on the repository but are absent from configuration are never deleted; a
milestone is retired with `state: closed`.

`rulesets[].bypassActors` lists the actors allowed to bypass a ruleset. Each
entry sets exactly one of `role` (`admin`, `write`, `maintain`), `team` (slug),
`app` (slug), `orgAdmin` (`true`), or `customRole` (name), plus a `bypassMode`
of `always` or `pull_request`. Slugs and names are resolved to the numeric
actor IDs the rulesets API requires before any write; an unresolvable actor
blocks the plan rather than silently dropping the bypass. A resolved
`id:<number>` form is also accepted so that `import` output can be applied
again unchanged, which matters for private GitHub Apps whose slug the
authenticated user cannot resolve.

`rulesets[].enforcement` also accepts `evaluate`, GitHub's dry-run mode, in
which violations are recorded but not blocked. It is the safe way to introduce
a ruleset to a repository that already has traffic.

`actions.forkPullRequestApproval` selects when a workflow run from a fork
requires manual approval: `first_time_contributors_new_to_github`,
`first_time_contributors`, or `all_external_contributors`, matching the
enumeration style already used by `allowedActions`.

`security.dependabotSecurityUpdates` requires Dependabot alerts to be
effectively enabled, because the GitHub API rejects it otherwise. The
dependency is evaluated during `plan` against both the configuration and the
live repository: enabling both in the same file is ordered so alerts are
applied first, omitting `dependabotAlerts` falls back to the current state on
GitHub, and an explicit `dependabotAlerts.enabled: false` alongside enabled
security updates is reported as blocked.

`actions.allowedActions: selected` requires `actions.selectedActions`
(`githubOwnedAllowed`, `verifiedAllowed`, and an optional `patternsAllowed`
list). GitHub's selected-actions allowlist otherwise defaults to empty, which
blocks every workflow run using an Action; validation rejects `selected`
without it, and rejects `selectedActions` when `allowedActions` is not
`selected`.

## Execution model

`validate` only parses and validates. `plan` produces a no-write change report;
`apply` recalculates that report, prompts unless `--yes` is present, then
applies the ordered changes. A command needs a separate
`--approve-command <id>` approval even with `--yes`. `import` reads a
repository and writes configuration; it performs no repository writes.
`version` (also `--version`/`-v`) prints the release version embedded at build
time with `-ldflags`, following GitHub CLI extension convention.

Exit codes are part of the contract, because CI branches on them. `0` means
success — for `plan`, that the requested state is already converged when
`--ci` is set. `1` means the command failed. `2` means `plan --ci` found at
least one difference. `apply --force-secrets` treats every configured secret
as `updatePolicy: always` for that run, which is how a credential rotation is
applied without editing the file.

Before writes, preflight checks the GitHub.com host, authenticated permissions,
repository state, feature availability, organization policy, seed source, and
required secret environment variables. Unsupported plan-dependent capabilities
may be skipped only if configured as `optional: true`; authorization failures
always stop the run.

Apply order is: repository creation or visibility; secret scanning and push
protection; files; default branch; repository and Actions settings, including
labels and milestones; secrets and variables; remaining security settings;
rulesets. Bypass actor identities are resolved before the ruleset stage runs,
so a name that cannot be resolved is reported by `plan` instead of failing
midway through `apply`. Rulesets are last so they do not block the initial
commit. The process is not transactional. A partial failure is reported and a
later apply converges the requested state.

## import

`import <owner/repository>` fetches the live state of an existing repository
and writes a `schemaVersion: 1` document, so an already-configured repository
can be brought under this extension without transcribing settings by hand. The
default output is the document on standard output; `--format` selects YAML or
JSON, and `-R`/`--repo` is accepted as an alternative to the positional
argument for consistency with the other commands.

Imported sections are `repository` (including topics, features, merge
settings, and archived state), `labels`, `milestones`, `rulesets` with their
bypass actors, `actions` permissions, variables, and environments, and
`security`. Server-generated values such as ruleset IDs and timestamps are not
written, because the ruleset name is the stable identity in this contract.

Three things are not importable and are reported, not silently omitted. Secret
values are never returned by the GitHub API, so secrets appear as a comment
listing the names found on the repository, and the user supplies each
`sourceEnv`. `seed` and `commands` describe local intent that no repository
state implies. `agents` file paths depend on the seed layout; agent variables
are imported, agent secret names follow the same rule as Actions secrets.

`--into <path>` runs the same fetch against an existing configuration file and
updates it in place instead of writing a new document. The file is rewritten
through YAML node editing so comments, key order, and formatting survive; a
JSON configuration is reformatted as JSON. The differences are printed and
confirmed before the file is written, and `--yes` skips the prompt. Fields the
file does not declare stay undeclared: `--into` refreshes what is managed, it
does not expand the managed surface.

## Drift detection and CI

`plan --ci` is the drift check. It exits `2` when any change would be made,
which turns a scheduled workflow into a pass/fail signal for settings that were
altered through the GitHub UI. `--format json` produces the same exit code, so
the report can be archived or posted while the exit code drives the job result.

Two workflows cover the common setup, and
[`examples/workflows`](examples/workflows) contains both. Applying on merge to
the default branch keeps the live repository converged with the file, and a
scheduled `plan --ci` catches changes made outside the tool. An `apply`
workflow should use a `concurrency` group so two runs cannot interleave: this
extension has no state file and therefore no lock, and concurrent applies are
last-write-wins.

The default `GITHUB_TOKEN` can administer the repository the workflow runs in.
Bootstrapping a different repository needs a fine-grained personal access
token or a GitHub App installation token with Administration, Contents, and —
for secrets and variables — the corresponding read and write permissions.

## Sources, agents, and limits

New template repositories use GitHub's template-generation API. Directory and
ZIP sources are staged locally; ZIP extraction rejects path traversal, symbolic
links, `.git`, and size abuse. Changes are uploaded as one commit with the Git
Database API. Existing repositories use their current default-branch tree and
never force-update a ref.

Agent files such as `.github/mcp.json`, instructions, custom agents, skills,
and setup workflows are seed content. Repository settings for Copilot MCP
servers, Copilot licenses or seats, organization policies, and inherited
organization rulesets have no supported repository-level automation path and
are reported as manual work. Command processes are not OS-sandboxed; the tool
limits them to an approved executable, argv, repository workspace, bounded
output, timeout, and an environment allowlist.

## Relationship to gh-infra

[`gh-infra-parity.md`](gh-infra-parity.md) records the feature comparison with
`babarot/gh-infra`, the capabilities adopted above, and the capabilities
deliberately left out — multi-repository sets, continuous file synchronization,
authoritative deletion of undeclared resources, and classic branch protection —
with the reason for each.
