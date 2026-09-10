# gh-repospec

`gh-repospec` は、YAML または JSON の宣言ファイルに基づいて GitHub.com リポジトリを収束させる、プリコンパイル済み GitHub CLI 拡張機能です。現在の GitHub CLI ログインを利用し、トークンを保存・表示しません。

[English](README.md)

## 対象範囲

Version 1 は GitHub.com 上の個人リポジトリと GitHub Free、Pro、Team の組織を対象にします。GitHub Enterprise Server は拒否し、Enterprise Cloud はサポート対象外です。

## インストール

公開済みリリースをインストールします。

```text
gh extension install kkamegawa/gh-repospec
```

ローカル開発では、`bin` 配下へ実行ファイルをビルドしてから、そのディレクトリを拡張機能としてインストールします。

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

公開設定契約は `schemaVersion: 1` です。YAML と JSON を受け付け、JSON Schema は [`schema/repospec.schema.json`](schema/repospec.schema.json) にあります。

未知フィールド、YAML の重複キー、不正な enum 値、複数ドキュメント、暗黙的な環境変数展開は拒否します。相対的な directory / ZIP seed のパスは、設定ファイルからの相対パスです。

| セクション | 役割 |
| --- | --- |
| `repository` | 対象、作成、可視性、説明、トピック、既定 branch、merge 設定。 |
| `seed` | `template`、`directory`、`zip` のいずれか 1 つ。異なる既存ファイルは `overwrite` への明示が必要。 |
| `actions` | GitHub Actions の権限、variables、secrets、environments。`allowedActions: selected` には `selectedActions` が必須(未設定だと全Actionがブロックされる)。 |
| `rulesets` | 名前で識別する repository ruleset。旧 branch protection API は使用しない。 |
| `security` | dependency graph、Dependabot、secret scanning、push protection、CodeQL default setup、private vulnerability reporting。 |
| `agents` | seed が提供する MCP、instructions、agents、skills の repository-relative path と agent variables / secrets。 |
| `labels` | issue と pull request の label。追加的で、設定にない label は削除しません。 |
| `milestones` | milestone。追加的で、廃止は削除ではなく `state: closed` で表します。 |
| `commands` | `apm init` などの、明示承認された argv 形式のローカル初期化コマンド。 |

secret に記載するのは値ではなく `sourceEnv` 名です。既定の更新方針は `createOnly` であり、意図的なローテーション時だけ `always` を指定します。

## コマンド

```text
gh repospec validate --file <path>
gh repospec plan --file <path> [-R owner/repository] [--format text|json] [--ci]
gh repospec apply --file <path> [-R owner/repository] [--yes] [--approve-command <id>] [--force-secrets]
gh repospec import <owner/repository> [--format yaml|json] [--into <path> [--yes]]
gh repospec version
```

`-R` / `--repo` は設定ファイル内の対象リポジトリを上書きします。`--approve-command` は繰り返し指定でき、設定済み command ごとに必須です。`--version` / `-v` は `version` の別名として使用できます。

`plan --ci` は、スケジュール実行の workflow 向けにプランをドリフト検査へ変えます。`apply --force-secrets` はその実行に限りすべての設定済み secret を再設定するため、ファイルを編集せずに資格情報をローテーションできます。`import` はリポジトリを読み取って設定を書き出し、`--into` を付けると既存ファイルをコメントとキー順を保ったまま更新します。refresh はファイルが既に宣言している範囲だけを触り、リポジトリ側に存在しない宣言を削除しません。その項目は残したまま、未適用として報告します。secret の値は GitHub API から取得できないため、`import` は値を作らず secret 名を報告します。

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

リポジトリ設定画面の Copilot MCP servers、Copilot のライセンス・seat、組織ポリシー、継承された組織 ruleset は自動化できません。`.github/mcp.json`、instructions、custom agents、skills、setup workflow は seed のコンテンツとして管理してください。プラン依存機能は `optional: true` のときだけ skip 可能で、認可不足は常に停止します。

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

貢献手順は [CONTRIBUTING_ja.md](CONTRIBUTING_ja.md)、確定済みの設計は [docs/design_ja.md](docs/design_ja.md) を参照してください。`babarot/gh-infra` との機能比較と、本拡張が意図的に行わない範囲は [docs/gh-infra-parity_ja.md](docs/gh-infra-parity_ja.md) に記録しています。

## ライセンス

[MIT License](LICENSE) の下で配布します。
