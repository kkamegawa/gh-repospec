# 実装計画: ドリフト検出、plan --ci、apply --force-secrets

## 概要

[Issue #12](https://github.com/kkamegawa/gh-repospec/issues/12)で、終了コードの契約、定期実行
ワークフロー向けのドリフト検査となる`plan --ci`、そしてファイル編集なしで資格情報を更新できる
`apply --force-secrets`を追加します。作業は終了コードの仕組みと`plan --ci`、`apply --force-secrets`、
そして両方に依存するドキュメントとワークフロー例の更新に分割します。本書は実装計画を記録するもの
で、現在のdraft PRには計画と設計文書だけを含めます。`docs/design.md`（`## Drift detection and CI`
節と終了コードの段落）と`docs/design_ja.md`は既に想定される挙動を正しく規定しており、本作業では
変更しません。

## 追跡

| 作業 | Issue |
|---|---|
| 親 | [#12](https://github.com/kkamegawa/gh-repospec/issues/12) |
| 終了コードの契約と`plan --ci` | [#28](https://github.com/kkamegawa/gh-repospec/issues/28) |
| `apply --force-secrets` | [#29](https://github.com/kkamegawa/gh-repospec/issues/29) |
| ドキュメントとワークフロー例 | [#30](https://github.com/kkamegawa/gh-repospec/issues/30) |

## ドリフト判定はsecret更新を除外する

`appendSecretChange`は、宣言済みsecretの`sourceEnv`が設定されていれば無条件に`ChangeUpdate`を
報告します。GitHubがsecretの値を返さないためです。`docs/adr.md`のrecord 4は、まさにこの理由で
「空のplan」（`HasChanges() == false && HasBlocked() == false`）からsecret更新を既に除外しています。
`plan --ci`を既存の`HasChanges()`にそのまま接続すると、secretを宣言した設定は定期実行のたびに
ドリフトを報告し、検査自体が無意味になります。

そこで本計画では`Plan.HasChanges()`を`Plan.HasDrift()`に置き換えます。create/updateの判定は同じ
ですが、`Opaque`が付いた変更を除外します。`Change`には`Opaque bool`フィールド（JSONでは
`omitempty`）を追加し、`appendSecretChange`の両分岐（blocked側とupdate側）に設定します。opacityは
stateではなくリソースの性質だからです。これはissue #12の文面（「create またはupdateが1件以上」）
からの意図的な逸脱であり、`docs/adr.md`と`docs/adr_ja.md`にADR record 6として2026-09-08付で記録
します。

除外の判定には、`actions.secret.` / `agents.secret.`というリソース名の前方一致ではなく明示的な
フィールドを選びました。前方一致は終了コードを表示文字列に結合してしまい、表示文字列だけが変わって
検査が静かに壊れる可能性があるためです。

## 終了コードの仕組み

`cli.Run`はerrorを返す既存の型のままとします。`(int, error)`への署名変更は、4コマンドにまたがる
約15箇所のreturn文すべてに終了コードの雑音を持ち込むことになり、必要なのはそのうち1箇所だけです。
代わりに`internal/cli`に終了コード`2`を運ぶ型付きエラー`DriftError`を追加します。
`cmd/gh-repospec/main.go`は、小さくテスト可能な`exitCode(err error, stderr io.Writer) int`関数で
これをマッピングします。`*DriftError`は`error:`行を出さずに`2`を返します（ドリフト検出は失敗ではなく
成功した検査結果だからです）。それ以外の非nilエラーは`error: <err>`を出力して`1`を返し、`nil`は
`0`を返します。

`plan --ci`はplanがblockedのとき、ドリフトが同時にあっても失敗（終了コード`1`）します。blockedな
planは検出結果より優先される失敗だからです。`--ci`を付けない`plan`は本作業の影響を受けません。
blockedを含め、現状どおりすべてのケースで`0`のまま終了します。

| 状況 | `plan` | `plan --ci` |
|---|---|---|
| 変更なし | 0 | 0 |
| secret更新のみ（opaque） | 0 | 0 |
| `ChangeManual`のみ（seed、commands、agentファイル） | 0 | 0 |
| create/updateあり | 0 | 2 |
| blockedあり | 0 | 1 |
| blockedとドリフトが同時 | 0 | 1 |

終了コード`2`に付随するドリフト通知は標準エラーへ書きます。そのため`--format json`の出力は、
ワークフローがファイルへリダイレクトする場合も含め、どの終了コードでも標準出力上で妥当かつ完全な
文書のままです。

## `apply --force-secrets`

`planner.Options`に`ForceSecrets bool`を追加します。`applySecrets`の`agents bool`引数は名前付き型
`secretTarget`（`secretTargetActions` / `secretTargetAgents`）に置き換え、2つ目のbool引数を増やす
代わりに`Options`を`applyActions`・`applyAgentVariables`経由で`applySecrets`まで渡します。
`createOnly`のスキップ判定は`!options.ForceSecrets`を条件に加えます。このフラグは`sourceEnv`の
要件を緩めません。source環境変数が無いsecretはplan時点で引き続き`ChangeBlocked`となり、`Apply`は
blockedなplanを引き続き拒否します。

`--force-secrets`は`apply`にのみ登録し、`Build`には影響しません。そのため、後から`apply`に
このフラグを渡すかどうかにかかわらずplanの出力は同一です。

## 検証

- `internal/planner`: `HasDrift()`がopaqueなupdateを除外し他は除外しないことを確認するテーブル
  テスト。`appendSecretChange`が両分岐で`Opaque`を設定することを確認する`Build`テスト。新規の
  `secretService`実装フェイク（テストパッケージには現状存在しない）を使った`apply`テスト群 ——
  既存`createOnly`スキップの基準確認、`--force-secrets`による既存Actions secretの再書き込み、
  `--force-secrets`によるagent secretの網羅、source環境変数が無い場合に`--force-secrets`でも
  blockedのままであること。
- `internal/cli`: 上記の`plan --ci`終了コード表に対応するテーブルテスト。`--ci`がplan専用、
  `--force-secrets`がapply専用であることを確認するフラグ解析テスト。usage表示のテスト。
- `cmd/gh-repospec`: 新規`main_test.go`で`exitCode`を直接単体テスト（ドリフト時に`error:`行が
  出ないことの確認を含む）に加え、`internal/seed/seed_test.go`で既に使われているサブプロセス
  再実行パターンで、オフラインで到達可能なケースについて`main`が`exitCode`を`os.Exit`に正しく
  配線していることを確認する1本のテスト。
- 実装完了時に`go test -shuffle=on ./...`、`go vet ./...`、検証スクリプト、lint、Linuxのrace test、
  `git diff --check`を実行します。
- 手動確認: ドリフトのあるリポジトリに対する`plan --ci --format json`が`2`で終了し標準出力が妥当な
  JSONであること。収束済みリポジトリに対しては`0`で終了すること。差分がsecretのみの設定は`0`で
  終了すること。既存の`createOnly`secretを持つリポジトリに対する`apply --force-secrets`がその
  secretを更新すること。

## スコープ境界

本計画は`docs/design.md`・`docs/design_ja.md`を変更しません。ドリフト検出と終了コードに関する節は
既に実装後の挙動を正しく記述しています。ドキュメントとワークフロー例の更新（READMEのstatus注記、
`docs/gh-infra-parity.md`、2件のワークフロー例）は
[#30](https://github.com/kkamegawa/gh-repospec/issues/30)の範囲であり、
[#28](https://github.com/kkamegawa/gh-repospec/issues/28)と
[#29](https://github.com/kkamegawa/gh-repospec/issues/29)の両方が完了した後にのみ着手します。
実装前の挙動をどの文書も主張しないようにするためです。
