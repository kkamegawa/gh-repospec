# リポジトリ設定の手動import・一致確認

この手順では2つのGitHubリポジトリに対して`import`、`plan`、`apply`を実行し、現在のrepospecスキーマが扱う設定を検証します。GitHub APIが返さない値や対象外の状態も明記します。認証情報とリポジトリ状態はこの文書に記録しません。

## 前提条件

- GoとGitHub CLI (`gh`) がインストールされていること。
- `gh auth status`が成功し、2つのリポジトリを管理できるアカウントであること。Organization所有のリポジトリでは、team accessとcustom propertiesを管理する権限も必要です。
- interaction制限とPR bypass listには対応するadmin/maintainer権限が必要です。parity合格前にsourceとdestinationの全API読取が成功することを確認してください。
- sourceは`kkamegawa/Relaypublisher`、destinationは`kkamegawa/repotest1`です。
- destinationにはコミットがないこと。applyが初回の`main`ブランチとコミットを作成します。
- apply前にplan全体を確認してください。`apply --yes`は確認プロンプトを省略しますが、blocked状態のplanは適用しません。

## ローカル回帰テスト

ライブのリポジトリテスト前に、次のテストを実行します。GitHub APIはメモリ上のfakeで、subprocess helperもローカルで動くため、GitHubには書き込みません。

```sh
go test ./internal/planner -run '^TestEmptyRepositoryInitializationAndCommandsRunOnce$' -count=1 -v
```

初回applyで`README.md`とVisualStudioの`.gitignore`を作成し、`main`を初期化し、決定的なhelper commandが書いた`apm.yml`をcommitすること、command完了状態を記録すること、2回目のapplyでcommandを再実行しないことを確認します。helperはRelaypublisherのtargets・dependenciesを持つ`name: repotest1`の固定ファイルを書きます。この内容はhelperのものであり、`apm init`の出力ではありません。

同じpackageの`TestFailedCommandReportsRedactedOutput`は、失敗したcommandの出力が報告されるエラーに含まれ、宣言されたsecretの値が`***`に置き換えられることを確認します。

全自動テストは次のコマンドです。

```sh
go test ./...
```

## ライブimport・plan・apply

BashとPowerShell 7の例を示します。どちらもローカルに設定ファイルを作成します。import後に次の`seed.initialize`を追加してからvalidateしてください。

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

環境変数allowlistは厳密です。`apm`は`git`を起動し、ホームディレクトリを読むため、`PATH`とホームディレクトリの変数(macOSとLinuxでは`HOME`、Windowsでは`USERPROFILE`)が必要です。列挙した変数が未設定ならスキップされるため、同じリストをすべてのプラットフォームで使えます。`APM_NONINTERACTIVE`だけでは`apm`が`git`を見つけられずに失敗し、`apply`はエラーの下にcommandの出力を表示します。

初回applyには`--approve-command apm-init`を指定します。`apm init --yes`はstaging workspaceに既定の`apm.yml`を作成します。Relaypublisherの`targets`や`dependencies`はコピーせず、プロジェクト名はdestinationリポジトリではなくstaging directoryの名前(`name: seed`)になります。初回commitに`apm.yml`が存在することを確認し、Relaypublisherのtargets・dependenciesが必要な場合は後続のcommitで内容を編集します。

source importは標準エラーにdiagnosticを出力します。publicなsourceでは、GitHubがworkflow accessポリシーをprivateとinternalのリポジトリにのみ適用するため`actions.workflowAccessLevel`が含まれ、secret値は返されないためsecret名ごとに1行が含まれます。

### Bash

```bash
set -euo pipefail
OWNER=kkamegawa
SOURCE=Relaypublisher
TARGET=repotest1
CONFIG=repotest1.yaml

gh auth status
go run ./cmd/gh-repospec import "$OWNER/$SOURCE" --format yaml > "$CONFIG"
# $CONFIGを編集し、上記のseed.initializeを追加します。
go run ./cmd/gh-repospec validate --file "$CONFIG"
go run ./cmd/gh-repospec plan --file "$CONFIG" --repo "$OWNER/$TARGET"
# plan全体を確認し、初回commandを明示的に承認します。
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
# $configを編集し、上記のseed.initializeを追加します。
go run ./cmd/gh-repospec validate --file $config
if ($LASTEXITCODE -ne 0) { throw 'Configuration validation failed.' }
go run ./cmd/gh-repospec plan --file $config --repo "$owner/$target"
if ($LASTEXITCODE -ne 0) { throw 'Plan failed.' }
# plan全体を確認し、初回commandを明示的に承認します。
go run ./cmd/gh-repospec apply --file $config --repo "$owner/$target" --yes --approve-command apm-init
if ($LASTEXITCODE -ne 0) { throw 'Apply failed.' }
```

## 結果の確認

1. targetにdefault branchがあり、初回commitに`README.md`、`.gitignore`、生成した`apm.yml`が含まれることを確認します。
2. 初期化commandの完了marker `.gh-repospec/command-state.json`がcommitされ、`apm-init`を含むことを確認します。2回目の`plan`と`apply`ではcommandを承認せずに実行し、完了済みとして表示されて再実行されないことを確認します。
3. source importを使ってdestinationに対する`plan`を再度実行します。blockedなエントリがないことを確認します。残る想定のエントリは次のとおりです。
   - すべてのrulesetの`ruleset.<name>`と`ruleset.<name>.bypassActors`がUPDATE。rulesetは比較せず常に書き込むためです。
   - `actions.forkPullRequestApproval`がUPDATE。比較せず常に書き込むためです。
   - 招待がまだ承諾されていないcollaboratorごとの`settings.collaborator.<login>`がCREATE。GitHubは承諾待ちの招待相手をcollaboratorではなく招待として返します。
   - sourceが`useImmutableSubject: false`を宣言している場合の`actions.oidcSubjectCustomization.useImmutableSubject`がMANUAL。GitHubは2026-07-15以降に作成または名前変更されたすべてのリポジトリで不変のsubject claimを有効にし、有効のまま維持します。
   - `seed`がMANUAL。実行済みのcommandはNOOP(完了済み)で、まだ実行していないcommandだけが承認されるまでMANUALです。
