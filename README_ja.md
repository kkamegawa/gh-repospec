# gh-repospec

`gh-repospec` は、YAML または JSON の宣言ファイルに基づいて GitHub.com リポジトリを収束させる、プリコンパイル済み GitHub CLI 拡張機能です。現在の GitHub CLI ログインを利用し、トークンを保存・表示しません。

[English](README.md)

## 対象範囲

リポジトリ設定のバージョン1と2は、GitHub.com 上の個人リポジトリと GitHub Free、Pro、Team の組織を対象にします。バージョン2では個人アカウントの Issue types と Issue fields にも対応します。GitHub Enterprise Server は拒否し、Enterprise Cloud はサポート対象外です。

## インストール

公開済みリリースをインストールします。

```text
gh extension install kkamegawa/gh-repospec
```

ローカル開発では、リポジトリルートへ実行ファイルをビルドしてから、ルートを拡張機能としてインストールします。`gh extension install` はディレクトリ名を拡張機能名として扱うため、チェックアウト先のディレクトリ名は `gh-repospec` で、同名の実行ファイルが直下に必要です。

```bash
go build -o gh-repospec ./cmd/gh-repospec
gh extension install .
```

```powershell
go build -o gh-repospec.exe ./cmd/gh-repospec
gh extension install .
```

`plan` または `apply` の前に、GitHub.com に対して GitHub CLI へログインしてください。

## クイックスタート

[`docs/examples/bootstrap.yaml`](docs/examples/bootstrap.yaml) をコピーして編集します。例にはプレースホルダーを使っているため、対象リポジトリと seed source を置き換えてください。[`docs/examples/bootstrap-extended.yaml`](docs/examples/bootstrap-extended.yaml) は、仕様レビューで追加した label、milestone、bypass actor、repository 設定の例です。

```text
gh repospec validate --file bootstrap.yaml
gh repospec plan --file bootstrap.yaml
gh repospec apply --file bootstrap.yaml
```

既存リポジトリから始める場合は、`import` が現在の設定を設定ファイルとして書き出します。手作業で書き写す必要はありません。

```text
gh repospec import OWNER/REPOSITORY > bootstrap.yaml
```

`plan` は読み取り専用です。`apply` はプランを再計算・表示し、`--yes` がない限り確認を求めます。設定済み command は `--yes` の有無にかかわらず、個別の `--approve-command <id>` が必要です。

## 設定

リポジトリ設定は `schemaVersion: 1` と `schemaVersion: 2` を受け付けます。バージョン2では `scope: repository` が必須で、バージョン1と同じリポジトリ設定を記述します。リポジトリの新規 import は既定でバージョン1を出力します。YAML と JSON を受け付け、JSON Schema は [`schema/repospec.schema.json`](schema/repospec.schema.json) にあります。

未知フィールド、YAML の重複キー、不正な enum 値、複数ドキュメント、暗黙的な環境変数展開は拒否します。相対的な directory / ZIP seed のパスは、設定ファイルからの相対パスです。

| セクション | 役割 |
| --- | --- |
| `repository` | 対象、作成、可視性、説明、トピック、既定 branch、merge 設定。 |
| `seed` | `template`、`directory`、`zip` のいずれか 1 つ。異なる既存ファイルは `overwrite` への明示が必要。 |
| `actions` | GitHub Actions の権限、variables、secrets、environments。`allowedActions: selected` には `selectedActions` が必須(未設定だと全Actionがブロックされる)。 |
| `rulesets` | 名前で識別する repository ruleset。旧 branch protection は `settings.branchProtections` で別に宣言する。 |
| `security` | dependency graph、Dependabot、secret scanning、push protection、CodeQL default setup、AI Scan for pull requests(`aiPullRequestScan`)、private vulnerability reporting。`aiPullRequestScan` は GitHub のパブリックプレビューで、GitHub Advanced Security、Copilot、CodeQL default setup が必要なため `optional: true` を推奨。 |
| `agents` | seed が提供する MCP、instructions、agents、skills の repository-relative path と agent variables / secrets。 |
| `labels` | issue と pull request の label。追加的で、設定にない label は削除しません。 |
| `milestones` | milestone。追加的で、廃止は削除ではなく `state: closed` で表します。 |
| `settings` | Pages、webhook、autolink、deploy key、collaborator、team、custom property の値、旧 branch protection、interaction limit、pull request の作成上限、Copilot Code Review。branch protection の `settings` は GitHub の更新 API のフィールド名で記述し、`import` もその形式で書き出す。 |
| `commands` | `apm init` などの、明示承認された argv 形式のローカル初期化コマンド。 |

secret に記載するのは値ではなく `sourceEnv` 名です。既定の更新方針は `createOnly` であり、意図的なローテーション時だけ `always` を指定します。

### Copilot Code Review

`settings.copilotCodeReview` で `reviewEffort`、`customInstructionsEnabled`、`mcpEnabled`、`severityFilter` を管理します。省略した項目は管理対象外です。`reviewEffort` には対象リポジトリで利用できる値を指定します。取得できる上書き値を解除するには `inherit` を指定します。重大度フィルターには `no_filter`、`low`、`low_and_medium` を指定できます。自動レビューの依頼は `copilot_code_review` の branch ruleset で設定します。

- **ローカルの GitHub CLI。** リポジトリ管理者として `gh auth login` を実行します。OAuth トークンまたは classic PAT には `repo` スコープが必要です。
- **読み取り専用 CI（`plan` と `import`）。** リポジトリの **Administration: read** 権限を持つ GitHub App トークンまたは fine-grained PAT を `GH_TOKEN` で CLI に渡します。
- **適用用 CI。** リポジトリの **Administration: write** 権限を持つ GitHub App トークンまたは fine-grained PAT を `GH_TOKEN` で渡します。同じ設定ファイルの他の項目に必要な権限も付与してください。

