# 設計: gh-repospec

## 目的

`gh-repospec` は、宣言的なbootstrapファイルに基づきGitHub.comの
リポジトリを収束させる、プリコンパイル版GitHub CLI拡張です。バージョン1は
個人リポジトリおよびFree、Pro、Team組織を対象とします。GitHub Enterprise
Serverは拒否し、Enterprise Cloudはサポート対象外です。有効なhostはgo-ghの
`auth.DefaultHost`で解決します。これはGitHub CLI自体の優先順位
(`GH_HOST`、次に設定済みhostが1つだけの場合はそれ、最後にgithub.com)と一致し、
リクエスト送信前に`auth.IsEnterprise`で拒否します。`GH_HOST`の単純な参照だけでは、
`GH_HOST`未設定のままEnterprise Serverの1 host限定でログインしているケースを
見逃します。

拡張は認証済みGitHub CLIセッションを使用し、GitHubトークンを保存・表示しません。
リポジトリへの変更は宣言的かつ追加的です。設定に含まれないフィールド、ファイル、
rulesetは削除しません。

`cmd/gh-repospec`の実行入口は標準入出力を`internal/cli`へ渡すだけにし、
コマンド実装を分離します。製品パッケージはルートの`go test ./...`と`go vet ./...`で
検証し、生成されたAPM依存ツリーは`apm_modules/go.mod`で分離します。

## 設定契約

公開設定形式は`schemaVersion: 1`であり、
[`schema/repospec.schema.json`](../schema/repospec.schema.json)
で定義します。YAMLとJSONを受け付けます。解析は厳密で、未知フィールド、重複した
mapping key、未対応のenum値、複数ドキュメントを拒否します。環境変数は展開しません。

トップレベルのセクションは`repository`、`seed`、`actions`、`rulesets`、
`security`、`agents`、`labels`、`milestones`、`commands`です。visibilityは
`allowVisibilityChange`がtrueの場合のみ変更します。seedはtemplate、directory、
ZIPのいずれか一つを選択します。既存ファイルとの内容差異は、対象のリポジトリ相対
パスが`seed.overwrite`に含まれる場合だけ上書きできます。

secretには値ではなく`sourceEnv`を指定します。`updatePolicy`の既定は
`createOnly`で、`always`を明示した場合のみ更新します。commandはプログラムとargv配列
で指定します。shellプログラムは拒否し、パスはリポジトリ相対、timeoutは`5m`などGoの
duration構文を使用します。

`repository`には、内容ではなくリポジトリ自体を表す設定も含みます。`homepage`、
`archived`、issues/projects/wiki/discussionsの`features`切り替え、merge commitの
題名と本文の書式(`mergeCommitTitle`、`mergeCommitMessage`、
`squashMergeCommitTitle`、`squashMergeCommitMessage`)、`releaseImmutability`です。
アーカイブは可逆で、`archived: false`で解除します。

`labels`と`milestones`は、契約全体と同じく追加的なコレクションです。labelは`name`、
先頭の`#`を含まない16進6桁の`color`、任意の`description`を取ります。milestoneは
`title`、任意の`description`、`open`または`closed`の`state`、任意の`dueOn`
(`YYYY-MM-DD`形式)を取ります。nameとtitleはリスト内で一意であり、GitHub上のリソースを
識別します。リポジトリに存在して設定にないlabelとmilestoneは削除しません。milestoneの
廃止は`state: closed`で表します。

`rulesets[].bypassActors`はrulesetを迂回できるactorを列挙します。各エントリは
`role`(`admin`、`write`、`maintain`)、`team`(slug)、`app`(slug)、`orgAdmin`(`true`)、
`customRole`(名前)のいずれか1つを指定し、さらにすべてのエントリで`always`または
`pull_request`の`bypassMode`が必須です。slugと名前は、書き込み前にruleset APIが要求する数値actor IDへ解決します。
解決できないactorはbypassを黙って落とさず、planをブロックします。解決済みの
`id:<number>`形式も受け付けます。これは`import`の出力をそのまま適用し直せるようにする
ためで、認証ユーザーがslugを解決できないprivate GitHub Appで必要になります。

`rulesets[].enforcement`は`evaluate`も受け付けます。GitHubのdry-runモードで、違反を
記録するだけでブロックしません。既に運用中のリポジトリへrulesetを導入する際の安全な
手段です。

`actions.forkPullRequestApproval`は、forkからのworkflow実行に手動承認を要求する条件を
選択します。値は`first_time_contributors_new_to_github`、`first_time_contributors`、
`all_external_contributors`で、`allowedActions`と同じ列挙スタイルに合わせます。

`security.dependabotSecurityUpdates`は、Dependabot alertが実効的に有効であることを
要求します。GitHub APIがそれ以外を拒否するためです。依存関係は`plan`時に設定と
ライブ状態の両方から評価します。同一ファイルで両方を有効にした場合はalertを先に適用し、
`dependabotAlerts`を省略した場合はGitHub上の現在の状態を使い、
`dependabotAlerts.enabled: false`を明示したままsecurity updateを有効にした場合は
blockedとして報告します。

`actions.allowedActions: selected`を指定する場合は`actions.selectedActions`
(`githubOwnedAllowed`、`verifiedAllowed`、任意の`patternsAllowed`)が必須です。
未指定のままだとGitHubのselected-actions allowlistは既定で空になり、Actionを使う
すべてのworkflow実行がブロックされます。検証では`selected`指定時の未設定、および
`allowedActions`が`selected`でない場合の`selectedActions`指定をそれぞれ拒否します。

