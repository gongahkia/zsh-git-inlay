GO ?= go
BUILD_DIR ?= .build
BINARY := $(BUILD_DIR)/zsh-git-inlay

.PHONY: all build test test-go test-zsh test-eval test-reliability lint bench clean

all: build

build:
	mkdir -p $(BUILD_DIR)
	$(GO) build -o $(BINARY) ./cmd/zsh-git-inlay

test: test-go test-zsh test-eval

test-go:
	$(GO) test ./...

test-zsh: build
	ZSH_GIT_INLAY_BIN=$(abspath $(BINARY)) zsh tests/integration.zsh
	ZSH_GIT_INLAY_BIN=$(abspath $(BINARY)) zsh tests/reliability.zsh

test-eval: build
	ZSH_GIT_INLAY_BIN=$(abspath $(BINARY)) zsh tests/evaluation.zsh

test-reliability: build
	ZSH_GIT_INLAY_BIN=$(abspath $(BINARY)) zsh tests/reliability.zsh

lint:
	$(GO) vet ./...
	test -z "$$($(GO)fmt -l $$(find cmd internal -name '*.go' -print))"
	zsh -n zsh-git-inlay.plugin.zsh tests/integration.zsh tests/reliability.zsh tests/evaluation.zsh
	! rg -n '\beval\b|function[[:space:]]+git\b' zsh-git-inlay.plugin.zsh cmd internal

bench:
	$(GO) test -run '^$$' -bench . -benchmem ./internal/command ./internal/gitstate ./internal/candidate ./internal/daemon

clean:
	rm -rf $(BUILD_DIR)
