# 機能比較記録: gh-infra

## この文書の目的

[`babarot/gh-infra`](https://github.com/babarot/gh-infra) は、state ファイルを
持たずに GitHub リポジトリ設定を YAML で宣言的に管理する、隣接領域のツールです。
一部の領域では `gh-repospec` より成熟しており、本拡張の契約に不足がないかを
確認するチェックリストとして有用です。

本文書はその比較結果と、`gh-repospec` の仕様に取り込んだ機能、そして同じく
重要な「意図的に取り込まなかった機能とその理由」を記録します。ロードマップではなく
設計記録です。取り込んだ機能はいずれも [`design_ja.md`](design_ja.md) で仕様化し、
実装済みです。

比較対象は 2026 年 8 月時点の gh-infra ドキュメント一式(`import`、`plan`、`apply`、
`validate` と、`Repository`、`RepositorySet`、`File`、`FileSet` の各リソース)です。

## 取り込む機能

| gh-infra の機能 | 本拡張の現状 | 仕様 |
|---|---|---|
| `import` — ライブ状態を manifest として書き出す | 実装済み | [`import` コマンド](design_ja.md#import) |
| `import --into` — 既存 manifest にライブ状態を取り込む | 実装済み | [`import` コマンド](design_ja.md#import) |
| `plan --ci` — 差分があれば非 0 終了 | 実装済み(ドリフト時は `2`。secret の更新は除外) | [ドリフト検出と CI](design_ja.md#ドリフト検出と-ci) |
| `--force-secrets` — 全 secret を 1 回の実行で再設定 | `apply --force-secrets` として実装済み(secret ごとの `updatePolicy: always` も引き続き利用可) | [ドリフト検出と CI](design_ja.md#ドリフト検出と-ci) |
| labels | 実装済み | `labels` セクション |
| milestones | 実装済み | `milestones` セクション |
| `homepage`、`archived`、機能タブの切り替え、merge commit の題名/本文書式、release の不変化 | 実装済み | `repository` セクション |
| ruleset の bypass actor(role / team / app / organization 管理者 / custom role) | 実装済み | `rulesets[].bypassActors` |
| rulesetの`evaluate` enforcement(dry run) | 実装済み | `rulesets[].enforcement` |
| Actions の fork pull request 承認ポリシー | 実装済み(public リポジトリのみ。GitHub は private リポジトリに対してこのエンドポイントを拒否します) | `actions.forkPullRequestApproval` |
| security update が vulnerability alert に依存することの検証 | 実装済み | `security` セクション |
| 旧 branch protection (`branch_protection`) | ruleset とは独立に実装済み。旧 branch protection を使い続けているリポジトリも `import` と `apply` で往復できます。GitHub は両方を合わせて適用し、最も厳しいルールが有効になります | `settings.branchProtections` |

## 取り込まない機能

| gh-infra の機能 | 理由 |
|---|---|
| `RepositorySet` / `FileSet` — 1 つの manifest で複数リポジトリを扱い、`defaults` とリポジトリごとの override を持つ | 本拡張は 1 ドキュメント 1 リポジトリの初期化と収束を対象とします。複数展開はドキュメントモデル(複数ドキュメント YAML、ディレクトリ走査、defaults のマージ)を現行契約から大きく変更します。現行契約は意図的に厳密かつ単一ドキュメントです。多数リポジトリへの反復利用が実際に必要になった時点で、別 schema version として再検討します。 |
| `File` / `FileSet` — `content`、`source`、`github://` 参照、`<% %>` テンプレート、unified diff patch、`via: pull_request` による継続的なファイル同期 | 本拡張は `seed` でリポジトリ内容を配置しており、その契約は template / directory / ZIP からの初回投入です。重複するファイル配信機構を追加すると、あるパスをどちらが所有するのかが曖昧になります。継続的なファイル同期は bootstrap とは別の製品目標です。 |
| `reconcile: authoritative` — manifest に宣言されていない label、ruleset、branch protection、ファイルを削除する | 「宣言したリソースは収束させ、宣言していないリソースは削除しない」ことは本拡張の確定した安全性です。削除は再実行では取り戻せない唯一の変更であり、本ツールの目的は fleet の強制ではなく初期化です。 |
| `internal` visibility | GitHub Enterprise Cloud の organization でのみ利用可能であり、対象範囲外です。 |

## 見送る機能

現時点では仕様に含めませんが、将来の検討対象です。

- **対話的 diff ビューア** — gh-infra の `apply` は全画面 diff とファイル単位の skip を提供します。本拡張はテキスト/JSON のプランと 1 回の確認プロンプトです。
- **worker pool による並列取得** — 1 回の実行が多数のリポジトリに触れる場合に意味があり、`RepositorySet` に付随します。
- **ログレベル**(`--log-level`、`GH_INFRA_LOG` 相当) — API 障害の調査に有用です。現状は型付き API エラーで報告します。
- **label の別名と除外パターン** — gh-infra でも未実装であり、authoritative な削除と組み合わせて初めて意味を持ちます。

## 取り込みによる互換性

追加する設定フィールドはすべて省略可能なため、`schemaVersion` は `1` のまま据え置き、
既存の設定ファイルは有効なままです。逆方向は成立しません。解析は未知フィールドを
仕様として拒否するため、新規フィールドを使う文書は古いバイナリでは拒否されます。
追加直後のフィールドを使う場合は、CI で拡張のバージョンを固定してください。
