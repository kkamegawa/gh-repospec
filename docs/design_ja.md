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

`homepage`は任意で、キーが無い状態と空の値を区別します。省略すれば現在の値をそのままにし、
空文字列を明示すれば値を消去します。`description`にも同じ理由で同じ区別が必要です。import出力が
読み取り元のリポジトリに対して収束しなければならず、「管理しない」を表現できないフィールドは
変更を報告し続けるためですが、現在のスキーマではまだ通常の文字列です。`homepage`と同じ三状態に
することは、[Issue #22](https://github.com/kkamegawa/gh-repospec/issues/22)で別途追跡しています。

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
`all_external_contributors`で、`allowedActions`と同じ列挙スタイルに合わせます。GitHubは
private リポジトリに対してこのエンドポイントを拒否するため、
`repository.visibility: private`と同時に指定した場合は適用を試みず、blockedとして
報告します。

`actions.workflowAccessLevel`(`none`、`user`、`organization`)は、他のどのリポジトリが
このリポジトリのworkflowを利用できるかを制御します。GitHubはこのアクセスポリシーを
privateとinternalのリポジトリにのみ適用し、publicリポジトリに対する読み取りと書き込みには
HTTP 422を返します。fork承認とは逆の条件です。そのため`import`はprivateまたはinternalの
リポジトリからのみ読み取り、publicリポジトリではdiagnosticを報告します。`plan`は宣言された
`repository.visibility`を評価し、`public`と同時に指定した場合は読み取りを試みずにblocked
として報告します。この実行でpublicから`private`へ変更するリポジトリでは、visibility変更の
後に適用するupdateとして計画します。

`actions.oidcSubjectCustomization`は、リポジトリのActions OIDC subject claimテンプレートを
設定します。`useImmutableSubject`は、ownerとリポジトリのIDを含む不変のclaim形式を選択します。
GitHubは2026-07-15以降に作成または名前変更されたすべてのリポジトリでこの形式を有効にし、
無効化する書き込みを受け付けても値を変更しないため、宣言が`false`でライブが`true`の差分は
収束しません。`plan`はこの差分を独立したmanualエントリ
`actions.oidcSubjectCustomization.useImmutableSubject`として理由とともに報告します。
カスタマイズの残りは通常どおり収束し、`apply`はこのフィールドを除いて書き込みます。
宣言が`true`でライブが`false`の場合は通常のupdateです。これにより、古いリポジトリから
importした設定を、毎回updateを報告することなく新しいリポジトリへ適用できます。

`security.dependabotSecurityUpdates`は、Dependabot alertが実効的に有効であることを
要求します。GitHub APIがそれ以外を拒否するためです。依存関係は`plan`時に設定と
ライブ状態の両方から評価します。同一ファイルで両方を有効にした場合はalertを先に適用し、
`dependabotAlerts`を省略した場合はGitHub上の現在の状態を使い、
`dependabotAlerts.enabled: false`を明示したままsecurity updateを有効にした場合は
blockedとして報告します。

`security.aiPullRequestScan`は、CodeQLが対応していない言語やフレームワークを対象とする
GitHubのAIによるセキュリティ検出、AI Scan for pull requestsを制御します。このAPIは
パブリックプレビューであり、仕様が変わる可能性があります。設定はリポジトリ単位です。
リポジトリでのAI Scanの有効化を妨げ得る組織単位の設定は管理せず、それを理由にGitHubが
拒否した書き込みは、他の書き込みと同様に失敗します。有効にするにはCodeQL default setupが
実質的に有効である必要があり、上記のDependabotの依存関係と同じ方法で評価します。
宣言された`codeqlDefaultSetup`を優先して先に適用し、省略した場合はGitHub上の現在の状態を使い、
`codeqlDefaultSetup.enabled: false`を明示したままAI Scanを有効にした場合はblockedとして
報告します。無効化には依存関係がないため、AI Scanを無効にする宣言は逆にCodeQL default setupより
先に書き込み、依存先の機能が無効になるまで依存元を残します。AI ScanにはGitHub Advanced Security、
Copilotライセンス、Enterpriseと組織による有効化も必要ですが、ツールは書き込み前にこれらを確認
できないため、欠けている可能性がある場合は`optional: true`を付けて宣言することを推奨します。

コードスキャン自体が利用できない場合もあります。GitHub Advanced Securityのないprivate
リポジトリ、たとえば初期構築しようとしている空の個人リポジトリでは、CodeQL default setupと
AI Scanの読み取りに403が返ります。また、GitHubがそのリポジトリについて報告できるAI Scanの設定を
持たない場合、AI Scanの読み取りに404が返ります。`plan`はこれらの応答を「状態が不明」ではなく
「機能が利用できない」と扱います。`codeqlDefaultSetup`または`aiPullRequestScan`を有効にする宣言は、
GitHubのメッセージを理由としてblocked、`optional: true`ならmanualとして報告し、`apply`は書き込みを
試みません。updateとして報告すると、どの`apply`でも変えられないため、optionalの宣言が永久に
ドリフトし続けます。機能を無効にする宣言は既に満たされている（利用できない機能は有効になり得ない）
ため、noopとして報告し、`apply`も書き込みません。権限不足による403も同じ経路をたどりますが、
機能を有効にする必須の宣言は引き続きblockedとなり実行を止めます。GitHubがレート制限として返す
403や429は機能が利用できないことを表さず、他の失敗した読み取りと同様に、`plan`は状態を不明として
報告し、`import`は失敗します。リポジトリでコードスキャンが有効になれば、次のplanは通常どおり
状態を読み取ります。

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
`plan`では、blockedな項目がなく、ツールが収束できる差分もないことを表します。secretの更新と
手動項目はこの判定から除外するため、すべての設定が一致していることの主張ではありません。
`1`はコマンドの失敗で、`--ci`付きのplanがblockedの場合も含みます。blockedなplanは失敗であり、
ドリフト検出に優先します。`2`は`plan --ci`が収束できる差分を1件以上検出したことを表します。
`apply --force-secrets`は、その実行に限りすべての設定済みsecretを`updatePolicy: always`として
扱います。ファイルを編集せずに資格情報をローテーションする手段です。

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

`import <owner/repository> [--format yaml|json] [--into <path> [--yes]]`は既存リポジトリを
取得し、`schemaVersion: 1`の文書を生成します。位置引数の代わりに`-R`/`--repo`を使えますが、
両方の指定は拒否します。標準出力の既定形式はYAMLで、自動処理向けにJSONも選べます。診断は
標準エラーへ出力するため、標準出力をリダイレクトした文書はそのまま有効です。importは
GitHubを読み取るだけで、リポジトリへの書き込みは行いません。

`repository`(topic、features、merge設定、archived状態を含む)、`labels`、`milestones`、
bypass actorを含む`rulesets`、Actionsの権限・variable・environment名、`security`、agent
variableを取り込みます。これらのうち本拡張にまだ存在しない読み取りがいくつかあり、
[`plan_ja.md`](plan_ja.md)に一覧を示します。うち2つは契約そのものに影響します。rulesetの
list APIは識別情報・target・enforcementしか返さないため、rules、conditions、bypass actorは
ruleset単位の詳細取得で得ます。securityの状態はリポジトリの`security_and_analysis`と機能別
エンドポイントから取得し、状態を取得できない機能は`false`とせず報告します。これが最も
効くのはAI Scanです。Advanced Securityが無効なリポジトリでは読み取りに403が返り、それを
`enabled: false`と書くと収束しないファイルになります。

rulesetのIDやtimestampなどサーバー生成値は除外します。ruleset nameが安定した識別子だからです。
bypass actorはactor typeで逆変換します。`Integration`は`app`、actor IDの1は`orgAdmin`、
`RepositoryRole`は固定IDの2・4・5(`maintain`、`write`、`admin`)を記号的な`role`とし、それ以外の
IDは`customRole: id:<number>`とします。両者は同じactor typeを共有するためです。team、app、
custom roleは既存の`id:<number>`形式を使い、`role`は使いません。bypass actorが空の場合も空リスト
として明示的に出力します。省略すると次のapplyでactorが消えるためです。ruleのparametersはキーを
整列し数値を整数として出力するため、同じリポジトリからは常に同じバイト列が得られます。継承された
rulesetや表現できないrulesetは、黙って縮退させず報告します。

GitHubからsecretの値と`sourceEnv`は取得できません。secret名はYAMLコメントとJSONの診断情報で
報告し、利用者がsource環境変数を指定します。ローカルの意図は推測しません。`seed`とその
`overwrite`、`commands`、agentのファイルパス、`agents.secrets`、`repository.create`、
`repository.allowVisibilityChange`は、リポジトリの状態からは導けない判断です。refresh時には
secretの更新ポリシーと`optional`を含め、これらをすべて保持します。

`--into <path>`は、ファイルに既に宣言されているパスとトップレベルセクションだけを更新します。
宣言済みcollectionの中では、GitHub上に存在する項目を更新し、ファイルに無い項目を名前順で追加
します。GitHub上に存在しない宣言済み項目は削除せず、未適用として報告します。設定の項目は
「まだ適用されていないかもしれない希望状態」であり、これを削除するとドリフトを黙って追認する
ためです。契約の他の部分を支える追加的な規則と同じ考え方です。未宣言セクションは追加せず、空の
宣言済みcollectionも宣言されたまま更新し、残す項目の順序と付属コメントを維持します。ライブの
状態を取得できなかった`security`の機能は、既存の宣言を保持して報告します。

YAML refreshは、すべてのトークンがバイトオフセットを持つASTへ解析し、変更されたスパンだけを
置換します。これにより、スパン外のコメント、キー順、字下げ、引用符、改行コードはそのまま残ります。
ノード開始位置の行番号と桁だけでは足りません。スパンの終端が得られないうえ、CRLFをLFへ正規化する
parserでは元の改行コードを保持できないためです。`config.Load`は従来どおり独自の厳格なparserを
使い、refresh用のparserは書き換え経路に限定します。両者が「有効な文書」の判断で食い違ってはいけません。
JSONはJSONとして再整形し、入力形式を維持します。

変更案を表示してから確認し、`--yes`で確認を省略します。保存前に更新候補を検証しますが、この検証には
`seed`のパスに対する通常のファイルシステム検査を含めません。実行環境にseedディレクトリが無いという
理由でrefreshが失敗しないようにするためです。変更なし、検証エラー、確認拒否、同時編集、保存エラーの
場合は元ファイルを変更しません。`--into`で明示した`--format`は入力ファイルの形式と一致している
必要があります。

## ドリフト検出と CI

`plan --ci`がドリフト検査です。ツールが収束できるcreateまたはupdateがplanに含まれる場合に
`2`で終了するため、GitHubのUIから変更された設定に対して、スケジュール実行のworkflowを
合否判定として使えます。blockedなplanは失敗であり検出に優先するため`1`で終了します。
`--ci`を付けない`plan`は常に`0`で終了します。

secretの更新は比較対象から除外します。GitHubはsecretの値を返さないため、source環境変数が
設定済みの宣言されたsecretは毎回updateとして報告され、含めるとスケジュール検査が恒久的に
赤になります。ツール外でローテーションされたsecretは構造上検出できません。ローテーションには
`apply --force-secrets`を使います。source環境変数が未設定のsecretはこれとは別で、updateでは
なくblockedとして報告されます。除外は適用されず、`plan --ci`は`1`で終了します。

| 状況 | `plan` | `plan --ci` |
|---|---|---|
| 変更なし | 0 | 0 |
| source環境変数が設定済みのsecretの更新のみ | 0 | 0 |
| 手動項目のみ(seed、commands、agentのファイル) | 0 | 0 |
| createまたはupdateあり | 0 | 2 |
| blockedあり | 0 | 1 |
| blockedとドリフトの両方あり | 0 | 1 |

`--format json`でも終了コードは同じで、ドリフトの通知は標準エラーへ出力します。そのため
リダイレクトしたレポートはどの終了コードでも妥当な文書のままであり、レポートを保存または
通知しつつ、終了コードでジョブの結果を決められます。

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
allowlistは厳密で、`PATH`とホームディレクトリの変数(`HOME`、Windowsでは`USERPROFILE`)も
暗黙には引き継がないため、他のプログラムを起動したりホームディレクトリを読んだりする
プログラムではこれらを列挙する必要があります。Windowsで常に渡すのは`SystemRoot`だけで、
これがないとプロセスを安定して起動できないためです。commandが失敗、timeout、または出力上限
超過となった場合、エラーには取得した出力を含め、宣言されたすべてのsecretのsource環境変数の
値を`***`に置き換えます。

## gh-infraとの関係

[`gh-infra-parity_ja.md`](gh-infra-parity_ja.md)に、`babarot/gh-infra`との機能比較、
上記で取り込んだ機能、および意図的に取り込まなかった機能(複数リポジトリのset、継続的な
ファイル同期、宣言外リソースの削除)とその理由を記録しています。