4. destinationを別ファイルにimportし、正規化された設定を確認します。repository identityと下記の明示的な対象外を考慮してsourceと比較します。続いてdestinationのimport結果を使ってdestinationに対する`plan`を実行します。blockedなエントリがなく、createまたはupdateが手順3のrulesetと`actions.forkPullRequestApproval`のエントリだけであることを確認します。これらは常に報告されるため、この設定では`plan --ci`が`2`で終了します。終了コードは合否の基準にできないため、列挙したエントリと比較します。secret値はこの比較から除外します。
5. GitHub SettingsのGeneral、Rules、Branches、Actions、Environments、Security、Pages、Webhooks、Deploy keys、Manage accessを確認し、以下の`gh api`のsnapshotと照合します。

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

PowerShell 7では、PowerShell sectionで設定した変数を使って同じread-only API checkを実行できます。

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

最後のbranch一覧に含まれる各ブランチでclassic branch protectionとrequired-signature状態を確認します。classic protectionの404は、rulesetだけで保護されている場合もあります。その場合はrulesets endpointの内容と照合します。

## APIカバレッジ表

| GitHubの設定領域 | import / applyの対象 | 注意点 |
| --- | --- | --- |
| Repository一般設定 | `GET`/`PATCH repos/{owner}/{repo}`のうち`RepositoryConfig`が表現するvisibility、description、homepage、default branch、feature、merge policy、archive、topics | repository identityはimport元で決まり、apply時に上書きできます。`has_downloads`は取得・importしますが、GitHub APIには既存repoを更新するフィールドがありません。 |
| Immutable releases | 専用endpointで取得・更新 | 読み取りの404はimmutable releasesが無効であることを示します。 |
| Repository rulesets | list/detail取得、対応するruleとbypass actorのcreate/update/delete | GitHubの権限とプランによる制約があります。 |
| Classic branch protection | protected branch一覧、protection取得・更新、署名必須設定の取得・更新 | rulesetだけで保護されたbranchはclassic protectionではなくrulesetsにimportします。 |
| ActionsとEnvironments | repository Actions policy、selected-actions許可リスト、OIDC subject customization、workflow token権限、fork承認policy、workflow repository access、environment protection、custom branch pattern、インストール済みcustom app protection rule、variables、secret metadata | secret値は意図的にimportと一致確認の対象外です。custom protection appはdestination repoで利用可能である必要があります。private repositoryではfork承認endpoint、public repositoryではworkflow repository access endpointが使えないため、それぞれdiagnosticになります。GitHubが有効のまま維持する不変のOIDC subjectはmanualとして報告します。 |
| Security | `security_and_analysis`、Dependabot alerts/security updates、private vulnerability reporting、CodeQL default setup、GitHubが値を公開する場合のAI pull-request scan | dependency graphの状態にはread endpointがありません。値が取得不能・権限不足の場合、falseと断定せずdiagnosticまたはblocked planにします。 |
| Pages | 有効状態、build type、source、custom domain、HTTPS enforcement | domain verificationとbuild/deployment履歴はrepository設定ではありません。 |
| Webhooks | URL、event一覧、active状態、content type、TLS検証設定 | GitHubはwebhook secret値を返さないため除外します。 |
| Access | direct/effective collaborator権限、organization team権限、deploy key | collaborator APIの返却値だけではdirect・team・organization・enterpriseのどのgrantかを特定できない場合があり、import時にdiagnosticを出します。team accessはOrganization所有repoのみ取得できます。 |
| Autolinks | key prefix、URL template、英数字keyの設定 | GitHub APIは専用updateではなくcreate/deleteを提供します。 |
| Custom properties | repository-level property value | organization-wide property定義やruleはorganization設定で、repository設定ではありません。権限により読み書きできないことがあります。 |
| Repository secrets | 既存secret importerがsecret名・metadataを報告する場合があります | GitHub APIからsecret値は取得できず、明示的に対象外です。後のapplyで設定する必要がある値は別途供給します。 |
| Interactions | interaction制限の種類、PR作成上限、PR上限のbypass user | GitHubは絶対時刻の有効期限を返しますが、書き込みには相対期限presetのみ指定できます。残り時間を次の長いpresetに切り上げdiagnosticを出すため、有効期限の厳密な一致はできません。UserまたはOrganizationから継承した制限はrepository単位で変更できません。 |

この表は現在のスキーマが表現するrepository-level設定を示します。REST APIの全操作をrepository設定とは見なしません。contents、release、deployment、workflow run、app installation管理、organization-wide policyは別リソースです。権限に隠れた必須設定や取得不能として報告された設定が残るimportは完了扱いにしません。

## 記録する結果

- source importが完了し、diagnosticを保存した。
- apply前にdestinationのplan全体を確認した。
- 初回commit、default branch、README、VisualStudio `.gitignore`を確認した。
- APM初期化が一度だけ実行され、2回目のapplyで再実行されなかった。
- Settings画面と`gh api`の比較を完了した。
- destinationの再importと2回目のplanが、手順4に列挙した想定のエントリだけを示した。
- diagnosticまたはblocked項目を解決したか、明示した対象外として記録した。
