# Feature Parity Review: gh-infra

## Why this document exists

[`babarot/gh-infra`](https://github.com/babarot/gh-infra) solves an adjacent
problem: declarative management of GitHub repository settings through YAML,
without a state file. It is more mature in some areas than
`gh-repospec`, so its feature set is a useful checklist for finding
gaps in this extension's contract.

This document records the comparison, the features adopted into the
`gh-repospec` specification, and — just as importantly — the features
deliberately not adopted, with the reason for each. It is a design record, not
a roadmap: adopted items are specified in [`design.md`](design.md) and tracked
as implementation issues.

The comparison was made against the gh-infra documentation set (`import`,
`plan`, `apply`, `validate`; the `Repository`, `RepositorySet`, `File`, and
`FileSet` resources) as of August 2026.

## Adopted

| gh-infra capability | State in this extension | Specification |
|---|---|---|
| `import` — export live repository state as a manifest | Absent | [`import` command](design.md#import) |
| `import --into` — pull live state into an existing manifest | Absent | [`import` command](design.md#import) |
| `plan --ci` — non-zero exit when a difference exists | `plan` always exits 0 | [Drift detection](design.md#drift-detection-and-ci) |
| `--force-secrets` — re-set every secret in one run | Only per-secret `updatePolicy: always` | [Drift detection](design.md#drift-detection-and-ci) |
| Labels | Absent | `labels` section |
| Milestones | Absent | `milestones` section |
| `homepage`, `archived`, feature toggles, merge commit title/message formats, release immutability | Absent | `repository` section |
| Ruleset bypass actors (role / team / app / organization admin / custom role) | Implemented | `rulesets[].bypassActors` |
| Ruleset `evaluate` enforcement (dry run) | Implemented | `rulesets[].enforcement` |
| Actions fork pull request approval policy | Implemented (public repositories only; GitHub rejects the endpoint on private repositories) | `actions.forkPullRequestApproval` |
| Dependency check: security updates require vulnerability alerts | Implemented | `security` section |

## Not adopted

| gh-infra capability | Reason |
|---|---|
| `RepositorySet` / `FileSet` — one manifest, many repositories, with `defaults` and per-repository overrides | This extension bootstraps and converges one repository per document. Fan-out changes the document model (multi-document YAML, directory scanning, defaults merging) far beyond the current contract, and the current contract is deliberately strict and single-document. Revisit as a separate schema version if repeated-fleet use appears. |
| `File` / `FileSet` — continuously synchronized files with `content`, `source`, `github://` references, `<% %>` templating, unified-diff patches, and `via: pull_request` delivery | This extension already delivers repository content through `seed`, whose contract is one-time initialization from a template, directory, or ZIP. A second, overlapping file-delivery mechanism would make it ambiguous which one owns a path. Continuous file synchronization is a different product goal from bootstrapping. |
| `reconcile: authoritative` — delete labels, rulesets, branch protection rules, and files that the manifest does not declare | The additive rule ("declared resources converge; undeclared resources are never removed") is a settled safety property of this extension. Deletion is the one class of change a user cannot recover from with a rerun, and this tool is aimed at initialization rather than fleet enforcement. |
| Classic branch protection (`branch_protection`) | Rulesets are GitHub's successor to classic branch protection and cover this extension's use cases with richer controls (enforcement modes, bypass actors, tag targets). Supporting both APIs would let two overlapping declarations describe the same branch with no defined precedence. |
| `internal` visibility | Available only to organizations on GitHub Enterprise Cloud, which is outside the supported target set. |

## Deferred

These are worth revisiting but are not part of the current specification:

- **Interactive diff viewer** — gh-infra's `apply` offers a full-screen diff with per-file skip. This extension shows a text or JSON plan and a single confirmation prompt.
- **Parallel fetch with a bounded worker pool** — relevant only once a run touches many repositories, which follows from `RepositorySet`.
- **Log levels** (`--log-level`, `GH_INFRA_LOG`) — useful for debugging API failures; the current extension reports typed API errors instead.
- **Label aliases and exclusion patterns** — not implemented in gh-infra either, and only meaningful together with authoritative deletion.

## Compatibility of the adopted changes

Every adopted configuration field is optional, so `schemaVersion` stays `1`
and existing configuration files remain valid. The reverse is not true: a
document that uses a newly added field is rejected by an older binary, because
parsing rejects unknown fields by design. Pin the extension version in CI when
a configuration file uses recently added fields.
