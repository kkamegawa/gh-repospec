GO_PACKAGES := ./...
COMMAND := ./cmd/gh-repospec
BINARY := bin/gh-repospec
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/kkamegawa/gh-repospec/internal/cli.Version=$(VERSION)

.PHONY: all test test-race vet build lint fmt clean

all: test vet build

test:
	go test -shuffle=on $(GO_PACKAGES)

test-race:
	go test -race -shuffle=on -count=1 $(GO_PACKAGES)

vet:
	go vet $(GO_PACKAGES)

build:
	mkdir -p bin
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) $(COMMAND)

lint:
	golangci-lint run $(GO_PACKAGES)

fmt:
	golangci-lint fmt $(GO_PACKAGES)

clean:
	rm -rf bin dist
