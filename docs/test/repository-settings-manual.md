# Manual repository import and parity test

This procedure exercises `import`, `plan`, and `apply` against two GitHub repositories. It verifies the settings represented by the current repospec schema and calls out states the GitHub API does not return. GitHub credentials and repository state are never written into this document.

## Preconditions

- Go and GitHub CLI (`gh`) are installed.
- `gh auth status` succeeds for an account with repository administration access to both repositories. Organization-owned targets also require permission to manage team access and custom properties.
- Repository interaction restrictions and PR cap bypasses require the corresponding administration or maintainer permissions; every source and target API read must succeed before parity can be accepted.
- The source is `kkamegawa/Relaypublisher`; the destination is `kkamegawa/repotest1`.
- The destination has no commits. The apply operation creates its initial `main` branch and commit.
- Review the generated plan before applying it. `apply --yes` skips only the confirmation prompt; it still refuses a blocked plan.

## Local regression test

Run this test before the live repository test. It uses an in-memory GitHub API and a subprocess helper, so it does not write to GitHub:

```sh
go test ./internal/planner -run '^TestEmptyRepositoryInitializationAndCommandsRunOnce$' -count=1 -v
```

The test verifies that the first apply creates `README.md` and the VisualStudio `.gitignore`, initializes `main`, commits the `apm.yml` that its deterministic helper command writes, records command completion, and does not run the command again on the second apply. The helper writes a fixed file named `repotest1` with the Relaypublisher targets and dependencies; that content belongs to the helper, not to `apm init`.

`TestFailedCommandReportsRedactedOutput` in the same package verifies that a failing command's output is part of the reported error, with declared secret values replaced by `***`.

The complete automated test suite is:

```sh
go test ./...
```

## Live import, plan, and apply

Build variables are shown for Bash and PowerShell 7. Both procedures create a local configuration file. Edit that file after import to add the `seed.initialize` block below before validating it.

```yaml
seed:
  initialize:
    gitignoreTemplate: VisualStudio
    readme: true

commands:
  - id: apm-init
    program: apm
    args: [init, --yes]
    cwd: .
    timeout: 5m
    environment: [APM_NONINTERACTIVE, PATH, HOME, USERPROFILE]
```

The environment allowlist is exact. `apm` starts `git` and reads the home directory, so it needs `PATH` and the home directory variable: `HOME` on macOS and Linux, `USERPROFILE` on Windows. A listed variable that is not set is skipped, so the same list works on every platform. With only `APM_NONINTERACTIVE`, `apm` fails because it cannot find `git`, and `apply` reports the command's output under the error.

The first apply must include `--approve-command apm-init`. `apm init --yes` creates a default `apm.yml` in the staging workspace; it does not copy Relaypublisher's `targets` or `dependencies`, and it names the project after the staging directory (`name: seed`), not after the destination repository. Verify that `apm.yml` exists in the initial commit; edit its content in a later commit if the project needs the Relaypublisher targets and dependencies.

The source import emits diagnostics on standard error. For a public source, they include `actions.workflowAccessLevel`, because GitHub applies the workflow access policy only to private and internal repositories, and one line per secret name, because secret values are never returned.

### Bash

```bash
set -euo pipefail
OWNER=kkamegawa
SOURCE=Relaypublisher
TARGET=repotest1
CONFIG=repotest1.yaml

gh auth status
go run ./cmd/gh-repospec import "$OWNER/$SOURCE" --format yaml > "$CONFIG"
# Edit $CONFIG and add the seed.initialize block shown above.
go run ./cmd/gh-repospec validate --file "$CONFIG"
go run ./cmd/gh-repospec plan --file "$CONFIG" --repo "$OWNER/$TARGET"
# Review the complete plan, then approve each first-run command explicitly.
go run ./cmd/gh-repospec apply --file "$CONFIG" --repo "$OWNER/$TARGET" --yes --approve-command apm-init
```

### PowerShell 7

```powershell
$ErrorActionPreference = 'Stop'
$owner = 'kkamegawa'
$source = 'Relaypublisher'
$target = 'repotest1'
$config = 'repotest1.yaml'

gh auth status
if ($LASTEXITCODE -ne 0) { throw 'GitHub CLI authentication is required.' }
go run ./cmd/gh-repospec import "$owner/$source" --format yaml | Set-Content -Encoding utf8 $config
if ($LASTEXITCODE -ne 0) { throw 'Import failed.' }
# Edit $config and add the seed.initialize block shown above.
go run ./cmd/gh-repospec validate --file $config
if ($LASTEXITCODE -ne 0) { throw 'Configuration validation failed.' }
go run ./cmd/gh-repospec plan --file $config --repo "$owner/$target"
if ($LASTEXITCODE -ne 0) { throw 'Plan failed.' }
# Review the complete plan, then approve each first-run command explicitly.
go run ./cmd/gh-repospec apply --file $config --repo "$owner/$target" --yes --approve-command apm-init
if ($LASTEXITCODE -ne 0) { throw 'Apply failed.' }
```

