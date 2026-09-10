# gh-repospec

`gh-repospec` is a precompiled GitHub CLI extension that converges a GitHub.com repository on a declarative YAML or JSON bootstrap file. It uses the current GitHub CLI login and never stores or prints its token.

[日本語](README_ja.md)

## Status

Version 1 targets personal repositories and GitHub Free, Pro, and Team organizations on GitHub.com. GitHub Enterprise Server is rejected. Enterprise Cloud is outside the supported scope.

## Install

Install a published release:

```text
gh extension install kkamegawa/gh-repospec
```

For local development, build the executable under `bin`, then install that directory as an extension.

```bash
mkdir -p bin
go build -o bin/gh-repospec ./cmd/gh-repospec
gh extension install ./bin
```

```powershell
New-Item -ItemType Directory -Path bin -Force
go build -o bin/gh-repospec.exe ./cmd/gh-repospec
gh extension install ./bin
```

Authenticate GitHub CLI against GitHub.com before using `plan` or `apply`.

## Quick start

Copy and edit [`docs/examples/bootstrap.yaml`](docs/examples/bootstrap.yaml). The example uses placeholders; replace them with the target repository and your chosen seed source. [`docs/examples/bootstrap-extended.yaml`](docs/examples/bootstrap-extended.yaml) demonstrates the labels, milestones, bypass actors, and repository settings added by the specification review.

```text
gh repospec validate --file bootstrap.yaml
gh repospec plan --file bootstrap.yaml
gh repospec apply --file bootstrap.yaml
```

To start from a repository that already exists, `import` writes its current
settings as a configuration file instead of transcribing them by hand:

```text
gh repospec import OWNER/REPOSITORY > bootstrap.yaml
```

`plan` is read-only. `apply` recalculates the plan, displays it, and asks for confirmation unless `--yes` is supplied. Each configured command still requires its own `--approve-command <id>` flag.

## Configuration

The public contract is `schemaVersion: 1`. YAML and JSON are accepted; the bundled JSON Schema is [`schema/repospec.schema.json`](schema/repospec.schema.json).

Unknown fields, duplicate YAML keys, invalid enum values, multiple documents, and implicit environment-variable expansion are rejected. Relative directory and ZIP seed paths are resolved relative to the configuration file.

| Section | Purpose |
| --- | --- |
| `repository` | Target repository, creation, visibility, description, topics, default branch, and merge settings. |
| `seed` | Exactly one `template`, `directory`, or `zip` source. Differing existing files require a path in `overwrite`. |
| `actions` | GitHub Actions permissions, variables, secrets, and environments. `allowedActions: selected` requires `selectedActions`, or every Action is blocked. |
| `rulesets` | Repository rulesets, identified by name. Branch protection is not managed through the legacy API. |
| `security` | Dependency graph, Dependabot, secret scanning, push protection, CodeQL default setup, and private vulnerability reporting. |
| `agents` | Agent variables/secrets and repository-relative locations of MCP, instruction, agent, and skill files supplied by the seed. |
| `labels` | Issue and pull request labels. Additive: labels absent from configuration are never deleted. |
| `milestones` | Milestones. Additive: retire one with `state: closed` rather than removing it. |
| `commands` | Explicitly approved, argv-based local initialization commands such as `apm init`. |

Secrets contain a `sourceEnv` name, never a secret value. Their default update policy is `createOnly`; use `always` only when intentional rotation is required.

## Commands

```text
gh repospec validate --file <path>
gh repospec plan --file <path> [-R owner/repository] [--format text|json] [--ci]
gh repospec apply --file <path> [-R owner/repository] [--yes] [--approve-command <id>] [--force-secrets]
gh repospec import <owner/repository> [--format yaml|json] [--into <path> [--yes]]
gh repospec version
```

The `-R`/`--repo` flag overrides the repository declared in the configuration. `--approve-command` is repeatable and required for every configured command, including with `--yes`. `--version`/`-v` are accepted as aliases for `version`.

`plan --ci` turns the plan into a drift check for scheduled workflows. `apply --force-secrets` re-sets every configured secret in that run, which is how a credential rotation is applied without editing the file. `import` reads a repository and writes configuration; with `--into` it refreshes an existing file in place, preserving its comments and key order. A refresh only touches what the file already declares, and it never deletes a declaration the repository does not have — that entry is kept and reported as not yet applied. Secret values are never returned by the GitHub API, so `import` reports secret names rather than inventing values.

| Exit code | Meaning |
| --- | --- |
| `0` | Success. For `plan --ci`, the plan has no blocked entry and no difference the tool can converge. It is not proof that every setting matches: secret updates and manual entries are excluded from the comparison. |
| `1` | The command failed. Under `--ci` this includes a blocked plan, which outranks drift. |
| `2` | `plan --ci` found at least one difference it can converge. Secret updates are excluded from the comparison. |

[`docs/examples/workflows`](docs/examples/workflows) contains an apply-on-merge workflow and a scheduled drift-detection workflow.

### Shorter invocation

`gh alias set` creates a personal shortcut for the extension name:

```text
gh alias set rb repospec
gh rb plan --file bootstrap.yaml
```

Neither example above needs quoting. Quote the expansion when it contains spaces or arguments, and on Windows Command Prompt use double quotes rather than single quotes for that.

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

The APM dependency directory is a nested Go module so generated skill examples
are excluded from the product module's `go test ./...` and `go vet ./...` scans.

```bash
go test ./...
go vet ./...
mkdir -p bin
go build -o bin/gh-repospec ./cmd/gh-repospec
```

```powershell
go test ./...
go vet ./...
New-Item -ItemType Directory -Path bin -Force
go build -o bin/gh-repospec.exe ./cmd/gh-repospec
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the contribution workflow and [docs/design.md](docs/design.md) for the settled design. [docs/gh-infra-parity.md](docs/gh-infra-parity.md) records the feature comparison with `babarot/gh-infra`, including what this extension deliberately does not do.

## License

Distributed under the [MIT License](LICENSE).
