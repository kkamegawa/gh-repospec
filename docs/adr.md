# ADR: import and format-preserving refresh

Every record below is dated 2026-09-05 and tracks
[Issue #11](https://github.com/kkamegawa/gh-repospec/issues/11).

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
