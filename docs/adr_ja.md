# ADR: importと書式を保持するrefresh

以下の記録はすべて日付2026-09-05、追跡は
[Issue #11](https://github.com/kkamegawa/gh-repospec/issues/11)です。

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
