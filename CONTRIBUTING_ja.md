# gh-repobootstrapper への貢献

拡張機能の改善にご協力いただきありがとうございます。貢献内容は、このリポジトリの MIT License の下で提供されることに同意したものとします。

[English](CONTRIBUTING.md)

## 開始前

- Go 1.26 以降と最新の GitHub CLI を使用してください。
- [docs/design.md](docs/design.md) を読んでください。このファイルは確定済みの動作を記録します。設計変更は実装前に提案・合意してください。
- token、credential、private repository の内容、生成した実行ファイル、ローカル APM dependency をコミットしないでください。

## ローカル作業

fork または clone 後に目的を絞った branch を作成し、pull request の前に次の確認を実行してください。

```bash
go test ./...
go vet ./...
mkdir -p bin
go build -o bin/gh-repobootstrapper ./cmd/gh-repobootstrapper
```

```powershell
go test ./...
go vet ./...
New-Item -ItemType Directory -Path bin -Force
go build -o bin/gh-repobootstrapper.exe ./cmd/gh-repobootstrapper
```

コミット件名は、`Add ruleset validation` のように英語の命令形で記述してください。

## Pull request

- 1 つの動作変更または一貫した修正を 1 pull request にまとめます。
- 利用者に見える影響と、実行した確認を説明します。
- 動作、validation、failure path に対するテストを追加・更新します。
- 英語ドキュメントと対応する `_ja.md` を同時に更新します。LICENSE と SECURITY.md は英語のみです。
- 合意済みの設計変更なしに、command approval、path validation、secret redaction、force しない Git 更新を弱めないでください。

## Issue の報告

再現可能な不具合、ドキュメントの不足、具体的な機能要望は issue で報告してください。脆弱性の疑いは公開 issue に書かず、[SECURITY.md](SECURITY.md) に従ってください。
