# Implementation Plan: import and format-preserving refresh

## Summary

Issue [#11](https://github.com/kkamegawa/gh-repospec/issues/11) adds a read-only `import` command and an `--into` refresh flow. The work is split into state fetching and reverse mapping, source-preserving file editing, CLI and planner convergence, and one schema correction that convergence depends on. This document records the implementation plan; the current draft PR contains planning and design documentation only.

## Tracking

| Workstream | Issue |
|---|---|
| Parent | [#11](https://github.com/kkamegawa/gh-repospec/issues/11) |
| State fetching and reverse mapping | [#18](https://github.com/kkamegawa/gh-repospec/issues/18) |
| Format-preserving `--into` refresh | [#19](https://github.com/kkamegawa/gh-repospec/issues/19) |
| CLI and round-trip convergence | [#20](https://github.com/kkamegawa/gh-repospec/issues/20) |
| Tri-state `repository.description` | [#22](https://github.com/kkamegawa/gh-repospec/issues/22) |

## Reads that do not exist yet

`import` cannot be written against the current `internal/githubapi` surface. Eight reads are missing, and the pagination helper cannot decode two of the response shapes involved.

| Read | Current state |
|---|---|
| Repository environments | Only the create/update call exists. The response is a wrapped `{total_count, environments}` object. |
| Actions selected-actions allowlist | Write-only today. Required for `actions.selectedActions`. |
| Actions default workflow permissions | Write-only today. Required for `defaultWorkflowPermissions` and `canApprovePullRequestWorkflows`. |
| Automated security fixes | Write-only today. Required for `security.dependabotSecurityUpdates`. |
| Private vulnerability reporting | Write-only today. |
| CodeQL default setup | Write-only today. |
| `security_and_analysis` on the repository | Not decoded by `RepositoryInfo`. Required for `security.secretScanning` and `security.pushProtection`. |
| Ruleset detail | Only the list call exists. The list endpoint returns identity, target, and enforcement — not rules, conditions, or bypass actors — so each ruleset needs an individual fetch. |

The `listAll` helper decodes a bare JSON array, so it cannot page a wrapped list. Variables, secrets, and custom repository roles therefore do not page at all today and silently stop at GitHub's default page size. Import needs a wrapped-list variant of the helper, and the page ceiling has to be reconsidered for repositories that exceed it.

## Implementation phases

1. Add an `internal/importer` package with a small read-only API, the eight reads above, a wrapped-list pagination variant, reverse mapping to `config.Config`, diagnostics, and normalization for ruleset actors and server-generated values ([#18](https://github.com/kkamegawa/gh-repospec/issues/18)). Bypass actors are reversed by actor type, with the `RepositoryRole` type split between the fixed role IDs and custom roles. Rule parameters are emitted with sorted keys and integral numbers so output is byte-stable. An empty bypass actor list is written explicitly, because omitting it clears the actors on apply.
2. Add a refresh engine that limits edits to the existing managed surface ([#19](https://github.com/kkamegawa/gh-repospec/issues/19)). YAML editing needs a parser whose tokens carry byte offsets and that does not normalize line endings; `gopkg.in/yaml.v3` exposes only a start line and column and normalizes CRLF, so the rewrite path uses a second YAML parser while `config.Load` keeps its own strict decoding. JSON files are reformatted as JSON. Local intent is preserved, and secrets and unsupported state are reported.
3. Add import argument parsing and output, wire the importer into the authenticated read client, and complete planner comparisons ([#20](https://github.com/kkamegawa/gh-repospec/issues/20)). Actions workflow permissions, selected actions, environments, every `security` feature, ruleset contents, and ruleset bypass actors are currently reported as an update without reading the live value; the matching apply paths write unconditionally for the same reason. Both sides need the comparison.
4. Make `repository.description` tri-state ([#22](https://github.com/kkamegawa/gh-repospec/issues/22)) so an imported document that does not manage the description stops reporting a change against the repository it was read from.

## Convergence

"An empty plan" means `HasChanges() == false` and `HasBlocked() == false`. Two classes of change are excluded from that predicate because they cannot converge by construction: `ChangeManual` entries for `seed`, `commands`, and agent file paths, and secret updates, whose values GitHub never returns. Import output contains no secrets, so the exclusion only affects `--into` verification.

`repository.visibility` is required by the schema, so import always emits it, while `allowVisibilityChange` is local intent that import cannot supply. Applying imported output to a *different* repository can therefore report a blocked visibility change; the round-trip criterion applies to the repository the document was imported from.

## Verification

- Test YAML and JSON output through `config.Load`, including all supported repository sections, pagination past one page and past the page ceiling, wrapped list responses, open and closed milestones, every bypass actor type, secret diagnostics, unsupported states, and API failures.
- Test `--into` with comments, non-alphabetical keys, CRLF and LF, quoted and multiline values, flow syntax, collection additions and updates, declared entries absent from GitHub, and empty declared collections. Verify unchanged bytes and preserved local declarations, and that pre-save validation does not fail on an absent seed directory.
- Test invalid argument combinations, a positional repository combined with `-R`, flags before and after the positional argument, a `--format` value outside `yaml|json`, non-interactive input without `--yes`, format mismatches, no-op writes, confirmation refusal, concurrent edits, and save failures. Assert that import issues no GitHub write request, and that `import` appears in the usage text.
- Run `go test -shuffle=on ./...`, `go vet ./...`, the repository verification scripts, lint, Linux race tests, and `git diff --check` when implementation is complete.

## Scope boundary

This plan does not include `plan --ci`, `apply --force-secrets`, or implementation code, and it does not change the README status until the import implementation ships. Exit code `2` is part of the `plan --ci` work: `cli.Run` returns only an error today, and the exit-code plumbing is tracked with that command, not here.
