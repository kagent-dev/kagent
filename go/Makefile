BUILD_DATE := $(shell date -u '+%Y-%m-%d')
GIT_COMMIT := $(shell git rev-parse --short HEAD || echo "unknown")
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/-dirty//' | grep v || echo "v0.0.0-$(GIT_COMMIT)")

LDFLAGS := -X github.com/kagent-dev/kagent/go/core/internal/version.Version=$(VERSION)    \
           -X github.com/kagent-dev/kagent/go/core/internal/version.GitCommit=$(GIT_COMMIT) \
           -X github.com/kagent-dev/kagent/go/core/internal/version.BuildDate=$(BUILD_DATE)

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

default: help

.PHONY: help
help: ## list makefile targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

##@ Code Generation (api module)

.PHONY: manifests
manifests: controller-gen generate ## Generate ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd paths="./api/..." output:crd:artifacts:config=api/config/crd/bases

.PHONY: generate
generate: controller-gen sqlc-generate ## Generate DeepCopy methods and sqlc query code.
	$(CONTROLLER_GEN) object:headerFile="api/hack/boilerplate.go.txt" paths="./api/..."

.PHONY: sqlc-generate
sqlc-generate: sqlc ## Generate type-safe Go code from SQL queries.
	cd core/internal/database && $(SQLC) generate

##@ Development

.PHONY: fmt
fmt: ## Run go fmt.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet.
	go vet ./...

KAL_SO = $(LOCALBIN)/kube-api-linter.so

.PHONY: lint
lint: golangci-lint $(KAL_SO) ## Run golangci-lint.
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint $(KAL_SO) ## Run golangci-lint with auto-fix.
	$(GOLANGCI_LINT) run --fix

.PHONY: lint-config
lint-config: golangci-lint ## Verify golangci-lint linter configuration.
	$(GOLANGCI_LINT) config verify

$(KAL_SO):
	@mkdir -p $(LOCALBIN)
	@echo "Building kube-api-linter plugin..."
	@rm -rf $(LOCALBIN)/golangci-lint-src
	@git clone --depth 1 --branch $(GOLANGCI_LINT_VERSION) --quiet https://github.com/golangci/golangci-lint.git $(LOCALBIN)/golangci-lint-src
	@cd $(LOCALBIN)/golangci-lint-src && go get sigs.k8s.io/kube-api-linter@$(KAL_VERSION)
	@cd $(LOCALBIN)/golangci-lint-src && go build -buildmode=plugin -o $(KAL_SO) sigs.k8s.io/kube-api-linter/pkg/plugin
	@rm -rf $(LOCALBIN)/golangci-lint-src

.PHONY: govulncheck
govulncheck: ## Run govulncheck.
	$(call go-install-tool,bin/govulncheck,golang.org/x/vuln/cmd/govulncheck,latest)
	go mod tidy -v && bin/govulncheck-latest ./...

##@ Build (core module — CLI binaries)

core/bin/kagent-local:
	go test -race ./core/cli/...
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o core/bin/kagent-local ./core/cli/cmd/kagent

core/bin/kagent-linux-amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o core/bin/kagent-linux-amd64 ./core/cli/cmd/kagent

core/bin/kagent-linux-amd64.sha256: core/bin/kagent-linux-amd64
	sha256sum core/bin/kagent-linux-amd64 > core/bin/kagent-linux-amd64.sha256

core/bin/kagent-linux-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o core/bin/kagent-linux-arm64 ./core/cli/cmd/kagent

core/bin/kagent-linux-arm64.sha256: core/bin/kagent-linux-arm64
	sha256sum core/bin/kagent-linux-arm64 > core/bin/kagent-linux-arm64.sha256

core/bin/kagent-darwin-amd64:
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o core/bin/kagent-darwin-amd64 ./core/cli/cmd/kagent

