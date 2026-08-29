# Contributing to gh-repospec

Thank you for improving the extension. By contributing, you agree that your contributions are licensed under the repository's MIT License.

[日本語](CONTRIBUTING_ja.md)

## Before you start

- Use Go 1.26 or later and a current GitHub CLI installation.
- Read [docs/design.md](docs/design.md). It records the settled behavior; propose and agree on design changes before implementing them.
- Do not commit tokens, credentials, private repository material, generated executables, or local APM dependency content.

## Local workflow

Fork or clone the repository, create a focused branch, and run the following checks before opening a pull request.

```bash
go test ./...
go vet ./...
mkdir -p bin
go build -o bin/gh-repospec ./cmd/gh-repospec
```

```powershell
go test ./...
go vet ./...
New-Item -ItemType Directory -Path bin -Force
go build -o bin/gh-repospec.exe ./cmd/gh-repospec
```

Use an imperative English commit subject, for example `Add ruleset validation`.

## Pull requests

- Keep one behavior change or cohesive fix per pull request.
- Explain the user-visible effect and the checks you ran.
- Add or update tests for behavior, validation, and failure paths.
- Update English documentation and its `_ja.md` counterpart together. LICENSE and SECURITY.md remain English-only.
- Do not weaken command approval, path validation, secret redaction, or non-forced Git updates without an agreed design change.

## Reporting issues

Use an issue for reproducible defects, documentation gaps, and concrete feature requests. Do not report suspected vulnerabilities in a public issue; follow [SECURITY.md](SECURITY.md) instead.
