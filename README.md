# gh-repobootstrapper

`gh-repobootstrapper` is a precompiled GitHub CLI extension that converges a GitHub.com repository on a declarative YAML or JSON bootstrap file. It uses the current GitHub CLI login and never stores or prints its token.

[日本語](README_ja.md)

## Status

Version 1 targets personal repositories and GitHub Free, Pro, and Team organizations on GitHub.com. GitHub Enterprise Server is rejected. Enterprise Cloud is outside the supported scope.

## Install

Install a published release:

```text
gh extension install kkamegawa/gh-repobootstrapper
```

For local development, build the executable at the repository root, then install that directory as an extension.

```bash
go build -o gh-repobootstrapper ./cmd/gh-repobootstrapper
gh extension install .
```

```powershell
go build -o gh-repobootstrapper.exe ./cmd/gh-repobootstrapper
gh extension install .
```

Authenticate GitHub CLI against GitHub.com before using `plan` or `apply`.

## Quick start

Copy and edit [`docs/examples/bootstrap.yaml`](docs/examples/bootstrap.yaml). The example uses placeholders; replace them with the target repository and your chosen seed source.

```text
gh repobootstrapper validate --file bootstrap.yaml
gh repobootstrapper plan --file bootstrap.yaml
gh repobootstrapper apply --file bootstrap.yaml
```

`plan` is read-only. `apply` recalculates the plan, displays it, and asks for confirmation unless `--yes` is supplied. Each configured command still requires its own `--approve-command <id>` flag.

## Configuration

The public contract is `schemaVersion: 1`. YAML and JSON are accepted; the bundled JSON Schema is [`schema/repobootstrapper.schema.json`](schema/repobootstrapper.schema.json).

Unknown fields, duplicate YAML keys, invalid enum values, multiple documents, and implicit environment-variable expansion are rejected. Relative directory and ZIP seed paths are resolved relative to the configuration file.

| Section | Purpose |
| --- | --- |
| `repository` | Target repository, creation, visibility, description, topics, default branch, and merge settings. |
| `seed` | Exactly one `template`, `directory`, or `zip` source. Differing existing files require a path in `overwrite`. |
| `actions` | GitHub Actions permissions, variables, secrets, and environments. |
| `rulesets` | Repository rulesets, identified by name. Branch protection is not managed through the legacy API. |
| `security` | Dependency graph, Dependabot, secret scanning, push protection, CodeQL default setup, and private vulnerability reporting. |
| `agents` | Agent variables/secrets and repository-relative locations of MCP, instruction, agent, and skill files supplied by the seed. |
| `commands` | Explicitly approved, argv-based local initialization commands such as `apm init`. |

Secrets contain a `sourceEnv` name, never a secret value. Their default update policy is `createOnly`; use `always` only when intentional rotation is required.

## Commands

```text
gh repobootstrapper validate --file <path>
gh repobootstrapper plan --file <path> [-R owner/repository] [--format text|json]
gh repobootstrapper apply --file <path> [-R owner/repository] [--yes] [--approve-command <id>]
```

The `-R`/`--repo` flag overrides the repository declared in the configuration. `--approve-command` is repeatable and required for every configured command, including with `--yes`.

## Safety model

- Only declared settings, files, and rulesets are converged. Omitted resources are never deleted.
- Changing the visibility of an existing repository requires `repository.allowVisibilityChange: true`.
- Rulesets are applied last so they do not block repository initialization.
- ZIP seeds reject path traversal, symbolic links, `.git`, and oversized extraction.
- Commands are run as a program plus an argument array; shell programs are rejected. They are not OS-sandboxed, so approve only trusted programs.
- The overall operation is not transactional. A failed apply reports the partial state; rerun it after resolving the error to converge again.

## Limits and manual work

Repository-level automation cannot configure Copilot MCP servers in repository settings, Copilot licenses or seats, organization policies, or inherited organization rulesets. Put seed-managed files such as `.github/mcp.json`, instructions, custom agents, skills, and setup workflows in the source content instead. Unsupported plan-dependent features are blocked unless their configuration marks them `optional: true`; authorization failures always stop the run.

## Development

Use Go 1.26 or later.

```bash
go test ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
go build ./cmd/gh-repobootstrapper
```

```powershell
go test ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
go build ./cmd/gh-repobootstrapper
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the contribution workflow and [docs/design.md](docs/design.md) for the settled design.

## License

Distributed under the [MIT License](LICENSE).
