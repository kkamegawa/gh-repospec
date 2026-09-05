# 実装計画: import と書式を保持する refresh

## 概要

[Issue #11](https://github.com/kkamegawa/gh-repospec/issues/11)で、読み取り専用の`import`コマンドと
`--into`によるrefreshを追加します。作業は状態取得・逆変換、元ソースを保持するファイル編集、CLIと
plannerの収束確認、そして収束の前提となるスキーマ修正1件に分割します。本書は実装計画を記録する
もので、現在のdraft PRには計画と設計文書だけを含めます。

## 追跡

| 作業 | Issue |
|---|---|
| 親 | [#11](https://github.com/kkamegawa/gh-repospec/issues/11) |
| 状態取得と逆変換 | [#18](https://github.com/kkamegawa/gh-repospec/issues/18) |
| 書式保持`--into` refresh | [#19](https://github.com/kkamegawa/gh-repospec/issues/19) |
| CLIとラウンドトリップ収束 | [#20](https://github.com/kkamegawa/gh-repospec/issues/20) |
| `repository.description`の三状態化 | [#22](https://github.com/kkamegawa/gh-repospec/issues/22) |

## まだ存在しない読み取り

現在の`internal/githubapi`のままでは`import`を書けません。読み取りが8件不足しており、さらに
ページングのヘルパーが必要なレスポンス形式のうち2つをデコードできません。

| 読み取り | 現状 |
|---|---|
| リポジトリのenvironment一覧 | 作成・更新の呼び出しのみ。レスポンスは`{total_count, environments}`のラップ形式 |
| Actionsのselected-actions許可リスト | 書き込み専用。`actions.selectedActions`に必要 |
| Actionsの既定workflow権限 | 書き込み専用。`defaultWorkflowPermissions`と`canApprovePullRequestWorkflows`に必要 |
| automated security fixes | 書き込み専用。`security.dependabotSecurityUpdates`に必要 |
| private vulnerability reporting | 書き込み専用 |
| CodeQL default setup | 書き込み専用 |
| リポジトリの`security_and_analysis` | `RepositoryInfo`がデコードしていない。`security.secretScanning`と`security.pushProtection`に必要 |
| rulesetの詳細 | list呼び出しのみ。listは識別情報・target・enforcementしか返さず、rules・conditions・bypass actorを返さないため、ruleset単位の取得が必要 |

`listAll`ヘルパーはJSON配列を直接デコードするため、ラップされたlistをページングできません。
その結果、variable、secret、custom repository roleは現状ページングされず、GitHubの既定ページ
サイズで暗黙に打ち切られます。importにはラップ形式に対応したヘルパーの変種が必要で、ページ数の
上限も上限を超えるリポジトリを想定して見直します。

## 実装フェーズ

1. `internal/importer`に、小さな読み取り専用API、上記8件の読み取り、ラップ形式対応のページング、
   `config.Config`への逆変換、診断、ruleset actorとサーバー生成値の正規化を追加します
   ([#18](https://github.com/kkamegawa/gh-repospec/issues/18))。bypass actorはactor typeで逆変換し、
   `RepositoryRole`は固定のrole IDとcustom roleに振り分けます。ruleのparametersはキーを整列し数値を
   整数として出力し、バイト列を安定させます。bypass actorが空でも空リストとして明示的に出力します。
   省略するとapplyでactorが消えるためです。
2. 既存の管理範囲だけを編集するrefreshエンジンを追加します
   ([#19](https://github.com/kkamegawa/gh-repospec/issues/19))。YAMLの編集には、トークンがバイト
   オフセットを持ち、改行コードを正規化しないparserが必要です。`gopkg.in/yaml.v3`は開始位置の行と桁
   しか公開せずCRLFを正規化するため、書き換え経路には別のYAML parserを使い、`config.Load`は従来の
   厳格なデコードを維持します。JSONはJSONとして再整形します。ローカルの意図を保持し、secretと
   未対応状態を報告します。
3. importの引数解析と出力を追加し、認証済みread clientへ接続し、plannerの比較を補完します
   ([#20](https://github.com/kkamegawa/gh-repospec/issues/20))。Actionsのworkflow権限、
   selected actions、environment、`security`の全機能、rulesetの内容、rulesetのbypass actorは、
   現在ライブの値を読まずに更新として報告しています。対応するapply側も同じ理由で無条件に書き込むため、
   両方に比較が必要です。
4. `repository.description`を三状態にします
   ([#22](https://github.com/kkamegawa/gh-repospec/issues/22))。descriptionを管理しないimport出力が、
   読み取り元のリポジトリに対して変更を報告し続けないようにするためです。

## 収束

「空のplan」とは`HasChanges() == false`かつ`HasBlocked() == false`を指します。ただし構造上収束
しない2種類は、この判定から除外します。`seed`、`commands`、agentのファイルパスに対する
`ChangeManual`と、GitHubが値を返さないsecretの更新です。import出力にsecretは含まれないため、
この除外は`--into`の検証にのみ影響します。

`repository.visibility`はスキーマ上必須なのでimportは常に出力しますが、`allowVisibilityChange`は
importできないローカルの意図です。そのため、import出力を*別の*リポジトリへ適用するとvisibilityの
変更がblockedになり得ます。ラウンドトリップの判定基準は、その文書をimportした元のリポジトリに
対して適用されます。

## 検証

- YAMLとJSONの出力を`config.Load`で検証し、repository各項目、1ページを超えるページングと上限超過、
  ラップ形式のレスポンス、open/closedのmilestone、すべてのbypass actor種別、secret診断、未対応状態、
  APIエラーを確認します。
- コメント、非アルファベット順のキー、CRLF/LF、引用符、複数行値、flow記法、collectionの追加と更新、
  GitHub上に存在しない宣言済み項目、空の宣言済みcollectionで`--into`を検証します。未変更byteと
  ローカル宣言の保持、およびseedディレクトリが無くても保存前検証が失敗しないことを確認します。
- 不正な引数、位置引数と`-R`の併用、位置引数の前後のフラグ、`yaml|json`以外の`--format`、
  非対話入力での`--yes`なし実行、形式不一致、変更なし、確認拒否、同時編集、保存エラーを検証します。
  importがGitHubのwrite APIを呼ばないこと、usage表示に`import`が含まれることも確認します。
- 実装完了時に`go test -shuffle=on ./...`、`go vet ./...`、検証スクリプト、lint、Linuxのrace test、
  `git diff --check`を実行します。

## スコープ境界

`plan --ci`、`apply --force-secrets`、実装コード、import実装完了前のREADME status変更は対象外です。
終了コード`2`は`plan --ci`側の作業です。現在`cli.Run`はerrorしか返さないため、終了コードの配線は
本作業ではなくそのコマンドとともに追跡します。
