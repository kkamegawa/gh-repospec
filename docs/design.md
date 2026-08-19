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
`security`, `agents`, and `commands`. Repository visibility is only changed
when `allowVisibilityChange` is true. A seed selects exactly one template,
directory, or ZIP source. Existing divergent files require their
repository-relative path in `seed.overwrite`.

Secrets have a `sourceEnv`, not a value. The default `updatePolicy` is
`createOnly`; `always` explicitly rotates it. Commands use a program and argv
array. Shell programs are rejected, paths are repository-relative, and timeout
uses Go duration syntax such as `5m`.

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
`--approve-command <id>` approval even with `--yes`. `version` (also
`--version`/`-v`) prints the release version embedded at build time with
`-ldflags`, following GitHub CLI extension convention.

Before writes, preflight checks the GitHub.com host, authenticated permissions,
repository state, feature availability, organization policy, seed source, and
required secret environment variables. Unsupported plan-dependent capabilities
may be skipped only if configured as `optional: true`; authorization failures
always stop the run.

Apply order is: repository creation or visibility; secret scanning and push
protection; files; default branch; repository and Actions settings; secrets and
variables; remaining security settings; rulesets. Rulesets are last so they do
not block the initial commit. The process is not transactional. A partial
failure is reported and a later apply converges the requested state.

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
