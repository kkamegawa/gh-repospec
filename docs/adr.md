# ADR: import managed-surface and source-preserving refresh

Date: 2026-09-05
Tracking: [Issue #11](https://github.com/kkamegawa/gh-repospec/issues/11)

## Decision

`import` remains read-only. It emits only repository state that the schema can represent and reports state that cannot be imported. `--into` refreshes only fields and top-level sections already declared by the user. A declared collection is managed as a complete boundary, including additions, updates, and removals of importable attributes. Local intent such as seed sources, approved commands, secret source mappings, optional flags, and agent file paths is retained.

YAML refresh will identify nodes with the parser but edit the original byte ranges directly. This preserves comments, key order, indentation, quoting, and line endings outside the changed ranges. Whole-document YAML re-encoding is not used for the preservation guarantee. JSON input is reformatted as JSON.

## Rationale

Import must be useful for adopting an existing repository without silently expanding management to sections the user did not declare. Collection-level refresh is necessary to remove stale declarations and represent the complete remote collection once the user has declared it. Local configuration cannot be inferred from GitHub and must survive a refresh. Minimal source edits avoid turning a small refresh into an unrelated formatting diff.

## Consequences

- New import output can omit non-importable values while explaining how to supply them.
- A declared collection can remove configuration entries during `--into`; this is an intentional managed-boundary operation and is shown before confirmation.
- The implementation needs source-span editing, deterministic collection ordering for new entries, and additional read APIs for complete round-trip comparison.
