# Implementation Plan: import and format-preserving refresh

## Summary

Issue [#11](https://github.com/kkamegawa/gh-repospec/issues/11) adds a read-only `import` command and an `--into` refresh flow. The work is split into state fetching/reverse mapping, source-preserving file editing, and CLI/planner convergence. This document records the implementation plan; the current draft PR contains planning and design documentation only.

## Tracking

| Workstream | Issue |
|---|---|
| Parent | [#11](https://github.com/kkamegawa/gh-repospec/issues/11) |
| State fetching and reverse mapping | [#18](https://github.com/kkamegawa/gh-repospec/issues/18) |
| Format-preserving `--into` refresh | [#19](https://github.com/kkamegawa/gh-repospec/issues/19) |
| CLI and round-trip convergence | [#20](https://github.com/kkamegawa/gh-repospec/issues/20) |

## Implementation phases

1. Add an `internal/importer` package with a small read-only API, complete pagination, reverse mapping to `config.Config`, diagnostics, and normalization for ruleset actors and server-generated values.
2. Add a refresh engine that limits edits to the existing managed surface. Use YAML node positions and byte ranges for minimal source edits; use deterministic JSON formatting for JSON files. Preserve local intent and report secrets and unsupported state.
3. Add import argument parsing and output, wire the importer into the authenticated read client, and complete planner comparisons for Actions, security, environments, and rulesets. Verify that imported output has no create/update/blocked changes against the same state.

## Verification

- Test YAML and JSON output through `config.Load`, including all supported repository sections, pagination, milestones, bypass actors, secret diagnostics, unsupported states, and API failures.
- Test `--into` with comments, non-alphabetical keys, CRLF/LF, quotes, multiline values, flow syntax, collection additions/updates/deletions, and empty declared collections. Verify unchanged bytes and local declarations.
- Test invalid argument combinations, format mismatches, no-op writes, confirmation refusal, concurrent edits, and save failures. Assert that import issues no GitHub write request.
- Run `go test -shuffle=on ./...`, `go vet ./...`, the repository verification scripts, lint, Linux race tests, and `git diff --check` when implementation is complete.

## Scope boundary

This plan does not include `plan --ci`, `apply --force-secrets`, implementation code, or changing the README status until the import implementation ships.
