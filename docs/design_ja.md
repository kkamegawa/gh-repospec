# 設計: gh-repobootstrapper

## 目的

`gh-repobootstrapper` は、宣言的なbootstrapファイルに基づきGitHub.comの
リポジトリを収束させる、プリコンパイル版GitHub CLI拡張です。バージョン1は
個人リポジトリおよびFree、Pro、Team組織を対象とします。GitHub Enterprise
Serverは拒否し、Enterprise Cloudはサポート対象外です。

拡張は認証済みGitHub CLIセッションを使用し、GitHubトークンを保存・表示しません。
リポジトリへの変更は宣言的かつ追加的です。設定に含まれないフィールド、ファイル、
rulesetは削除しません。

`cmd/gh-repobootstrapper`の実行入口は標準入出力を`internal/cli`へ渡すだけにし、
コマンド実装を分離します。製品パッケージはルートの`go test ./...`と`go vet ./...`で
検証し、生成されたAPM依存ツリーは`apm_modules/go.mod`で分離します。

## 設定契約

公開設定形式は`schemaVersion: 1`であり、
[`schema/repobootstrapper.schema.json`](../schema/repobootstrapper.schema.json)
で定義します。YAMLとJSONを受け付けます。解析は厳密で、未知フィールド、重複した
mapping key、未対応のenum値、複数ドキュメントを拒否します。環境変数は展開しません。

トップレベルのセクションは`repository`、`seed`、`actions`、`rulesets`、
`security`、`agents`、`commands`です。visibilityは
`allowVisibilityChange`がtrueの場合のみ変更します。seedはtemplate、directory、
ZIPのいずれか一つを選択します。既存ファイルとの内容差異は、対象のリポジトリ相対
パスが`seed.overwrite`に含まれる場合だけ上書きできます。

secretには値ではなく`sourceEnv`を指定します。`updatePolicy`の既定は
`createOnly`で、`always`を明示した場合のみ更新します。commandはプログラムとargv配列
で指定します。shellプログラムは拒否し、パスはリポジトリ相対、timeoutは`5m`などGoの
duration構文を使用します。

## 実行モデル

`validate`は解析と検証のみを行います。`plan`は書き込みなしの変更レポートを作成し、
`apply`はレポートを再計算して`--yes`がなければ確認後に適用します。commandは`--yes`
が指定されていても、`--approve-command <id>`で個別承認が必要です。

書き込み前のpreflightでは、GitHub.com host、認証・権限、リポジトリ状態、機能の利用可否、
組織ポリシー、seed source、必須secret環境変数を確認します。プラン依存の未対応機能は
`optional: true`の場合のみskipでき、権限不足は常に停止します。

適用順は、リポジトリ作成またはvisibility、secret scanningとpush protection、ファイル、
default branch、repositoryとActions設定、secretとvariable、残りのsecurity設定、ruleset
です。初期コミットを妨げないようrulesetは最後に適用します。全体はトランザクションでは
ありません。部分失敗はレポートし、後続のapplyで要求状態へ収束させます。

## source、agent、制限事項

新規template repositoryにはGitHubのtemplate-generation APIを使用します。directoryとZIPは
ローカルでstageし、ZIP展開時にはpath traversal、symbolic link、`.git`、サイズ濫用を
拒否します。変更はGit Database APIで1コミットとしてアップロードし、既存リポジトリでは
現在のdefault branch treeを使用してrefをforce更新しません。

`.github/mcp.json`、instructions、custom agent、skill、setup workflowなどのagentファイルは
seed内容です。Copilot MCP serverのrepository設定、Copilot license/seat、組織ポリシー、
継承した組織rulesetには、対応するrepository-level自動化経路がなく、手動作業として
レポートします。commandプロセスはOS sandboxではありません。ツールは承認済み実行ファイル、
argv、repository workspace、出力上限、timeout、環境変数allowlistに制限します。
