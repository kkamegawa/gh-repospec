# gh-repospec

`gh-repospec` is a precompiled GitHub CLI extension that converges a GitHub.com repository on a declarative YAML or JSON bootstrap file. It uses the current GitHub CLI login and never stores or prints its token.

[日本語](README_ja.md)

## Status

Repository configuration version 1 and version 2 target personal repositories and GitHub Free, Pro, and Team organizations on GitHub.com. Version 2 also supports personal account issue types and fields. GitHub Enterprise Server is rejected. Enterprise Cloud is outside the supported scope.

## Install

Install a published release:

```text
gh extension install kkamegawa/gh-repospec
```

For local development, build the executable into the repository root, then install the root as an extension. `gh extension install` uses the directory name as the extension name, so the checkout directory must be named `gh-repospec` and contain an executable of the same name.

```bash
go build -o gh-repospec ./cmd/gh-repospec
gh extension install .
```

```powershell
go build -o gh-repospec.exe ./cmd/gh-repospec
gh extension install .
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

Repository configuration accepts `schemaVersion: 1` and `schemaVersion: 2`. Version 2 requires `scope: repository` and carries the same repository settings as version 1. New repository imports use version 1 by default. YAML and JSON are accepted; the bundled JSON Schema is [`schema/repospec.schema.json`](schema/repospec.schema.json).

Unknown fields, duplicate YAML keys, invalid enum values, multiple documents, and implicit environment-variable expansion are rejected. Relative directory and ZIP seed paths are resolved relative to the configuration file.

| Section | Purpose |
| --- | --- |
| `repository` | Target repository, creation, visibility, description, topics, default branch, and merge settings. |
| `seed` | Exactly one `template`, `directory`, or `zip` source. Differing existing files require a path in `overwrite`. |
| `actions` | GitHub Actions permissions, variables, secrets, and environments. `allowedActions: selected` requires `selectedActions`, or every Action is blocked. |
| `rulesets` | Repository rulesets, identified by name. Classic branch protection is declared separately in `settings.branchProtections`. |
| `security` | Dependency graph, Dependabot, secret scanning, push protection, CodeQL default setup, AI Scan for pull requests (`aiPullRequestScan`), and private vulnerability reporting. `aiPullRequestScan` is a GitHub public preview that requires GitHub Advanced Security, Copilot, and CodeQL default setup; mark it `optional: true`. |
| `agents` | Agent variables/secrets and repository-relative locations of MCP, instruction, agent, and skill files supplied by the seed. |
| `labels` | Issue and pull request labels. Additive: labels absent from configuration are never deleted. |
| `milestones` | Milestones. Additive: retire one with `state: closed` rather than removing it. |
| `settings` | Pages, webhooks, autolinks, deploy keys, collaborators, teams, custom property values, classic branch protection, interaction limits, the pull request creation cap, and Copilot Code Review. Branch protection `settings` use the field names of GitHub's update API, and `import` writes them in that form. |
| `commands` | Explicitly approved, argv-based local initialization commands such as `apm init`. |

Secrets contain a `sourceEnv` name, never a secret value. Their default update policy is `createOnly`; use `always` only when intentional rotation is required.

### Copilot Code Review

`settings.copilotCodeReview` manages `reviewEffort`, `customInstructionsEnabled`, `mcpEnabled`, and `severityFilter`. Omitted fields remain unmanaged. Set `reviewEffort` to an effort available on the target repository or to `inherit` to clear a visible override. The severity filter accepts `no_filter`, `low`, and `low_and_medium`. Configure automatic review requests with the `copilot_code_review` branch ruleset.

- **Local GitHub CLI.** Use `gh auth login` as a repository administrator. The OAuth token or classic PAT needs the `repo` scope.
- **Read-only CI (`plan` and `import`).** Pass a GitHub App token or fine-grained PAT with repository **Administration: read** to the CLI through `GH_TOKEN`.
- **Apply CI.** Pass a GitHub App token or fine-grained PAT with repository **Administration: write** through `GH_TOKEN`. Include any additional permissions required by other settings in the same configuration.

GitHub Actions workflow `permissions` cannot grant **Administration** to the built-in `GITHUB_TOKEN`.

### Personal account issue settings

Issue types and issue fields belong to the GitHub personal account. GitHub makes them available across repositories owned by that account. Keep these settings in a separate personal document with `schemaVersion: 2`, `scope: personal`, and the account login. The document declares `issueTypes` and `issueFields`, including select field options. Issue field pinning and display order are configured in GitHub settings. See the [schema v2 design](docs/architecture/schema-v2.md) for the full contract and behavior.

```yaml
schemaVersion: 2
scope: personal
account: octocat
issueTypes:
  - name: Bug
    isEnabled: true
issueFields:
  - name: Target date
    dataType: date
```

`personal plan`, `personal apply`, and `personal import` manage the authenticated user's account-wide definitions. The declared account must match the GitHub CLI login. Applying personal settings can change issue behavior in every repository owned by that account. Imports and plans require access to the personal Issue Types and Issue Fields APIs. With classic or OAuth credentials, reads require `read:user` and writes require `user`. Personal field updates preserve undeclared options and existing option IDs.

## Commands

```text
gh repospec validate --file <path>
gh repospec plan --file <path> [-R owner/repository] [--format text|json] [--ci]
gh repospec apply --file <path> [-R owner/repository] [--yes] [--approve-command <id>] [--force-secrets]
gh repospec import <owner/repository> [--format yaml|json] [--schema-version 1|2] [--into <path> [--yes]]
gh repospec personal plan --file <path> [--format text|json] [--ci]
gh repospec personal apply --file <path> [--format text|json] [--yes]
gh repospec personal import [--format yaml|json] [--into <path> [--yes]]
gh repospec version
```

The `-R`/`--repo` flag overrides the repository declared in the configuration. `--approve-command` is repeatable and required for every configured command, including with `--yes`. `--version`/`-v` are accepted as aliases for `version`.

`plan --ci` turns the plan into a drift check for scheduled workflows. `apply --force-secrets` re-sets every configured secret in that run, which is how a credential rotation is applied without editing the file. New repository imports use schema version 1 by default, and `--schema-version 2` opts into the repository scope in v2. With `--into`, the destination determines the schema version, and the command refreshes declared entries while preserving YAML comments and key order. Personal commands use a separate account document and GitHub's personal issue definition APIs. Personal import writes version 2. Secret values are never returned by the GitHub API, so repository import reports secret names rather than inventing values.

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

Define Copilot MCP servers in seed content such as `.github/mcp.json`, alongside instructions, custom agents, skills, and setup workflows. `settings.copilotCodeReview.mcpEnabled` controls whether code reviews can use MCP servers. Copilot licenses and seats, organization policies, and inherited organization rulesets require manual setup. Unsupported plan-dependent features are blocked unless their configuration marks them `optional: true`. For Copilot Code Review, `plan` blocks on a failed read, and `import` reports a diagnostic if GitHub returns 404 or a 403 unrelated to rate limiting.

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

If `plan` or `apply` reports an error, see [docs/troubleshooting.md](docs/troubleshooting.md) for common causes and fixes, including caveats when applying an imported configuration to another repository.

See [CONTRIBUTING.md](CONTRIBUTING.md) for the contribution workflow, [docs/design.md](docs/design.md) for the repository design, and [docs/architecture/schema-v2.md](docs/architecture/schema-v2.md) for the personal and repository schema v2 design. [docs/gh-infra-parity.md](docs/gh-infra-parity.md) records the feature comparison with `babarot/gh-infra`, including what this extension deliberately does not do.

## License

Distributed under the [MIT License](LICENSE).