core/bin/kagent-darwin-amd64.sha256: core/bin/kagent-darwin-amd64
	sha256sum core/bin/kagent-darwin-amd64 > core/bin/kagent-darwin-amd64.sha256

core/bin/kagent-darwin-arm64:
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o core/bin/kagent-darwin-arm64 ./core/cli/cmd/kagent

core/bin/kagent-darwin-arm64.sha256: core/bin/kagent-darwin-arm64
	sha256sum core/bin/kagent-darwin-arm64 > core/bin/kagent-darwin-arm64.sha256

core/bin/kagent-windows-amd64.exe:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o core/bin/kagent-windows-amd64.exe ./core/cli/cmd/kagent

core/bin/kagent-windows-amd64.exe.sha256: core/bin/kagent-windows-amd64.exe
	sha256sum core/bin/kagent-windows-amd64.exe > core/bin/kagent-windows-amd64.exe.sha256

.PHONY: clean
clean:
	rm -f core/bin/kagent* && mkdir -p core/bin
	rm -f $(LOCALBIN)/kube-api-linter.so
	rm -rf $(LOCALBIN)/golangci-lint-src

.PHONY: build
build: core/bin/kagent-linux-amd64.sha256 core/bin/kagent-linux-arm64.sha256 core/bin/kagent-darwin-amd64.sha256 core/bin/kagent-darwin-arm64.sha256 core/bin/kagent-windows-amd64.exe.sha256

.PHONY: run
run: fmt vet ## Run a controller from your host.
	go run ./core/cmd/controller/main.go

.PHONY: test
test: ## Run all unit tests.
	UPDATE_GOLDEN=$(UPDATE_GOLDEN) go test -race -skip 'TestE2E.*' -v ./...

.PHONY: e2e
e2e: ## Run end-to-end tests.
	go test -v github.com/kagent-dev/kagent/go/core/test/e2e -failfast

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= kubectl
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint
SQLC = $(LOCALBIN)/sqlc

## Tool Versions
CONTROLLER_TOOLS_VERSION ?= v0.19.0
#ENVTEST_VERSION is the version of controller-runtime release branch to fetch the envtest setup script (i.e. release-0.20)
ENVTEST_VERSION ?= $(shell go list -m -f "{{ .Version }}" sigs.k8s.io/controller-runtime | awk -F'[v.]' '{printf "release-%d.%d", $$2, $$3}')
#ENVTEST_K8S_VERSION is the version of Kubernetes to use for setting up ENVTEST binaries (i.e. 1.31)
ENVTEST_K8S_VERSION ?= $(shell go list -m -f "{{ .Version }}" k8s.io/api | awk -F'[v.]' '{printf "1.%d", $$3}')
GOLANGCI_LINT_VERSION ?= v2.12.2
KAL_VERSION ?= v0.0.0-20260423112246-3fa174937a6b
SQLC_VERSION ?= v1.30.0
BUF ?= $(LOCALBIN)/buf
BUF_VERSION ?= v1.52.1

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: setup-envtest
setup-envtest: envtest ## Download the binaries required for ENVTEST in the local bin directory.
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@$(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path || { \
		echo "Error: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	}

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

.PHONY: sqlc
sqlc: $(SQLC) ## Download sqlc locally if necessary.
$(SQLC): $(LOCALBIN)
	$(call go-install-tool,$(SQLC),github.com/sqlc-dev/sqlc/cmd/sqlc,$(SQLC_VERSION))

.PHONY: buf
buf: $(BUF) ## Download buf locally if necessary (OpenShell proto generation).
$(BUF): $(LOCALBIN)
	$(call go-install-tool,$(BUF),github.com/bufbuild/buf/cmd/buf,$(BUF_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef

.PHONY: openshell-proto
openshell-proto: buf ## Regenerate OpenShell stubs from api/openshell/proto (buf; needs protoc-gen-go + protoc-gen-go-grpc on PATH).
	@cd api/openshell/proto && $(BUF) generate
