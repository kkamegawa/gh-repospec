# Implementation Plan: drift detection, plan --ci, and apply --force-secrets

## Summary

Issue [#12](https://github.com/kkamegawa/gh-repospec/issues/12) adds the exit-code contract, `plan --ci` as a drift check for scheduled workflows, and `apply --force-secrets` for credential rotation without a file edit. The work is split into the exit-code mechanism with `plan --ci`, `apply --force-secrets`, and the documentation and example-workflow updates that depend on both. This document records the implementation plan; the current draft PR contains planning and design documentation only. `docs/design.md` (`## Drift detection and CI`, and the exit-code paragraph) and `docs/design_ja.md` already specify the intended behavior correctly and are not changed by this work.

## Tracking

| Workstream | Issue |
|---|---|
| Parent | [#12](https://github.com/kkamegawa/gh-repospec/issues/12) |
| Exit-code contract and `plan --ci` | [#28](https://github.com/kkamegawa/gh-repospec/issues/28) |
| `apply --force-secrets` | [#29](https://github.com/kkamegawa/gh-repospec/issues/29) |
| Documentation and example workflows | [#30](https://github.com/kkamegawa/gh-repospec/issues/30) |

## Drift predicate excludes secret updates

`appendSecretChange` reports every declared secret whose `sourceEnv` is set as `ChangeUpdate` unconditionally, because GitHub never returns a secret value. `docs/adr.md` record 4 already excludes secret updates from "an empty plan" (`HasChanges() == false && HasBlocked() == false`) for exactly this reason. Wiring `plan --ci` directly to the existing `HasChanges()` would report drift on every scheduled run for any configuration that declares a secret, which makes the check useless.

This plan therefore replaces `Plan.HasChanges()` with `Plan.HasDrift()`: the same create/update predicate, but excluding changes marked `Opaque`. `Change` gains an `Opaque bool` field (`omitempty` in JSON), set on both branches of `appendSecretChange` (the blocked branch and the update branch), because opacity is a property of the resource, not of the state it happens to be in. This is a deliberate deviation from issue #12's literal wording ("at least one create or update change") and is recorded as ADR record 6 in `docs/adr.md` and `docs/adr_ja.md`, dated 2026-09-08.

An explicit field was chosen over matching the `actions.secret.` / `agents.secret.` resource-name prefix, because a prefix match couples the exit code to a display string that can change independently and silently break the check.

## Exit-code mechanism

`cli.Run` keeps its `error` return type; a signature change to `(int, error)` would add exit-code noise to roughly fifteen return statements across four commands for the sake of one. Instead, `internal/cli` gains a typed `DriftError` carrying exit code `2`. `cmd/gh-repospec/main.go` maps it through a small testable `exitCode(err error, stderr io.Writer) int` function: a `*DriftError` returns `2` without printing an `error:` line (drift is a successful detection, not a failure), any other non-nil error prints `error: <err>` and returns `1`, and `nil` returns `0`.

`plan --ci` fails (exit `1`) on a blocked plan even when drift is also present — a blocked plan is a failure that outranks detection. `plan` without `--ci` is unaffected by this work: it keeps exiting `0` in every case, including when the plan is blocked, exactly as it does today.

| Situation | `plan` | `plan --ci` |
|---|---|---|
| No changes | 0 | 0 |
| Only secret updates (opaque) | 0 | 0 |
| Only `ChangeManual` (seed, commands, agent files) | 0 | 0 |
| Create/update present | 0 | 2 |
| Blocked present | 0 | 1 |
| Blocked and drift present | 0 | 1 |

The drift notice that accompanies exit `2` is written to standard error, so `--format json` output on standard output stays a valid, complete document at every exit code, including when a workflow redirects it to a file.

## `apply --force-secrets`

`planner.Options` gains `ForceSecrets bool`. `applySecrets`'s `agents bool` parameter is replaced with a named `secretTarget` type (`secretTargetActions` / `secretTargetAgents`), and `Options` is threaded through `applyActions` and `applyAgentVariables` down to `applySecrets`, rather than adding a second boolean parameter. The `createOnly` skip becomes conditional on `!options.ForceSecrets`. The flag does not relax the `sourceEnv` requirement: a secret without its source environment variable is still `ChangeBlocked` at plan time, and `Apply` still refuses a blocked plan.

`--force-secrets` is registered for `apply` only and has no effect on `Build`, so plan output is identical whether or not the flag is later passed to `apply`.

## Verification

- `internal/planner`: a table test asserting `HasDrift()` excludes opaque updates but not others; a `Build` test asserting `appendSecretChange` marks both its branches `Opaque`; new `apply` tests (using a new fake implementing `secretService`, none exists in the test package today) covering the baseline `createOnly` skip, `--force-secrets` rewriting an existing Actions secret, `--force-secrets` covering agent secrets, and `--force-secrets` still blocking on a missing source environment variable.
- `internal/cli`: table tests for the `plan --ci` outcome table above; flag-parsing tests asserting `--ci` is plan-only and `--force-secrets` is apply-only; a usage-text test.
- `cmd/gh-repospec`: a new `main_test.go` unit-testing `exitCode` directly (including the "no `error:` line on drift" assertion), plus one subprocess re-exec test (the pattern already used in `internal/seed/seed_test.go`) proving `main` wires `exitCode` into `os.Exit` for an offline case.
- Run `go test -shuffle=on ./...`, `go vet ./...`, the repository verification scripts, lint, Linux race tests, and `git diff --check` when implementation is complete.
- Manual: `plan --ci --format json` against a drifted repository exits `2` with valid JSON on stdout; against a converged repository exits `0`; against a configuration whose only difference is a secret exits `0`. `apply --force-secrets` against a repository with an existing `createOnly` secret updates it.

## Scope boundary

This plan does not change `docs/design.md` or `docs/design_ja.md` — the drift-detection and exit-code sections there already describe the shipped behavior. Documentation and example-workflow updates (README status note, `docs/gh-infra-parity.md`, the two example workflows) are scoped to [#30](https://github.com/kkamegawa/gh-repospec/issues/30) and land only after both [#28](https://github.com/kkamegawa/gh-repospec/issues/28) and [#29](https://github.com/kkamegawa/gh-repospec/issues/29) ship, so no document claims behavior before it exists.