GitHub Actions の workflow `permissions` では、組み込みの `GITHUB_TOKEN` に **Administration** 権限を付与できません。

### 個人アカウントの Issue 設定

Issue types と Issue fields は GitHub の個人アカウントに属し、そのアカウントが所有するリポジトリで利用できます。これらは別の個人用ファイルに置き、`schemaVersion: 2`、`scope: personal`、アカウントのログイン名を指定します。`issueTypes` と `issueFields` に選択肢を含めた定義を記述します。フィールドのピン留めと表示順は GitHub の設定で管理します。詳細は[スキーマ v2 の設計](docs/architecture/schema-v2_ja.md)を参照してください。

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

`personal plan`、`personal apply`、`personal import` は認証中のユーザーのアカウント共通定義を管理します。`account` は GitHub CLI のログインと一致する必要があります。個人設定を適用すると、そのアカウントが所有する全リポジトリの Issue に反映されます。import と plan には個人アカウントの Issue Types と Issue Fields API へのアクセス権が必要です。クラシックトークンまたは OAuth の場合、読み取りには `read:user`、書き込みには `user` スコープを使います。個人用フィールドの更新では、未宣言の選択肢と既存の選択肢 ID を保持します。

## コマンド

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

`-R` / `--repo` は設定ファイル内の対象リポジトリを上書きします。`--approve-command` は繰り返し指定でき、設定済み command ごとに必須です。`--version` / `-v` は `version` の別名として使用できます。

`plan --ci` は、スケジュール実行の workflow 向けにプランをドリフト検査へ変えます。`apply --force-secrets` はその実行に限りすべての設定済み secret を再設定するため、ファイルを編集せずに資格情報をローテーションできます。リポジトリの新規 import は既定でバージョン1を出力し、`--schema-version 2` で v2 を選べます。`--into` を指定したときは更新先のバージョンに従い、宣言済みの項目だけを更新して YAML のコメントとキー順を保ちます。個人用コマンドは別のアカウント設定ファイルと個人の Issue 定義 API を使い、個人用 import はバージョン2を書き出します。secret の値は GitHub API から取得できないため、リポジトリの `import` は値を作らず secret 名を報告します。

| 終了コード | 意味 |
| --- | --- |
| `0` | 成功。`plan --ci` では、blocked な項目がなく、ツールが収束できる差分もないことを表します。secret の更新と手動項目は比較対象から除外するため、すべての設定が一致している証明にはなりません。 |
| `1` | コマンドが失敗しました。`--ci` では blocked な plan も含み、blocked はドリフト検出に優先します。 |
| `2` | `plan --ci` が収束できる差分を 1 件以上検出しました。secret の更新は比較対象から除外します。 |

[`docs/examples/workflows`](docs/examples/workflows) に、マージ時に apply する workflow と、スケジュール実行でドリフトを検出する workflow があります。

### 短縮呼び出し

`gh alias set` で個人用の短縮名を作成できます。

```text
gh alias set rb repospec
gh rb plan --file bootstrap.yaml
```

上記の例はどちらも引用符が不要です。expansion にスペースや引数が含まれる場合は引用符が必要で、その際 Windows の Command Prompt ではシングルクォートではなくダブルクォートを使ってください。

## 安全性

- 宣言した設定、ファイル、ruleset だけを収束させます。宣言していないリソースは削除しません。
- 既存リポジトリの可視性変更には `repository.allowVisibilityChange: true` が必要です。
- repository の初期化を妨げないよう、ruleset は最後に適用します。
- ZIP seed は path traversal、symbolic link、`.git`、過大な展開を拒否します。
- command はプログラムと引数配列で実行し、shell プログラムは拒否します。OS sandbox ではないため、信頼できるプログラムだけを承認してください。
- 処理全体はトランザクションではありません。失敗時は部分状態を報告するため、問題を解消後に再実行して収束させます。

## 手動作業となる制限

Copilot MCP サーバーは、instructions、custom agents、skills、setup workflow と同様に `.github/mcp.json` などの seed コンテンツで定義します。`settings.copilotCodeReview.mcpEnabled` はコードレビューで MCP サーバーを利用するかを制御します。Copilot のライセンスやシート、組織ポリシー、継承された組織 ruleset は手動で設定します。プランに依存する機能は `optional: true` のときだけスキップできます。Copilot Code Review の設定を取得できない場合、`plan` は処理をブロックし、`import` は HTTP 404 またはレート制限以外の HTTP 403 に対して診断を表示します。

## 開発

Go 1.26 以降を使用します。

APM依存ディレクトリはネストしたGo moduleとして分離しています。生成されたskillのサンプルコードが製品moduleの`go test ./...`および`go vet ./...`の対象にならないようにするためです。

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

`plan` や `apply` でエラーが発生した場合は、よくある原因と対処をまとめた [docs/troubleshooting_ja.md](docs/troubleshooting_ja.md) を参照してください。import した設定を別のリポジトリへ適用する場合の注意事項も記載しています。

貢献手順は [CONTRIBUTING_ja.md](CONTRIBUTING_ja.md)、リポジトリ設計は [docs/design_ja.md](docs/design_ja.md)、個人設定とリポジトリ設定のスキーマ v2 は [docs/architecture/schema-v2_ja.md](docs/architecture/schema-v2_ja.md) を参照してください。`babarot/gh-infra` との機能比較と、本拡張が意図的に行わない範囲は [docs/gh-infra-parity_ja.md](docs/gh-infra-parity_ja.md) に記録しています。

## ライセンス

[MIT License](LICENSE) の下で配布します。
