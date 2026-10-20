# Halotrcer. `make gates` runs every gate CLAUDE.md lists; CI runs the same
# targets. Go always comes from go.mod's toolchain line (STOCK_GO), never the
# machine's own build: this machine's go1.27.1-X:nodwarf5 is refused by
# toolchain-check.

TOOLCHAIN   := $(shell sed -n 's/^toolchain //p' go.mod)
STOCK_GO    ?= GOTOOLCHAIN=$(TOOLCHAIN) go
BIN         := bin/halotrcer
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS     := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

GOLANGCI_LINT_VERSION := 2.14.0
GOVULNCHECK := golang.org/x/vuln/cmd/govulncheck@v1.9.0
# CI installs the checksum-verified release binary and sets ACTIONLINT=actionlint.
ACTIONLINT  ?= $(STOCK_GO) run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12

# shellcheck at the CI runner's version (v0.9.0) and the current one
# (v0.11.0), pinned by image digest as Sievant does (its Makefile:109).
SHELLCHECK_IMAGES := koalaman/shellcheck@sha256:f35e8987b02760d4e76fc99a68ad5c42cc10bb32f3dd2143a3cf92f1e5446a45 koalaman/shellcheck@sha256:61862eba1fcf09a484ebcc6feea46f1782532571a34ed51fedf90dd25f925a8d
SHELL_SCRIPTS = $(wildcard scripts/*.sh) $(wildcard scripts/smoke/*.sh) $(wildcard scripts/release/*.sh)

.PHONY: build fmt-check vet test lint toolchain-check vuln secrets actionlint shellcheck gates release-check repos-check

build:
	CGO_ENABLED=0 $(STOCK_GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/halotrcer

# gofmt with no file arguments reads stdin, so an empty file list (no Go
# files tracked yet) must skip the check rather than hang.
fmt-check:
	@files="$$(git ls-files '*.go')"; test -n "$$files" || { echo "fmt-check: no Go files tracked yet"; exit 0; }; \
	out="$$(gofmt -l $$files)"; test -z "$$out" || { echo "gofmt needed:"; echo "$$out"; exit 1; }

vet: fmt-check
	$(STOCK_GO) vet ./...

test:
	$(STOCK_GO) test -race -count=1 ./...

lint:
	@v="$$(golangci-lint version 2>&1)"; case "$$v" in *"version $(GOLANGCI_LINT_VERSION) "*) ;; *) echo "lint: need golangci-lint $(GOLANGCI_LINT_VERSION), have: $$v" >&2; exit 1;; esac
	GOTOOLCHAIN=$(TOOLCHAIN) golangci-lint run ./...

# Refuses anything but the stock release named on go.mod's toolchain line
# (an X: experiment build, a distro build, or another version).
toolchain-check:
	@test -n "$(TOOLCHAIN)" || { echo "toolchain-check: go.mod has no toolchain line" >&2; exit 1; }
	@v="$$($(STOCK_GO) env GOVERSION)" || { echo "toolchain-check: could not run $(TOOLCHAIN)" >&2; exit 1; }; \
	full="$$($(STOCK_GO) version)"; \
	case "$$full" in *X:*) echo "toolchain-check: '$$full' is a non-release build; refusing" >&2; exit 1;; esac; \
	echo "$$v" | grep -Eq '^go[0-9]+\.[0-9]+\.[0-9]+$$' || { echo "toolchain-check: '$$v' is not a release goX.Y.Z; refusing" >&2; exit 1; }; \
	test "$$v" = "$(TOOLCHAIN)" || { echo "toolchain-check: running $$v, go.mod says $(TOOLCHAIN); refusing" >&2; exit 1; }; \
	echo "toolchain-check: $$full"

vuln: toolchain-check build
	$(STOCK_GO) run $(GOVULNCHECK) ./...
	$(STOCK_GO) run $(GOVULNCHECK) -mode=binary $(BIN)

secrets:
	gitleaks detect --redact --no-banner --exit-code 1
	gitleaks dir --redact --no-banner --exit-code 1 .

actionlint:
	$(ACTIONLINT) -no-color

shellcheck:
	@if [ -z "$(strip $(SHELL_SCRIPTS))" ]; then echo "shellcheck: no shell scripts yet"; exit 0; fi; \
	for img in $(SHELLCHECK_IMAGES); do \
		docker run --rm -v "$(CURDIR):/mnt:ro" -w /mnt $$img --version | sed -n 2p; \
		docker run --rm -v "$(CURDIR):/mnt:ro" -w /mnt $$img -s sh $(SHELL_SCRIPTS) || exit 1; \
	done

gates: vet test lint vuln secrets actionlint shellcheck

# The release configuration: `goreleaser check` at the release pin (2.18.3,
# as both tools). GORELEASER names the binary; CI installs the
# checksum-verified one.
GORELEASER ?= goreleaser
release-check:
	@v="$$($(GORELEASER) --version 2>&1 | sed -n 's/^GitVersion: *//p')"; test "$$v" = 2.18.3 || { echo "release-check: need goreleaser 2.18.3, have '$$v'" >&2; exit 1; }
	$(GORELEASER) check

# The shared apt/yum repository contract and the bundle's package
# dependencies, in Docker (scripts/package_repos_integration_test.go): needs
# docker, gpg, git and nfpm 2.47.0; runs the pinned Ubuntu, Fedora and
# Alpine images several times and takes minutes. Ask before running it on a
# shared machine (CLAUDE.md).
repos-check:
	$(STOCK_GO) test -tags integration -count=1 -timeout 30m -run 'TestSharedRepositories|TestPublishReposRetriesARace' -v ./scripts/
