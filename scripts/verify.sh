#!/usr/bin/env bash
set -euo pipefail

packages=(./...)

go test -shuffle=on "${packages[@]}"
go vet "${packages[@]}"
mkdir -p bin
go build -o bin/gh-repospec ./cmd/gh-repospec

if [[ "${RACE:-false}" == "true" ]]; then
  go test -race -shuffle=on -count=1 "${packages[@]}"
fi

if [[ "${LINT:-false}" == "true" ]]; then
  golangci-lint run "${packages[@]}"
fi
