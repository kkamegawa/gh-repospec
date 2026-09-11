# ADR

record 1〜5は日付2026-09-05、追跡は
[Issue #11](https://github.com/kkamegawa/gh-repospec/issues/11)（importと書式を保持するrefresh）
です。record 6は日付2026-09-08、追跡は
[Issue #12](https://github.com/kkamegawa/gh-repospec/issues/12)（ドリフト検出）です。
record 7は日付2026-09-10、追跡は
[Issue #30](https://github.com/kkamegawa/gh-repospec/issues/30)（ドリフト検出のドキュメント）
です。record 8は日付2026-09-11、追跡は
[Issue #35](https://github.com/kkamegawa/gh-repospec/issues/35)（AI Scan for pull requests）
です。

## 1. importはスキーマとファイルが既に宣言したものだけを管理する

**決定。** `import`は読み取り専用のままにします。スキーマで表現できるリポジトリ状態だけを出力し、
表現できない状態は報告します。`--into`は利用者が既に宣言したフィールドとトップレベルセクション
だけを更新します。ローカルの意図、すなわちseed source、seedの`overwrite`、承認済みcommand、
secretのsource mappingと更新ポリシー、`optional`フラグ、agentのファイルパス、`agents.secrets`、
`repository.create`、`repository.allowVisibilityChange`は保持します。

**理由。** 既存リポジトリを管理下へ取り込む際、利用者が宣言していないセクションまで管理対象を
黙って広げないことが必要です。上記のローカルな意図はGitHubから推測できないため、refresh後も
残さなければなりません。

**結果。** import出力は取り込めない値を省略でき、その補い方を説明する必要があります。GitHubの
`RepositoryRole` actor typeは固定roleとcustom roleの両方を含むため、bypass actorの逆変換には
明示的な規則が必要です。

## 2. refreshはGitHubに存在しない宣言を削除しない

**決定。** 宣言済みcollectionの中で、`--into`はGitHub上に存在する項目を更新し、ファイルに現れない
項目を追加します。GitHub上に存在しない宣言済み項目は保持し、未適用として報告します。

これは「宣言済みcollectionを完全な管理境界とし、取り込んだ状態に合わせて項目を追加・更新・*削除*
する」という以前の決定を置き換えます。

**理由。** 設定の項目は「まだ適用されていないかもしれない希望状態」です。これを削除すると、利用者が
受け入れを判断しないままドリフトを追認することになり、[`gh-infra-parity_ja.md`](gh-infra-parity_ja.md)が
`reconcile: authoritative`を不採用にした理由である追加的な規則とも矛盾します。項目を残す代償は
診断1行ですが、削除の代償はバージョン管理からしか復元できない宣言です。

**結果。** `--into`で設定ファイルを整理することはできません。ファイルには収束しない項目が蓄積し得る
ため、報告する差分は「更新」「GitHubから追加」「宣言済みだがGitHubに存在しない」を区別する必要が
あります。

## 3. refresh経路では2つ目のYAML parserを使う

**決定。** YAMLのrefreshでは、トークンがバイトオフセットを持ち改行コードを正規化しないparserで
ファイルを解析し、変更されたスパンだけを置換します。`config.Load`は従来どおり
`gopkg.in/yaml.v3`の厳格なデコードを使い、2つ目のparserは書き換え経路に限定します。この厳格さを
緩めてはいけません。

これは「既存のparserでノードを特定し、元のbyte範囲を直接編集する」という以前の決定を置き換えます。

**理由。** `gopkg.in/yaml.v3`はノードの開始位置の行と桁しか公開しません。スパンの終端もバイト
オフセットも無いため、ノードの終端は走査で再導出するしかなく、ブロックスカラー、flow collection、
anchor、マルチバイト文字で信頼できません。またこのparserはCRLFをLFへ正規化するため、この経路では
元の改行コードを保持できません。文書全体の再エンコードも選択肢にはなりません。小さなrefreshが
無関係な書式差分になるためです。

**結果。** 1つの経路のために2つ目のYAML依存を持つことになります。両者は「有効な文書」の判断で
一致していなければならず、fixtureでの検証が必要です。`config.Load`が拒否するものを、refresh経路が
黙って受け入れてはいけません。

## 4. 「空のplan」は収束し得ない変更を除外する

**決定。** 空のplanとは`HasChanges() == false`かつ`HasBlocked() == false`を指し、`seed`、
`commands`、agentのファイルパスに対する`ChangeManual`と、secretの更新を除外します。

**理由。** GitHubはsecretの値を返さないため、source環境変数が設定された宣言済みsecretは常に更新に
なります。manualの項目は、ツールが実行せず報告するだけの作業を表します。この除外がないと、
secretやseedを宣言したファイルでは
[Issue #20](https://github.com/kkamegawa/gh-repospec/issues/20)のラウンドトリップ条件を
満たせません。

**結果。** import出力にsecretは含まれないため、この除外は`--into`の検証にのみ影響します。条件を
満たすには、[`plan_ja.md`](plan_ja.md)が挙げる現在値の比較と、
[Issue #22](https://github.com/kkamegawa/gh-repospec/issues/22)で追跡する
`repository.description`の三状態化も必要です。

## 5. `repository.description`を三状態にする

**決定。** `repository.description`を、キーが無い状態と空の値を区別する任意の値に変更し、
`repository.homepage`と揃えます。キーが無ければdescriptionはそのまま、空文字列を明示すれば消去
します。`schemaVersion`は`1`のままです。

**理由。** plannerはdescriptionをnilチェックなしで比較するため、descriptionを管理しない文書は、
descriptionを持つリポジトリに対して変更を報告し続けます。リポジトリをimportしてdescriptionを管理
しないという一般的な場合に、ラウンドトリップ条件を満たせなくなります。`homepage`は既にこの区別を
持っており、非対称は決定ではなく見落としでした。

**結果。** 宣言済みの`description`の意味は変わらないため、既存の設定ファイルはすべて有効なままです。
変更はスキーマ、config型、plannerの比較、applyのリクエストに及び、
[Issue #22](https://github.com/kkamegawa/gh-repospec/issues/22)で個別に追跡します。

## 6. ドリフト検出はsecret更新を除外する

**決定。** `plan --ci`は、planにsecret以外のcreateまたはupdateが含まれる場合にのみ`2`で終了します。
`Plan.HasDrift()`が`Plan.HasChanges()`を置き換え、`Change`はsecretエントリに設定される`Opaque`
フラグを持ちます。blockedなplanは`1`で終了し、ドリフトより優先されます。`--ci`を付けない`plan`は
引き続きすべてのケースで`0`で終了します。

**理由。** issue #12は「createまたはupdateが1件以上」と述べています。文字どおり実装すると、
secretを宣言したどの設定も定期実行のたびにドリフトを報告することになります。GitHubがsecretの値を
返さないためで、record 4は既にこの理由でsecret更新を「空のplan」の定義から除外しています。
常に失敗するドリフト検査は検査として機能しません。除外はリソース名の一致ではなく明示的なフィールド
で運ぶため、表示文字列の変更が終了コードを静かに壊すことはありません。

**結果。** ツール外でローテーションされたsecretは検出されません。構造上検出できないためで、
ローテーションには`apply --force-secrets`を使います。`Change`は`omitempty`のJSONフィールドを
得るため、secret以外のエントリについて保存済み`plan.json`の出力は変わりません。`HasChanges`は
削除されます。唯一の呼び出し元はテストでした。

## 7. ドリフト検出の仕様文をドキュメント作業と併せて訂正する

**決定。** Issue #30では、スコープ上「既に正しい」とされていた`docs/design.md`と
`docs/design_ja.md`のドリフト検出・終了コードの記述も書き換えます。訂正後の記述では、
`plan --ci`はツールが収束できるcreateまたはupdateがある場合に`2`で終了すること、secretの
更新は除外すること、blockedなplanは`1`で終了し検出に優先すること、`--ci`なしの`plan`は常に
`0`で終了することを明示します。実装計画にあった終了コードの一覧表は、規範側の記録として
`design_ja.md`へ移します。

**理由。** `design_ja.md`は`plan --ci`を「変更が発生する場合に`2`で終了」と記述していました。
secretについてはrecord 6が既に反対の結論を出しており、`planOutcome`はblockedなplanに`1`を
返すため、確定済みの挙動が自身の仕様と矛盾していました。Issue #30はドリフト検出のworkflow例に
secret除外を含む終了コード契約の正確な説明を求めています。仕様側が別のことを述べたままでは、
例とその参照先の文書が食い違います。

**結果。** 挙動の変更はありません。訂正はドキュメントのみで、record 4とrecord 6が既に決定した
内容を反映するものです。実装計画のスコープ境界（「本計画は`docs/design.md`を変更しない」）は
この理由により置き換えられ、wikiの計画ページにその旨の完了ノートを記載します。

## 8. AI Scan for pull requestsはリポジトリ単位のセキュリティ機能とする

**決定。** `security.aiPullRequestScan`は`FeatureSetting`(`enabled`、`optional`)とし、
`GET`/`PATCH /repos/{owner}/{repo}/code-scanning/ai-scan`だけで管理します。組織の
エンドポイントは管理しません。有効化はCodeQL default setupに依存し、この依存は
`dependabotSecurityUpdates`と`dependabotAlerts`の関係と同じ方法で評価し、同じstageの中で
CodeQLを先に適用します。`import`は403または404の読み取りを`enabled: false`とは書かず、
診断として報告します。APIはパブリックプレビューですが、クライアントが既に固定している
APIバージョン`2026-03-10`で提供されるため、オプトインのフラグや別のリクエストヘッダーは
不要です。`schemaVersion`は`1`のままです。

**理由。** この拡張は1つのリポジトリを収束させるものであり、組織ポリシーは既に自動化せず
手動作業として報告しています。`FeatureSetting`を再利用すれば、スキーマ、planner、refresh
経路が他のセキュリティ機能と揃います。GitHubはAI ScanにCodeQL default setupを要求するため、
依存を`plan`で確認すれば、`apply`の途中で失敗させずにblockedとして報告できます。403を`false`と
書くとrecord 1と矛盾し、しかも毎回のplanで同じ読み取りが失敗するため収束しません。

**結果。** 組織でAI Scanが無効になっているリポジトリでは、宣言が`optional: true`でない限り
`apply`が失敗します。Advanced Security、Copilotライセンス、Enterpriseと組織による有効化、
AI creditsの消費は書き込み前に確認できないため、ドキュメントでは`optional: true`を推奨します。
GitHubがプレビューAPIの形を変えた場合に見直すのは、クライアントの型とこのフィールドだけです。
この変更より古いバイナリは、新しいキーを未知のフィールドとして拒否します。

## 9. 403で拒否されたコードスキャンの読み取りは機能が利用できないことを表す

**決定。** CodeQL default setupまたはAI Scanの読み取りにGitHubが403を返した場合、`plan`は
そのリポジトリでは機能が利用できないものとして扱います。宣言はGitHubのメッセージを理由として
blocked、`optional: true`ならmanualとして報告し、`apply`は書き込みを行いません。`import`は
CodeQL default setupの403を診断として報告してキーを省略します。AI Scanについてはrecord 8が
既にそうしています。

これはrecord 8を拡張するものです。record 8では、`plan`は読み取れないAI Scanの状態を
updateとして報告していました。

**理由。** この拡張の主な用途は空のリポジトリの初期構築であり、個人のprivateリポジトリには
GitHub Advanced Securityがないため、コードスキャンは有効になっていません。このような機能を
updateとして報告すると、optionalの宣言が永久にドリフトし続けます。どの`apply`でも変えられない
ため`plan --ci`は毎回`2`で終了し、`optional: true`の目的が損なわれます。また`import`は従来
CodeQLの403でそのまま失敗しており、record 1と矛盾していました。

**結果。** 権限不足による403は、機能が利用できない場合と区別できず、同じ経路をたどります。
必須の宣言はblockedとなるため、権限不足で実行が止まるという規則は保たれます。optionalの宣言は
manualとして報告され、理由にGitHubのメッセージが入るため、利用者は両者を見分けられます。後で
リポジトリのコードスキャンが有効になれば、次のplanは通常どおり状態を読み取って収束します。
