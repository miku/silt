# VERSION overrides the version silt reports; by default it comes from the
# module's build info (a VCS-stamped pseudo-version for local builds).
VERSION ?= 0.1.0
LDFLAGS := $(if $(VERSION),-X github.com/miku/silt/internal/silt.version=$(VERSION))

SHELL := /bin/bash

# Built with the module's own toolchain: a golangci-lint binary built with an
# older Go cannot load this module's export data.
GOLANGCI_LINT ?= go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0

.PHONY: build
build:
	go build -ldflags "$(LDFLAGS)" -o silt ./cmd/silt

# Twice: the numeric golden tests are skipped under the race detector, which
# slows instrumented float loops many times over. TODO: revisit
.PHONY: test
test:
	go test ./...
	go test -race ./...

# Live smoke tests against the real registries; never part of `make test`.
.PHONY: test-live
test-live:
	go test -race -tags live ./...

# The GIFs in demo/, recorded with vhs (https://github.com/charmbracelet/vhs)
# from the tapes there, e.g. make demos or make demo/json.gif. They search
# the live registries, so every run records anew.
DEMOS := $(patsubst %.tape,%.gif,$(filter-out demo/settings.tape,$(wildcard demo/*.tape)))

.PHONY: demos
demos: $(DEMOS)

demo/%.gif: demo/%.tape demo/settings.tape build
	cd demo && PATH="$(CURDIR):$$PATH" vhs $*.tape

# deb/rpm packages for linux amd64/arm64 via goreleaser, see .goreleaser.yaml.
# snapshot builds them into ./dist without publishing; release needs a git
# tag (vX.Y.Z) and GITHUB_TOKEN, and takes the version from the tag.
.PHONY: snapshot
snapshot:
	goreleaser release --snapshot --clean --skip=publish,archive

.PHONY: release
release:
	goreleaser release --clean --skip=archive

.PHONY: lint
lint:
	go vet ./...
	$(GOLANGCI_LINT) run

.PHONY: clean
clean:
	rm -f silt
	rm -rf .mypy_cache/
	rm -rf .ropeproject/
	rm -rf scripts/patent-harness/target/
