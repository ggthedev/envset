# Builds the checked-out code, including uncommitted changes.
# To build committed branches instead, use scripts/build-branches.sh.
.DEFAULT_GOAL := help

OUT    := output
PKG    := github.com/ggthedev/envset
REV    := $(shell git rev-parse --short HEAD 2>/dev/null)
BRANCH := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null)
DIRTY  := $(shell test -z "$$(git status --porcelain 2>/dev/null)" || echo +modified)
# Empty outside a git checkout: envset then reports what Go recorded.
VERSION := $(if $(REV),$(BRANCH)@$(REV)$(DIRTY))

GOBUILD = CGO_ENABLED=0 go build -trimpath -ldflags "-X $(PKG)/pkg/version.Version=$(VERSION)"

.PHONY: help build clear

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

clear: ## Delete the binaries in output/
	rm -f $(OUT)/envset $(OUT)/envset-*
	@echo "Binaries deleted. A link to $(OUT)/envset stays broken until the next 'make build'."
