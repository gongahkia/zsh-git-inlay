GO ?= go
BUILD_DIR ?= .build
BINARY := $(BUILD_DIR)/zsh-git-inlay
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --verify HEAD 2>/dev/null || printf unknown)
DIST ?= dist/release-snapshot
LDFLAGS := -s -w -buildid= -X main.version=$(VERSION) -X main.commit=$(COMMIT)
BUILD_FLAGS := -trimpath -buildvcs=false -ldflags "$(LDFLAGS)"

.PHONY: all build test test-go test-zsh test-eval test-reliability test-nvim test-install lint bench release-snapshot clean

all: build

build:
	mkdir -p $(BUILD_DIR)
	$(GO) build $(BUILD_FLAGS) -o $(BINARY) ./cmd/zsh-git-inlay

test: test-go test-zsh test-eval test-nvim test-install

test-go:
	$(GO) test ./...

test-zsh: build
	ZSH_GIT_INLAY_BIN=$(abspath $(BINARY)) zsh tests/integration.zsh
	ZSH_GIT_INLAY_BIN=$(abspath $(BINARY)) zsh tests/reliability.zsh

test-eval: build
	ZSH_GIT_INLAY_BIN=$(abspath $(BINARY)) zsh tests/evaluation.zsh

test-reliability: build
	ZSH_GIT_INLAY_BIN=$(abspath $(BINARY)) zsh tests/reliability.zsh

test-nvim:
	@if command -v nvim >/dev/null 2>&1; then \
		nvim --headless -u NONE -l tests/neovim.lua "$(abspath .)"; \
	else \
		echo "skipping Neovim adapter test: nvim is unavailable"; \
	fi

test-install: build
	zsh tests/install.zsh "$(abspath .)"

lint:
	$(GO) vet ./...
	test -z "$$($(GO)fmt -l $$(find cmd internal -name '*.go' -print))"
	zsh -n zsh-git-inlay.plugin.zsh tests/integration.zsh tests/reliability.zsh tests/evaluation.zsh tests/install.zsh
	sh -n scripts/install.sh scripts/uninstall.sh scripts/checksums.sh scripts/sbom.sh scripts/release-snapshot.sh
	! rg -n '\beval\b|function[[:space:]]+git\b' zsh-git-inlay.plugin.zsh cmd internal

bench:
	$(GO) test -run '^$$' -bench . -benchmem ./internal/command ./internal/gitstate ./internal/candidate ./internal/daemon

release-snapshot:
	sh scripts/release-snapshot.sh --source "$(abspath .)" --output "$(abspath $(DIST))" --version "$(VERSION)" --commit "$(COMMIT)"

clean:
	rm -rf $(BUILD_DIR)