## Verify the result

1. Confirm the target has a default branch and the initial commit contains `README.md`, `.gitignore`, and the generated `apm.yml`.
2. Confirm the initialization command's completion marker, `.gh-repospec/command-state.json`, was committed and lists `apm-init`. Run `plan` and `apply` a second time without approving the command; the command must be reported as already completed and must not execute again.
3. Run `plan` with the source import against the destination again. It must have no blocked entry. The expected remaining entries are:
   - `ruleset.<name>` and `ruleset.<name>.bypassActors` as UPDATE for every ruleset, because rulesets are always written rather than compared.
   - `actions.forkPullRequestApproval` as UPDATE, because it is always written rather than compared.
   - `settings.collaborator.<login>` as CREATE for each collaborator whose invitation has not been accepted yet; GitHub lists a pending invitee as an invitation, not as a collaborator.
   - `actions.oidcSubjectCustomization.useImmutableSubject` as MANUAL when the source declares `useImmutableSubject: false`. GitHub enables the immutable subject claim for every repository created or renamed after 2026-07-15 and keeps it on.
   - MANUAL entries for `seed` and each command.
4. Import the destination into a separate file and inspect its normalized settings. Compare source and destination after accounting for the repository identity and the documented exclusions below. Then run `plan` with the destination import against the destination. It must have no blocked entry, and its only creates or updates must be the ruleset and `actions.forkPullRequestApproval` entries listed in step 3. Because those are always reported, `plan --ci` exits `2` for this configuration, so its exit code cannot be the acceptance criterion; compare the listed entries instead. Secret values are excluded from this pass.
5. In GitHub Settings, inspect General, Rules, Branches, Actions, Environments, Security, Pages, Webhooks, Deploy keys, and Manage access. Cross-check the corresponding API snapshot with `gh api` as shown below.

```sh
gh api "repos/$OWNER/$TARGET"
gh api "repos/$OWNER/$TARGET/interaction-limits"
gh api "repos/$OWNER/$TARGET/interaction-limits/pulls/creation-cap"
gh api "repos/$OWNER/$TARGET/interaction-limits/pulls/bypass-list"
gh api --paginate "repos/$OWNER/$TARGET/rulesets"
gh api "repos/$OWNER/$TARGET/actions/permissions"
gh api "repos/$OWNER/$TARGET/actions/permissions/workflow"
gh api "repos/$OWNER/$TARGET/actions/permissions/selected-actions"
gh api "repos/$OWNER/$TARGET/actions/oidc/customization/sub"
gh api "repos/$OWNER/$TARGET/actions/secrets"
gh api "repos/$OWNER/$TARGET/actions/variables"
gh api "repos/$OWNER/$TARGET/agents/secrets"
gh api "repos/$OWNER/$TARGET/agents/variables"
gh api --paginate "repos/$OWNER/$TARGET/environments"
gh api --paginate "repos/$OWNER/$TARGET/environments/production/deployment_protection_rules"
gh api --paginate "repos/$OWNER/$TARGET/environments/production/deployment_protection_rules/apps"
gh api "repos/$OWNER/$TARGET/environments/production/secrets"
gh api "repos/$OWNER/$TARGET/pages"
gh api --paginate "repos/$OWNER/$TARGET/hooks"
gh api --paginate "repos/$OWNER/$TARGET/autolinks"
gh api --paginate "repos/$OWNER/$TARGET/keys"
gh api --paginate "repos/$OWNER/$TARGET/collaborators?affiliation=direct"
gh api --paginate "repos/$OWNER/$TARGET/teams"
gh api "repos/$OWNER/$TARGET/properties/values"
gh api --paginate "repos/$OWNER/$TARGET/branches?protected=true"
gh api "repos/$OWNER/$TARGET/immutable-releases"
gh api "repos/$OWNER/$TARGET/vulnerability-alerts"
gh api "repos/$OWNER/$TARGET/automated-security-fixes"
gh api "repos/$OWNER/$TARGET/private-vulnerability-reporting"
gh api "repos/$OWNER/$TARGET/code-scanning/default-setup"
gh api "repos/$OWNER/$TARGET/code-scanning/ai-scan"
gh api --paginate "repos/$OWNER/$TARGET/labels"
gh api --paginate "repos/$OWNER/$TARGET/milestones?state=all"
```

On PowerShell 7, run the same read-only API checks with the variables from the PowerShell section:

