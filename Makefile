BINARY    := srcmap
MODULE    := github.com/morethancoder/srcmap
ENTRY     := ./cmd/srcmap
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   := -s -w -X main.version=$(VERSION)
BIN       := bin
DIST      := dist

PLATFORMS := \
	darwin/amd64 \
	darwin/arm64 \
	linux/amd64 \
	linux/arm64 \
	windows/amd64 \
	windows/arm64

.PHONY: build build-all install upgrade clean test lint fmt vet

## build: build for current platform
build:
	@mkdir -p $(BIN)
	go build -ldflags '$(LDFLAGS)' -o $(BIN)/$(BINARY) $(ENTRY)

## build-all: cross-compile for all platforms
build-all: clean
	@mkdir -p $(DIST)
	@$(foreach platform,$(PLATFORMS), \
		$(eval OS   := $(word 1,$(subst /, ,$(platform)))) \
		$(eval ARCH := $(word 2,$(subst /, ,$(platform)))) \
		$(eval EXT  := $(if $(filter windows,$(OS)),.exe,)) \
		echo "Building $(OS)/$(ARCH)..." && \
		GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags '$(LDFLAGS)' \
			-o $(DIST)/$(BINARY)-$(OS)-$(ARCH)$(EXT) $(ENTRY) && \
	) true

## install: install to $GOPATH/bin from the local working copy
install:
	go install -ldflags '$(LDFLAGS)' $(ENTRY)

## upgrade: pull the latest published srcmap from GitHub and install it
upgrade:
	go install $(MODULE)/cmd/srcmap@latest

## test: run all tests with race detector
test:
	go test ./... -race

## lint: run golangci-lint
lint:
	golangci-lint run

## fmt: format code
fmt:
	gofmt -w .

## vet: run go vet
vet:
	go vet ./...

## clean: remove build artifacts
clean:
	rm -rf $(BIN) $(DIST)

## help: show this help
help:
	@grep -E '^## ' Makefile | sed 's/## //' | column -t -s ':'
