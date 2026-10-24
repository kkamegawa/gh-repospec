# Troubleshooting and FAQ

[日本語](troubleshooting_ja.md)

This page collects the errors and plan entries users hit most often, especially
when a configuration produced by `import` is applied to a different repository.
Each entry shows what you see, why it happens, and what to do. The settled
behavior behind each case is recorded in [design.md](design.md).

Before retrying any failed `apply`, run `plan` again. `apply` is not
transactional: changes written before the error stay applied, and later stages
(rulesets run last) were not reached. A fresh plan shows what is left.

## Applying a configuration to a different repository

`import` describes the repository it was read from. Applying that file to a
repository with a different visibility, plan, or creation date can hit GitHub
limits that did not exist on the source. Review these entries before reusing an
imported file, particularly when moving from a public to a private repository.

### HTTP 422 "Please ensure the billing plan supports the required reviewers protection rule"

```text
error: ensure Actions environment "copilot": PUT /repos/OWNER/REPO/environments/copilot
returned HTTP 422: Failed to create the environment protection rule. Please ensure the
billing plan supports the required reviewers protection rule.
```

**Cause.** GitHub Free, Pro, and Team plans support environment wait timers and
required reviewers only on public repositories. GitHub treats `wait_timer`,
`prevent_self_review`, and `reviewers` in the request as protection rule writes,
so a private repository rejects them even when the values only disable
protection. `import` writes `waitTimer: 0`, `preventSelfReview: false`, and
`reviewers: []` for an unprotected environment, so a file imported from a
public repository triggers this when applied to a private one.

