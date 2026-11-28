# Builds the checked-out code, including uncommitted changes.
# To build committed branches instead, use scripts/build-branches.sh.
.DEFAULT_GOAL := help

OUT    := output
PKG    := github.com/ggthedev/envset
# The newest version tag (v0.1.0), with commits since it and their hash when
# HEAD is past the tag (v0.1.0-3-gabc1234); just the hash if there is no tag.
TAG   := $(shell git describe --tags --match 'v[0-9]*' --always 2>/dev/null)
# git describe --dirty would miss new files, which are compiled too.
DIRTY := $(shell test -z "$$(git status --porcelain 2>/dev/null)" || echo -dirty)
# Empty outside a git checkout: envset then reports what Go recorded.
VERSION := $(if $(TAG),$(TAG)$(DIRTY))

GOBUILD = CGO_ENABLED=0 go build -trimpath -ldflags "-X $(PKG)/pkg/version.Version=$(VERSION)"

.PHONY: help build release clear

help: ## Show this help
	@awk -F ':.*## ' '/^[a-z]+:.*## /{printf "  make %-8s %s\n", $$1, $$2}' Makefile

build: ## Build output/envset (MACS=1: Intel and Apple Silicon binaries)
	@mkdir -p $(OUT)
ifeq ($(MACS),1)
	GOOS=darwin GOARCH=amd64 $(GOBUILD) -o $(OUT)/envset-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 $(GOBUILD) -o $(OUT)/envset-darwin-arm64 .
	@ls -l $(OUT)/envset-darwin-amd64 $(OUT)/envset-darwin-arm64
else
	$(GOBUILD) -o $(OUT)/envset .
	@$(OUT)/envset version
endif

release: ## Build the newest release tag and link ~/.local/bin/envset to it
	@scripts/install-release.sh

clear: ## Delete the development binaries in output/
	rm -f $(OUT)/envset $(OUT)/envset-*
	@echo "Binaries deleted; releases are kept. A link to $(OUT)/envset is broken until the next 'make build'."
