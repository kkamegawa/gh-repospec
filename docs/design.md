# Design: gh-repospec

## Purpose

`gh-repospec` is a precompiled GitHub CLI extension that converges a
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

The executable entry point in `cmd/gh-repospec` only wires standard
streams into the command implementation in `internal/cli`. Product packages
are covered by the root `go test ./...` and `go vet ./...` commands; the
generated APM dependency tree is isolated by `apm_modules/go.mod`.

## Configuration contract

The public document contract is `schemaVersion: 1`, described by
[`schema/repospec.schema.json`](../schema/repospec.schema.json).
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

`homepage` is optional and distinguishes an absent key from an empty value:
omitting it leaves the current value alone, while an explicit empty string
clears it. `description` needs the same distinction for the same reason —
`import` output has to converge against the repository it was read from, and
a field that cannot express "not managed" reports a change forever — but is
still a plain string in the current schema; making it tri-state like
`homepage` is tracked separately as
[Issue #22](https://github.com/kkamegawa/gh-repospec/issues/22).

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
`app` (slug), `orgAdmin` (`true`), or `customRole` (name), and every entry
requires a `bypassMode` of `always` or `pull_request`. Slugs and names are
resolved to the numeric actor IDs the rulesets API requires before any write;
an unresolvable actor blocks the plan rather than silently dropping the
bypass. A resolved `id:<number>` form is also accepted so that `import` output can be applied
again unchanged, which matters for private GitHub Apps whose slug the
authenticated user cannot resolve.

`rulesets[].enforcement` also accepts `evaluate`, GitHub's dry-run mode, in
which violations are recorded but not blocked. It is the safe way to introduce
a ruleset to a repository that already has traffic.

`actions.forkPullRequestApproval` selects when a workflow run from a fork
requires manual approval: `first_time_contributors_new_to_github`,
`first_time_contributors`, or `all_external_contributors`, matching the
enumeration style already used by `allowedActions`. GitHub rejects this
endpoint outright on a private repository; declaring it alongside
`repository.visibility: private` is reported as blocked rather than
attempted.

`actions.workflowAccessLevel` (`none`, `user`, or `organization`) controls
which other repositories may use this repository's workflows. GitHub applies
that access policy only to private and internal repositories and answers a
read or write for a public one with HTTP 422, the inverse of the fork approval
case. `import` therefore reads it only from a private or internal repository
and reports a diagnostic for a public one. `plan` evaluates the declared
`repository.visibility`: a declaration alongside `public` is reported as
blocked without attempting the read, and a repository this run changes from
public to `private` is planned as an update applied after the visibility
change.

`actions.oidcSubjectCustomization` sets the repository's Actions OIDC subject
claim template. Its `useImmutableSubject` selects the immutable claim format,
which carries the owner and repository IDs. GitHub enables that format for
every repository created or renamed after 2026-07-15 and accepts a write that
turns it off without changing the value, so a declared `false` against a live
`true` can never converge. `plan` reports that difference as a separate
manual entry, `actions.oidcSubjectCustomization.useImmutableSubject`, with the
reason; the rest of the customization converges as usual, and `apply` writes
it without the field. A declared `true` against a live `false` is an ordinary
update. This keeps a configuration imported from an older repository
applicable to a new one instead of reporting an update on every run.

`security.dependabotSecurityUpdates` requires Dependabot alerts to be
effectively enabled, because the GitHub API rejects it otherwise. The
dependency is evaluated during `plan` against both the configuration and the
live repository: enabling both in the same file is ordered so alerts are
applied first, omitting `dependabotAlerts` falls back to the current state on
GitHub, and an explicit `dependabotAlerts.enabled: false` alongside enabled
security updates is reported as blocked.

`security.aiPullRequestScan` controls AI Scan for pull requests, GitHub's
AI-powered security detection for the languages and frameworks CodeQL does not
cover. Its API is in public preview and may change. The setting is
repository-scoped: the organization-level setting, which can prevent a
repository from enabling AI Scan, is not managed, and a write GitHub rejects
because of it fails like any other write. Enabling it requires CodeQL default
setup to be effectively enabled, evaluated the same way as the Dependabot
dependency above: a declared `codeqlDefaultSetup` takes precedence and is
applied first, an omitted one falls back to the current state on GitHub, and
an explicit `codeqlDefaultSetup.enabled: false` alongside an enabled AI Scan is
reported as blocked. Disabling it has no dependency. AI Scan also needs GitHub
Advanced Security, a Copilot license, and enablement by the enterprise and
organization, none of which the tool can verify before writing, so it is best
declared with `optional: true` wherever those may be missing.

Code scanning itself can be unavailable: a private repository without GitHub
Advanced Security, such as an empty personal repository about to be
bootstrapped, answers the CodeQL default setup and AI Scan reads with 403. `plan`
treats that answer as the feature being unavailable rather than its state being
unknown, so `codeqlDefaultSetup` and `aiPullRequestScan` are reported as
blocked, or as manual when `optional: true`, with GitHub's message as the
reason, and `apply` does not attempt the write. Reporting them as updates would
make an optional declaration drift forever, because no `apply` can change it.
A 403 caused by missing permissions takes the same path; a required declaration
is still blocked and stops the run. Once code scanning is enabled for the
repository, the next plan reads the state normally.

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
success — for `plan --ci`, that the plan has no blocked entry and no
difference the tool can converge. It is not a claim that every setting matches,
because secret updates and manual entries are excluded from that judgement.
`1` means the command failed, which includes a blocked plan under `--ci`: a
blocked plan is a failure and outranks drift. `2` means `plan --ci` found at
least one difference it can converge. `apply --force-secrets` treats every
configured secret as `updatePolicy: always` for that run, which is how a
credential rotation is applied without editing the file.

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

`repository.archived` sits outside that ordering because GitHub rejects writes
to an archived repository. `archived: false` is applied first, before any
other change, so the rest of `apply` can run against a writable repository.
`archived: true` is applied last of all, after rulesets, so archiving never
blocks a change this run was also supposed to make.

## import

`import <owner/repository> [--format yaml|json] [--into <path> [--yes]]`
fetches an existing repository and produces a `schemaVersion: 1` document.
`-R`/`--repo` substitutes for the positional repository, but the two forms
cannot be combined. YAML is the default stdout format; JSON is available for
automation. Diagnostics go to standard error, so a redirected document stays
valid. Import only reads GitHub and never makes a repository write.

The importer maps `repository` (topics, features, merge settings, and archived
state), `labels`, `milestones`, `rulesets` and bypass actors, Actions
permissions, variables, and environment names, `security`, and agent
variables. Several of those reads do not exist in this extension yet;
[`plan.md`](plan.md) enumerates them. Two shape the contract. The rulesets list
endpoint returns only identity, target, and enforcement, so each ruleset is
fetched individually to obtain its rules, conditions, and bypass actors.
Security state comes from `security_and_analysis` on the repository plus the
per-feature endpoints; a feature whose state cannot be read is reported rather
than written as `false`. AI Scan is the case where this matters most: GitHub
answers its read with 403 when Advanced Security is not enabled, and writing
that as `enabled: false` would produce a file that never converges.

Server-generated ruleset IDs and timestamps are omitted, because the ruleset
name is the stable identity. Bypass actors are reversed by actor type:
`Integration` becomes `app`, actor ID 1 becomes `orgAdmin`, and
`RepositoryRole` becomes the symbolic `role` for the fixed IDs 2, 4, and 5
(`maintain`, `write`, `admin`) and `customRole: id:<number>` for any other ID,
because both share that actor type. Teams, apps, and custom roles use the
existing `id:<number>` selector; `role` never does. An empty bypass actor list
is written out explicitly, since omitting it clears the actors on the next
apply. Rule parameters are emitted with sorted keys and integral numbers, so
the same repository always produces the same bytes. Inherited or otherwise
unrepresentable rulesets are reported rather than silently reduced.

Secret values and `sourceEnv` names cannot be read from GitHub. Secret names
are reported as YAML comments and JSON diagnostics so the user can supply a
source environment variable. Local intent is never inferred: `seed` and its
`overwrite` list, `commands`, agent file paths, `agents.secrets`,
`repository.create`, and `repository.allowVisibilityChange` describe decisions
that no repository state implies. A refresh preserves all of them, along with
secret update policies and `optional` flags.

`--into <path>` refreshes only paths and top-level sections already declared in
the file. Within a declared collection, entries that exist on GitHub are
updated and entries missing from the file are added in name order; a declared
entry that does not exist on GitHub is kept and reported as not yet applied,
never deleted. A configuration entry is a desired state that may simply not
have been applied, and dropping it would silently accept drift — the same
additive rule that governs the rest of the contract. Undeclared sections stay
undeclared, an empty declared collection stays declared, and retained entry
order with attached comments is preserved. A `security` feature whose live
state could not be read keeps its existing declaration and is reported.

YAML refresh parses the file into an AST that carries a byte offset for every
token and replaces only the spans that changed, so comments, key order,
indentation, quote style, and line endings outside those spans survive
untouched. Start-of-node line and column numbers alone are not enough: they
give no span end, and a parser that normalizes CRLF to LF cannot preserve the
original line endings. `config.Load` keeps its own strict parser, and the
refresh parser is confined to rewriting; the two must not diverge on what a
valid document is. JSON is reformatted as JSON while retaining JSON input.

The proposed difference is shown and confirmed before saving, with `--yes`
skipping confirmation. The candidate document is validated before it is
written, but that validation excludes the filesystem checks that `seed` paths
normally receive, so a refresh does not fail because a seed directory is
absent on the machine running it. A no-op, validation error, declined
confirmation, concurrent edit, or save error leaves the original file
unchanged. An input format and an explicit `--format` value must agree when
`--into` is used.

## Drift detection and CI

`plan --ci` is the drift check. It exits `2` when the plan contains a create or
update the tool can converge, which turns a scheduled workflow into a pass/fail
signal for settings that were altered through the GitHub UI. A blocked plan
exits `1`, because a blocked plan is a failure and outranks detection. `plan`
without `--ci` exits `0` in every case.

Secret updates are excluded from the comparison. GitHub never returns a secret
value, so a declared secret whose source environment variable is set is
reported as an update on every run and would otherwise leave a scheduled check
permanently red; a secret rotated outside the tool is undetectable by
construction, and `apply --force-secrets` is the answer for rotation. A secret
whose source environment variable is not set is a different case: it is
reported as blocked rather than as an update, the exclusion does not reach it,
and `plan --ci` exits `1`.

| Situation | `plan` | `plan --ci` |
|---|---|---|
| No changes | 0 | 0 |
| Only secret updates, with their source environment variables set | 0 | 0 |
| Only manual entries (seed, commands, agent files) | 0 | 0 |
| Create or update present | 0 | 2 |
| Blocked present | 0 | 1 |
| Blocked and drift present | 0 | 1 |

`--format json` produces the same exit code, and the drift notice is written to
standard error, so a redirected report stays a valid document at every exit
code and can be archived or posted while the exit code drives the job result.

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
output, timeout, and an environment allowlist. The allowlist is exact: `PATH`
and the home directory variable (`HOME`, or `USERPROFILE` on Windows) are not
inherited implicitly, so a program that starts other programs or reads its
home directory needs them listed. Only `SystemRoot` is always passed on
Windows, because processes cannot start reliably without it. When a command
fails, times out, or exceeds its output limit, the error includes its captured
output, with the value of every declared secret source environment variable
replaced by `***`.

## Relationship to gh-infra

[`gh-infra-parity.md`](gh-infra-parity.md) records the feature comparison with
`babarot/gh-infra`, the capabilities adopted above, and the capabilities
deliberately left out — multi-repository sets, continuous file synchronization,
authoritative deletion of undeclared resources, and classic branch protection —
with the reason for each.