## 実行モデル

`validate`は解析と検証のみを行います。`plan`は書き込みなしの変更レポートを作成し、
`apply`はレポートを再計算して`--yes`がなければ確認後に適用します。commandは`--yes`
が指定されていても、`--approve-command <id>`で個別承認が必要です。`import`は
リポジトリを読み取って設定を書き出すだけで、リポジトリへは書き込みません。`version`
(`--version`/`-v`も可)は、GitHub CLI拡張の慣習に従い、ビルド時に`-ldflags`で
埋め込んだリリースバージョンを表示します。

終了コードは契約の一部です。CIがこれで分岐するためです。`0`は成功で、`--ci`付きの
`plan`では要求状態が既に収束していることを表します。`1`はコマンドの失敗、`2`は
`plan --ci`が1件以上の差分を検出したことを表します。`apply --force-secrets`は、
その実行に限りすべての設定済みsecretを`updatePolicy: always`として扱います。
ファイルを編集せずに資格情報をローテーションする手段です。

書き込み前のpreflightでは、GitHub.com host、認証・権限、リポジトリ状態、機能の利用可否、
組織ポリシー、seed source、必須secret環境変数を確認します。プラン依存の未対応機能は
`optional: true`の場合のみskipでき、権限不足は常に停止します。

適用順は、リポジトリ作成またはvisibility、secret scanningとpush protection、ファイル、
default branch、labelとmilestoneを含むrepositoryとActions設定、secretとvariable、
残りのsecurity設定、rulesetです。bypass actorの識別子はrulesetの段階より前に解決するため、
解決できない名前は`apply`の途中で失敗せず`plan`が報告します。初期コミットを妨げないよう
rulesetは最後に適用します。全体はトランザクションでは
ありません。部分失敗はレポートし、後続のapplyで要求状態へ収束させます。

`repository.archived`はこの適用順の外側にあります。GitHubはアーカイブ済み
リポジトリへの書き込みを拒否するためです。`archived: false`（解除）は他の
すべての変更より前に最初に適用し、残りの`apply`が書き込み可能なリポジトリに
対して実行できるようにします。`archived: true`（アーカイブ）はrulesetより
後、全体の最後に適用し、アーカイブが同じ実行内の他の変更を妨げないようにします。

## import

`import <owner/repository>`は既存リポジトリのライブ状態を取得し、`schemaVersion: 1`の
文書を書き出します。設定済みリポジトリを手作業で書き写すことなく本拡張の管理下に
置くためです。既定では文書を標準出力へ出力し、`--format`でYAMLとJSONを選択します。
他のコマンドとの一貫性のため、位置引数の代わりに`-R`/`--repo`も使用できます。

取り込むセクションは、`repository`(topic、features、merge設定、archived状態を含む)、
`labels`、`milestones`、bypass actorを含む`rulesets`、`actions`の権限・variable・
environment、`security`です。rulesetのIDやtimestampなどサーバー生成値は書き出しません。
本契約ではruleset nameが安定した識別子だからです。

取り込めないものは3つあり、黙って省略せず報告します。secretの値はGitHub APIが返さないため、
リポジトリ上に存在するsecret名をコメントとして提示し、`sourceEnv`は利用者が指定します。
`seed`と`commands`はリポジトリ状態からは導けないローカルな意図です。`agents`のファイル
パスはseedの構成に依存します。agent variableは取り込み、agent secretの扱いはActions
secretと同じです。

`--into <path>`は同じ取得を行い、新しい文書を出力する代わりに既存の設定ファイルを
その場で更新します。書き換えはYAMLノード編集で行い、コメント、キー順、整形を保持します。
JSON設定はJSONとして再整形します。差分を表示して確認を求めてから書き込み、`--yes`で
確認を省略します。ファイルが宣言していないフィールドは宣言されないままです。`--into`は
管理下にある項目を更新するもので、管理対象を広げるものではありません。

## ドリフト検出と CI

`plan --ci`がドリフト検査です。変更が発生する場合に`2`で終了するため、GitHubのUIから
変更された設定に対して、スケジュール実行のworkflowを合否判定として使えます。
`--format json`でも終了コードは同じです。レポートを保存または通知しつつ、終了コードで
ジョブの結果を決められます。

一般的な構成は2つのworkflowで実現でき、いずれも
[`examples/workflows`](examples/workflows)にあります。default branchへのマージ時に
applyするとライブのリポジトリがファイルへ収束し、スケジュール実行の`plan --ci`が
ツール外の変更を検出します。`apply`のworkflowには`concurrency`グループを設定してください。
本拡張はstateファイルを持たないためロックもなく、同時applyは後勝ちになります。

既定の`GITHUB_TOKEN`は、workflowが動作するリポジトリを管理できます。別のリポジトリを
bootstrapする場合は、Administration、Contents、およびsecretとvariableを扱うなら対応する
読み書き権限を持つfine-grained personal access tokenかGitHub App installation tokenが
必要です。

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

## gh-infraとの関係

[`gh-infra-parity_ja.md`](gh-infra-parity_ja.md)に、`babarot/gh-infra`との機能比較、
上記で取り込んだ機能、および意図的に取り込まなかった機能(複数リポジトリのset、継続的な
ファイル同期、宣言外リソースの削除、旧branch protection)とその理由を記録しています。