**Fix.** Versions that include
[#51](https://github.com/kkamegawa/gh-repospec/pull/51) read the live
environment first and send a disabling value only when there is an existing rule
to clear. Update the extension with `gh extension upgrade repospec`.

On an older version, remove those three fields, and an empty
`customProtectionRules: []`, from the affected environment:

```yaml
environments:
  - name: copilot
    deploymentBranchPolicy:
      allowAllBranches: true
      protectedBranches: false
      customBranchPolicies: false
```

An omitted field means "not managed", not "disabled", so this does not clear
protection on an environment that already has it.

If you actually want reviewers or a wait timer on a private repository, the
error is a real plan limit: use GitHub Enterprise, or make the repository
public. The tool never drops a requested protection rule to make `apply`
succeed.

### `actions.forkPullRequestApproval` is blocked on a private repository

**Cause.** GitHub rejects the fork pull request approval endpoint on private
repositories with HTTP 422, so `plan` blocks the field when
`repository.visibility` is `private`.

**Fix.** Remove `actions.forkPullRequestApproval` from configurations for
private repositories.

### `actions.workflowAccessLevel` is blocked on a public repository

**Cause.** The inverse of the previous case. Workflow access applies only to
private and internal repositories, and GitHub answers HTTP 422 for a public
one. `import` reads it only from a private or internal repository.

**Fix.** Remove `actions.workflowAccessLevel` when the destination is public.

### `repository.visibility` is blocked with "allowVisibilityChange is required"

**Cause.** A visibility change is deliberately guarded. `import` never writes
`allowVisibilityChange`, because no repository state implies that you intend to
change visibility.

**Fix.** Confirm that the change is intended, then add
`repository.allowVisibilityChange: true`.

### CodeQL default setup or AI Scan is blocked, manual, or reported unavailable

**Cause.** A private repository without GitHub Advanced Security answers the
CodeQL default setup and AI Scan reads with 403, and AI Scan may answer 404.
AI Scan also needs CodeQL default setup, a Copilot license, and enterprise and
organization enablement.

**Fix.** Declare these features with `optional: true` when the destination may
not have Advanced Security. An optional enabling declaration is then reported as
manual instead of blocking the run. A declaration that disables an unavailable
feature is already satisfied and is a noop.

### `actions.oidcSubjectCustomization.useImmutableSubject` is a MANUAL entry

**Cause.** GitHub enables the immutable OIDC subject format for every
repository created or renamed after 2026-07-15 and silently ignores a write
that turns it off. A file imported from an older repository declares `false`,
which can never converge on a newer one.

**Fix.** Nothing is required: the rest of the OIDC customization still applies,
and the manual entry does not fail `apply` or `plan --ci`. Set the field to
`true`, or remove it, to make the entry disappear.

### `repository.hasDownloads` is blocked

**Cause.** GitHub's repository update API does not support changing
`has_downloads` on an existing repository. It is honored only when the
repository is created.

**Fix.** Remove the field, or match it to the live value.

### Custom deployment protection app is unavailable for the destination environment

**Cause.** A custom deployment protection rule refers to a GitHub App that is
not installed on, or not available to, the destination repository. Custom
protection rules are also limited to public repositories on Free, Pro, and Team
plans.

**Fix.** Install the app on the destination, or remove its slug from
`customProtectionRules`.

### Ruleset bypass actor cannot be resolved

**Cause.** Bypass actor slugs and names are resolved to numeric IDs before any
write. An app, team, or custom role that does not exist for the destination
owner, or a private app the authenticated user cannot see, blocks the plan
instead of silently dropping the bypass.

**Fix.** Use an actor that exists for the destination, or the resolved
`id:<number>` form for a private app.

## Inputs and local intent

### A secret is blocked with "source environment variable is not set"

**Cause.** Secret values are never stored in configuration and never returned by
GitHub. Each secret reads its value from the environment variable named in
`sourceEnv`. `import` reports secret names only, as comments or diagnostics.

**Fix.** Export the variable before running `plan` or `apply`, or remove the
secret from the file.

### A secret update never shows as converged

**Cause.** GitHub does not return secret values, so a secret whose source
variable is set is always an opaque update. It is excluded from `plan --ci`
drift detection.

**Fix.** This is expected. Use `updatePolicy: createOnly` to write it only once,
or `apply --force-secrets` to rotate every configured secret in one run.

### "seed conflicts with existing file"

**Cause.** A seed file would overwrite a different existing file in the
repository, which `apply` never does implicitly.

**Fix.** Add the path to `seed.overwrite` if replacing it is intended, or remove
the file from the seed.

### "cannot set default branch on an empty repository without seed files or commands"

**Cause.** GitHub cannot select a default branch until a commit creates it.

**Fix.** Add a `seed` or `commands`, or remove `repository.defaultBranch` until
the repository has content.

### A command is reported as "requires --approve-command"

**Cause.** Every declared command needs explicit approval, even with `--yes`.

**Fix.** Pass `--approve-command <id>` for each command you have reviewed.

## Validation and dependencies

### `allowedActions: selected` is rejected

**Cause.** Without `actions.selectedActions`, GitHub's allowlist defaults to
empty and every workflow that uses an Action would be blocked.

**Fix.** Add `selectedActions` with `githubOwnedAllowed`, `verifiedAllowed`,
and, if needed, `patternsAllowed`.

### Dependabot security updates or AI Scan is blocked by a dependency

**Cause.** Dependabot security updates require Dependabot alerts, and AI Scan
requires CodeQL default setup. An explicit `enabled: false` on the dependency
alongside the dependent feature is contradictory.

**Fix.** Enable the dependency, or omit it so the live state is used.

### Deployment branch patterns require `customBranchPolicies`

**Cause.** GitHub serves environment branch and tag patterns only while custom
branch policies are enabled.

**Fix.** Set `deploymentBranchPolicy.customBranchPolicies: true` on an
environment that declares `branchPolicies`.

## GitHub API responses

### A 403 is reported as a rate limit

**Cause.** GitHub marks some 403 and 429 answers as primary or secondary rate
limits. These say nothing about whether a feature is available, so `plan`
reports the state as unknown and `import` fails.

**Fix.** Wait for the limit to reset and run the command again.

### `plan --ci` exits with 1 or 2

**Cause.** `1` means the command failed, including a blocked plan. `2` means
`plan` found at least one difference that `apply` can converge. Manual entries
and opaque secret updates do not cause `2`.

**Fix.** Inspect the plan output: resolve blocked entries first, then apply the
remaining changes.
