# 実装計画: import と書式を保持する refresh

## 概要

[Issue #11](https://github.com/kkamegawa/gh-repospec/issues/11)で、読み取り専用の`import`コマンドと
`--into`によるrefreshを追加します。作業は状態取得・逆変換、元ソースを保持するファイル編集、CLIと
plannerの収束確認に分割します。本書は実装計画を記録するもので、現在のdraft PRには計画と設計文書
だけを含めます。

## 追跡

| 作業 | Issue |
|---|---|
| 親 | [#11](https://github.com/kkamegawa/gh-repospec/issues/11) |
| 状態取得と逆変換 | [#18](https://github.com/kkamegawa/gh-repospec/issues/18) |
| 書式保持`--into` refresh | [#19](https://github.com/kkamegawa/gh-repospec/issues/19) |
| CLIとラウンドトリップ収束 | [#20](https://github.com/kkamegawa/gh-repospec/issues/20) |

## 実装フェーズ

1. 小さな読み取り専用API、全件ページング、`config.Config`への逆変換、診断、ruleset actorとサーバー生成値の正規化を`internal/importer`に追加します。
2. 既存の管理範囲だけを編集するrefreshエンジンを追加します。YAMLはノード位置とbyte範囲で最小限の編集を行い、JSONは決定的に整形します。ローカルの意図を保持し、secretと未対応状態を報告します。
3. importの引数解析と出力を追加し、認証済みread clientへ接続します。Actions、security、environment、rulesetのplanner比較を補完し、同じ状態に対するimport出力にcreate/update/blockedが残らないことを確認します。

## 検証

- YAMLとJSONの出力を`config.Load`で検証し、repository各項目、ページング、milestone、bypass actor、secret診断、未対応状態、APIエラーを確認します。
- コメント、非アルファベット順のキー、CRLF/LF、引用符、複数行値、flow記法、collectionの追加・更新・削除、空の宣言済みcollectionで`--into`を検証します。未変更byteとローカル宣言を確認します。
- 不正な引数、形式不一致、変更なし、確認拒否、同時編集、保存エラーを検証します。importがGitHubのwrite APIを呼ばないことも確認します。
- 実装完了時に`go test -shuffle=on ./...`、`go vet ./...`、検証スクリプト、lint、Linuxのrace test、`git diff --check`を実行します。

## スコープ境界

`plan --ci`、`apply --force-secrets`、実装コード、import実装完了前のREADME status変更は対象外です。
