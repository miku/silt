# VERSION overrides the version silt reports; by default it comes from the
# module's build info (a VCS-stamped pseudo-version for local builds).
VERSION ?=
LDFLAGS := $(if $(VERSION),-X github.com/miku/silt.version=$(VERSION))

# Built with the module's own toolchain: a golangci-lint binary built with an
# older Go cannot load this module's export data.
GOLANGCI_LINT ?= go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0

.PHONY: build
build:
	go build -ldflags "$(LDFLAGS)" -o silt ./cmd/silt

# Twice: the numeric golden tests are skipped under the race detector, which
# slows instrumented float loops many times over.
.PHONY: test
test:
	go test ./...
	go test -race ./...

# Live smoke tests against the real registries; never part of `make test`.
.PHONY: test-live
test-live:
	go test -race -tags live ./...

.PHONY: lint
lint:
	go vet ./...
	$(GOLANGCI_LINT) run

.PHONY: clean
clean:
	rm -f silt
