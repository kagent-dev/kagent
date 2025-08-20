VERSION ?= dev
GIT_COMMIT := $(shell git rev-parse --short HEAD || echo "unknown")
BUILD_DATE := $(shell date -u '+%Y-%m-%d')

# get kmcp version from go.mod
KMCP_VERSION ?= $(shell $(AWK) '/github\.com\/kagent-dev\/kmcp/ { print substr($$2, 2) }' go/go.mod)

LDFLAGS := -X github.com/kagent-dev/kagent/go/internal/version.Version=$(VERSION)    \
           -X github.com/kagent-dev/kagent/go/internal/version.GitCommit=$(GIT_COMMIT) \
           -X github.com/kagent-dev/kagent/go/internal/version.BuildDate=$(BUILD_DATE) \
           -X github.com/kagent-dev/kagent/go/internal/version.KmcpVersion=$(KMCP_VERSION)

.PHONY: build
build:
	go build -ldflags "$(LDFLAGS)" -o bin/kagent ./cmd/kagent

.PHONY: install
install:
	go install -ldflags "$(LDFLAGS)" ./cmd/kagent

.PHONY: clean
clean:
	rm -rf bin/

.PHONY: test
test:
	go test ./...

.PHONY: deps
deps:
	go mod download
	go mod tidy

.DEFAULT_GOAL := build