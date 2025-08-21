VERSION ?= dev
GIT_COMMIT := $(shell git rev-parse --short HEAD || echo "unknown")
BUILD_DATE := $(shell date -u '+%Y-%m-%d')

LDFLAGS := -X github.com/kagent-dev/kagent/go/internal/version=$(VERSION)    \
           -X github.com/kagent-dev/kagent/go/internal/version=$(GIT_COMMIT) \
           -X github.com/kagent-dev/kagent/go/internal/version=$(BUILD_DATE)

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