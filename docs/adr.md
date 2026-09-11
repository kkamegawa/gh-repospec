# ADR

Records 1–5 are dated 2026-09-05 and track
[Issue #11](https://github.com/kkamegawa/gh-repospec/issues/11) (import and
format-preserving refresh). Record 6 is dated 2026-09-08 and tracks
[Issue #12](https://github.com/kkamegawa/gh-repospec/issues/12) (drift detection).
Record 7 is dated 2026-09-10 and tracks
[Issue #30](https://github.com/kkamegawa/gh-repospec/issues/30) (documentation
for drift detection). Record 8 is dated 2026-09-11 and tracks
[Issue #35](https://github.com/kkamegawa/gh-repospec/issues/35) (AI Scan for pull
requests).

## 1. Import manages only what the schema and the file already declare

**Decision.** `import` stays read-only. It emits only repository state the schema can
represent and reports state it cannot. `--into` refreshes only fields and top-level
sections the user already declared. Local intent — seed sources, the seed `overwrite`
list, approved commands, secret source mappings and update policies, `optional` flags,
agent file paths, `agents.secrets`, `repository.create`, and
`repository.allowVisibilityChange` — is retained.

**Rationale.** Import has to be useful for adopting an existing repository without
silently expanding management to sections the user did not declare. None of the local
intent above can be inferred from GitHub, so it has to survive a refresh.

**Consequences.** Import output can omit non-importable values, and has to explain how
to supply them. The bypass actor reverse mapping needs an explicit rule, because
GitHub's `RepositoryRole` actor type covers both the fixed roles and custom roles.

## 2. A refresh does not delete a declaration that GitHub does not have

**Decision.** Within a declared collection, `--into` updates entries that exist on
GitHub and adds entries that do not appear in the file. A declared entry that does not
exist on GitHub is kept and reported as not yet applied.

This replaces the earlier decision that a declared collection is a complete managed
boundary whose entries are added, updated, *and removed* to match the imported state.

**Rationale.** A configuration entry is a desired state that may simply not have been
applied yet. Deleting it turns drift into acceptance without the user deciding to accept
it, and it contradicts the additive rule that
[`gh-infra-parity.md`](gh-infra-parity.md) records as the reason for rejecting
`reconcile: authoritative`. Keeping the entry costs one diagnostic line; removing it
costs a declaration the user cannot recover except from version control.

**Consequences.** `--into` cannot be used to prune a configuration file. A file can
accumulate entries that never converge, so the reported difference has to distinguish
"updated", "added from GitHub", and "declared but not present on GitHub".

## 3. The refresh path uses a second YAML parser

**Decision.** YAML refresh parses the file with a parser whose tokens carry byte
offsets and that does not normalize line endings, and replaces only the spans that
changed. `config.Load` keeps using `gopkg.in/yaml.v3` with its strict decoding; the
second parser is confined to the rewrite path and must not relax that strictness.

This replaces the earlier decision to identify nodes with the existing parser and edit
the original byte ranges directly.

**Rationale.** `gopkg.in/yaml.v3` exposes only a node's start line and column. There is
no span end and no byte offset, so the end of a node can only be re-derived by scanning
— which is unreliable for block scalars, flow collections, anchors, and multi-byte
characters. The parser also normalizes CRLF to LF, which makes preserving the original
line endings impossible on that path. Whole-document re-encoding is still not an
option: it would turn a small refresh into an unrelated formatting diff.

**Consequences.** The project takes a second YAML dependency for one code path. The two
parsers must agree on what a valid document is, which needs fixture coverage: anything
`config.Load` rejects, the refresh path must not silently accept.

## 4. "An empty plan" excludes changes that cannot converge

**Decision.** An empty plan means `HasChanges() == false` and `HasBlocked() == false`,
excluding `ChangeManual` entries for `seed`, `commands`, and agent file paths, and
excluding secret updates.

**Rationale.** GitHub never returns a secret value, so a declared secret whose source
environment variable is set is always an update. Manual entries describe work the tool
reports rather than performs. Without the exclusion, the round-trip criterion in
[Issue #20](https://github.com/kkamegawa/gh-repospec/issues/20) is unsatisfiable for any
file that declares a secret or a seed.

**Consequences.** Import output contains no secrets, so the exclusion only affects
`--into` verification. Completing the criterion also requires the current-value
comparisons listed in [`plan.md`](plan.md) and the tri-state `repository.description`
tracked in [Issue #22](https://github.com/kkamegawa/gh-repospec/issues/22).

## 5. `repository.description` becomes tri-state

**Decision.** `repository.description` changes from a plain string to an optional value
that distinguishes an absent key from an empty one, matching `repository.homepage`.
An absent key leaves the description alone; an explicit empty string clears it.
`schemaVersion` stays `1`.

**Rationale.** The planner compares the description without a nil check, so a document
that does not manage the description reports an update forever against a repository that
has one. That makes the round-trip criterion unsatisfiable for the common case of
importing a repository and choosing not to manage its description. `homepage` already
has the distinction; the asymmetry was an oversight rather than a decision.

**Consequences.** Every existing configuration file stays valid, because a declared
`description` keeps its current meaning. The change touches the schema, the config
types, the planner comparison, and the apply request, and is tracked separately as
[Issue #22](https://github.com/kkamegawa/gh-repospec/issues/22).

## 6. Drift detection ignores secret updates

**Decision.** `plan --ci` exits `2` only when the plan contains a create or update that
is not a secret. `Plan.HasDrift()` replaces `Plan.HasChanges()`, and `Change` carries an
`Opaque` flag set on secret entries. A blocked plan exits `1` and outranks drift. `plan`
without `--ci` still exits `0` in every case.

**Rationale.** Issue #12 says "at least one create or update change". Taken literally,
any configuration that declares a secret would report drift on every scheduled run,
because GitHub never returns a secret value and record 4 already excludes secret
updates from "an empty plan". A drift check that always fails is not a drift check. The
exclusion is carried by an explicit field rather than by matching the resource name, so
a display-string change cannot silently break the exit code.

**Consequences.** A secret rotated outside the tool is not detected — by construction,
it cannot be; `apply --force-secrets` is the answer for rotation. `Change` gains an
`omitempty` JSON field, so archived `plan.json` output is unchanged for every non-secret
entry. `HasChanges` is removed; its only caller was a test.

## 7. The drift-detection specification text is corrected with the documentation

**Decision.** Issue #30 also rewrites the drift-detection and exit-code prose in
`docs/design.md` and `docs/design_ja.md`, which its scope described as already correct.
The corrected text states that `plan --ci` exits `2` for a create or update the tool can
converge, that secret updates are excluded, that a blocked plan exits `1` and outranks
drift, and that `plan` without `--ci` always exits `0`. The outcome table from the
implementation plan moves into `design.md` as the normative record.

**Rationale.** `design.md` said `plan --ci` "exits `2` when any change would be made".
Record 6 had already settled the opposite for secrets, and `planOutcome` gives a blocked
plan exit `1`, so the settled behavior contradicted its own specification. Issue #30
requires the drift-detection workflow example to describe the exit-code contract
accurately, including the secret exclusion; leaving the specification saying something
else would make the example and the document it points to disagree.

**Consequences.** No behavior changes — the correction is documentation only, and it
records what records 4 and 6 already decided. The implementation plan's scope boundary
("this plan does not change `docs/design.md`") is superseded for that reason, and the
wiki plan page carries a completion note saying so.

## 8. AI Scan for pull requests is a repository-scoped security feature

**Decision.** `security.aiPullRequestScan` is a `FeatureSetting` (`enabled`, `optional`)
managed only through `GET`/`PATCH /repos/{owner}/{repo}/code-scanning/ai-scan`. The
organization endpoint is not managed. Enabling it depends on CodeQL default setup, which
is evaluated like `dependabotSecurityUpdates` on `dependabotAlerts` and applied first in
the same stage. `import` reports a 403 or 404 read as a diagnostic instead of writing
`enabled: false`. The API is a public preview, but it needs no opt-in flag or separate
request header, because it is served under the `2026-03-10` API version the client
already pins. `schemaVersion` stays `1`.

**Rationale.** The extension converges one repository, and organization policy is
already reported as manual work rather than automated. Reusing `FeatureSetting` keeps
the schema, the planner, and the refresh paths uniform with the other security
features. GitHub requires CodeQL default setup for AI Scan, so checking the dependency
in `plan` reports it as blocked instead of failing partway through `apply`. Writing a
403 as `false` would contradict record 1 and never converge, because the same read fails
on every plan.

**Consequences.** A repository whose organization has disabled AI Scan fails at
`apply` unless the declaration is `optional: true`. Advanced Security, Copilot
licensing, enterprise and organization enablement, and AI credit consumption cannot be
checked before writing, so documentation recommends `optional: true`. If GitHub changes
the preview API's shape, only the client types and this field need revising. Binaries
older than this change reject the new key as an unknown field.

## 9. A code-scanning read refused with 403 means the feature is unavailable

**Decision.** When GitHub answers the CodeQL default setup or AI Scan read with 403,
`plan` treats the feature as unavailable on that repository: the declaration is
reported as blocked, or as manual when `optional: true`, with GitHub's message as the
reason, and `apply` skips the write. `import` reports a CodeQL default setup 403 as a
diagnostic and omits the key, as record 8 already does for AI Scan.

This extends record 8, under which `plan` reported an unreadable AI Scan state as an
update.

**Rationale.** The extension's primary use is bootstrapping an empty repository, and a
private personal repository has no GitHub Advanced Security, so code scanning is not
enabled there. Reporting such a feature as an update makes an optional declaration
drift forever — no `apply` can change it, so `plan --ci` exits `2` on every run — which
defeats the purpose of `optional: true`. `import` previously failed outright on the
CodeQL 403, which contradicts record 1.

**Consequences.** A 403 caused by insufficient permissions cannot be told apart from an
unavailable feature and takes the same path. A required declaration is blocked, so
authorization failures still stop the run; an optional one is reported as manual, and
its reason carries GitHub's message so the user can tell the two apart. If code scanning
is later enabled for the repository, the next plan reads the state normally and
converges.