```powershell
$apiPaths = @(
    "repos/$owner/$target",
    "repos/$owner/$target/interaction-limits",
    "repos/$owner/$target/interaction-limits/pulls/creation-cap",
    "repos/$owner/$target/interaction-limits/pulls/bypass-list",
    "repos/$owner/$target/rulesets",
    "repos/$owner/$target/actions/permissions",
    "repos/$owner/$target/actions/permissions/workflow",
    "repos/$owner/$target/actions/permissions/selected-actions",
    "repos/$owner/$target/actions/oidc/customization/sub",
    "repos/$owner/$target/environments",
    "repos/$owner/$target/environments/production/deployment_protection_rules",
    "repos/$owner/$target/environments/production/deployment_protection_rules/apps",
    "repos/$owner/$target/pages",
    "repos/$owner/$target/hooks",
    "repos/$owner/$target/autolinks",
    "repos/$owner/$target/keys",
    "repos/$owner/$target/collaborators?affiliation=direct",
    "repos/$owner/$target/teams",
    "repos/$owner/$target/properties/values",
    "repos/$owner/$target/branches?protected=true",
    "repos/$owner/$target/immutable-releases",
    "repos/$owner/$target/vulnerability-alerts",
    "repos/$owner/$target/automated-security-fixes",
    "repos/$owner/$target/private-vulnerability-reporting",
    "repos/$owner/$target/code-scanning/default-setup",
    "repos/$owner/$target/code-scanning/ai-scan",
    "repos/$owner/$target/labels",
    "repos/$owner/$target/milestones?state=all"
)
foreach ($apiPath in $apiPaths) {
    gh api --paginate $apiPath
    if ($LASTEXITCODE -ne 0) { throw "GitHub API read failed: $apiPath" }
}
```

For every protected branch returned by the last branch query, inspect its classic branch protection and required-signature status. A 404 for classic protection can mean that the branch is protected by a ruleset instead; compare that rule through the rulesets endpoint.

## API coverage matrix

| GitHub settings area | Import / apply coverage | Notes |
| --- | --- | --- |
| Repository general settings | `GET`/`PATCH repos/{owner}/{repo}` fields represented by `RepositoryConfig`, including visibility, description, homepage, default branch, features, merge policy, archive, and topics | Repository identity is selected by the import source and can be overridden for apply. `has_downloads` is read and imported; GitHub does not expose an update-existing field for it. |
| Immutable releases | Dedicated immutable-releases read and write endpoints | A 404 from the read endpoint means immutability is disabled. |
| Repository rulesets | List and detail reads; create/update/delete convergence for supported rules and bypass actors | Ruleset permissions and GitHub plan availability still apply. |
| Classic branch protection | Protected branch listing, protection read/write, and required-signature read/write | Ruleset-only branches are represented by rulesets, not classic branch-protection records. |
| Actions and environments | Repository Actions policy, selected-actions allowlist, OIDC subject customization, workflow token permissions, fork approval policy, workflow repository access, environment protection, custom branch patterns, installed custom-app protection rules, variables, and secret metadata | Secret values are intentionally outside the import and parity test. A custom protection app must be available to the destination repository. The fork approval endpoint is unavailable for a private repository and the workflow repository access endpoint for a public one; each is reported as a diagnostic. An immutable OIDC subject that GitHub keeps enabled is reported as manual. |
| Security | `security_and_analysis`, Dependabot alerts and security updates, private vulnerability reporting, CodeQL default setup, and AI pull-request scan where GitHub exposes a readable value | Dependency-graph status has no read endpoint. Unavailable or permission-denied values must appear as diagnostics or blocked plan entries, not as a confirmed false value. |
| Pages | Site enabled state, build type, source, custom domain, and HTTPS enforcement | Domain verification and build/deployment history are not repository configuration. |
| Webhooks | URL, event list, active state, content type, and TLS verification setting | GitHub never returns webhook secret values; they are excluded. |
| Access | Direct/effective collaborator permissions, organization team permissions, and deploy keys | GitHub's collaborator response does not reliably identify whether access came from a direct, team, organization, or enterprise grant. Import emits a diagnostic. Team access is only available for organization-owned repositories. |
| Autolinks | Key prefix, URL template, and alphanumeric-key behavior | GitHub exposes create/delete rather than a dedicated update operation. |
| Custom properties | Repository-level property values | Organization property definitions and rules are organization-wide and are not repository settings. GitHub permissions may prevent reading or setting these values. |
| Repository secrets | Secret names/metadata may be reported by the existing secret importer | Secret values cannot be read from GitHub and are explicitly excluded. Supply values independently if a later apply needs to set them. |
| Interactions | Interaction restriction type, PR creation cap, and PR cap bypass users | GitHub returns an absolute expiry timestamp but accepts only relative expiry presets. The importer rounds the remaining time up to the next supported preset and emits a diagnostic; exact expiry parity is impossible. User- or organization-inherited restrictions cannot be changed at repository scope. |

This matrix describes repository-level settings represented by the current schema. It does not claim that every GitHub REST operation is a repository setting: content, releases, deployments, workflow runs, app installation management, and organization-wide policy are separate resources. An import is not considered complete if a required repository setting is hidden by permissions or reported only as unavailable.

## Results to record

- Source import completed, with diagnostics saved.
- Destination `plan` reviewed before apply.
- Initial commit, default branch, README, and VisualStudio `.gitignore` verified.
- One-time APM initialization ran once; second apply did not run it again.
- Settings-page and `gh api` comparisons completed.
- Destination re-import and second plan showed only the expected entries listed in step 4.
- Every diagnostic or blocked item was resolved or recorded as an explicit exclusion.
