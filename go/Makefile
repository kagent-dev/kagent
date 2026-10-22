BUILD_DATE := $(shell date -u '+%Y-%m-%d')
GIT_COMMIT := $(shell git rev-parse --short HEAD || echo "unknown")
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/-dirty//' | grep v || echo "v0.0.0-$(GIT_COMMIT)")
SUBSTRATE_VERSION ?= $(shell awk '/github\.com\/kagent-dev\/substrate/ { print substr($$5, 2) }' go.mod)

LDFLAGS := -X github.com/kagent-dev/kagent/go/core/internal/version.Version=$(VERSION)    \
           -X github.com/kagent-dev/kagent/go/core/internal/version.GitCommit=$(GIT_COMMIT) \
           -X github.com/kagent-dev/kagent/go/core/internal/version.BuildDate=$(BUILD_DATE) \
           -X github.com/kagent-dev/kagent/go/core/internal/version.SubstrateVersion=$(SUBSTRATE_VERSION)

default: help

.PHONY: help
help: ## list makefile targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

##@ Code Generation (api module)

CRD_OUTPUT_DIR := ../helm/kagent-crds/templates

.PHONY: manifests
manifests: controller-gen generate ## Generate ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd paths="./api/..." output:crd:artifacts:config=$(CRD_OUTPUT_DIR)

.PHONY: generate
generate: controller-gen client-generate ## Generate DeepCopy methods and Kubernetes clients.
	$(CONTROLLER_GEN) object:headerFile="api/hack/boilerplate.go.txt" paths="./api/..."

.PHONY: client-generate
client-generate: ## Generate the typed v1alpha3 Kubernetes clientset.
	go run k8s.io/code-generator/cmd/client-gen@v0.36.3 \
		--clientset-name versioned \
		--input-base github.com/kagent-dev/kagent/go \
		--input api/v1alpha3 \
		--output-dir api/clientset \
		--output-pkg github.com/kagent-dev/kagent/go/api/clientset

##@ Development

.PHONY: fmt
fmt: ## Run go fmt.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet.
	go vet ./...

CUSTOM_GOLANGCI_LINT_BIN ?= $(CURDIR)/bin/golangci-lint-custom
CUSTOM_GOLANGCI_LINT ?= $(CUSTOM_GOLANGCI_LINT_BIN)
GOLANGCI_LINT ?= go tool golangci-lint
LINT_ARGS ?=

.PHONY: lint
lint: $(CUSTOM_GOLANGCI_LINT_BIN) ## Run golangci-lint and kube-api-linter.
	$(CUSTOM_GOLANGCI_LINT) run $(LINT_ARGS)

.PHONY: lint-fix
lint-fix: LINT_ARGS := --fix
lint-fix: lint ## Run golangci-lint and kube-api-linter with auto-fix.

.PHONY: lint-config
lint-config: $(CUSTOM_GOLANGCI_LINT_BIN) ## Verify golangci-lint linter configuration.
	$(CUSTOM_GOLANGCI_LINT) config verify

$(CUSTOM_GOLANGCI_LINT_BIN): go.mod go.sum .custom-gcl.yml
	$(GOLANGCI_LINT) custom

.PHONY: govulncheck
govulncheck: ## Run govulncheck.
	go mod tidy -v && go tool govulncheck ./...

##@ Build (core module — CLI binaries)

.PHONY: core/bin/kagent-local
core/bin/kagent-local:
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

.PHONY: build
build: core/bin/kagent-linux-amd64.sha256 core/bin/kagent-linux-arm64.sha256 core/bin/kagent-darwin-amd64.sha256 core/bin/kagent-darwin-arm64.sha256 core/bin/kagent-windows-amd64.exe.sha256

.PHONY: run
run: fmt vet ## Run a controller from your host.
	go run ./core/cmd/controller/main.go

GO_TEST_FLAGS ?= -race
GO_TEST_PACKAGES ?= $(shell go list ./... | grep -vE "/test/")

.PHONY: test
test: setup-envtest ## Run all unit tests.
	KAGENT_TEST_UPDATE_GOLDEN=$(KAGENT_TEST_UPDATE_GOLDEN) \
		KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" \
		go test $(GO_TEST_FLAGS) $(GO_TEST_PACKAGES)

# Pin e2e tests to the kind cluster so a stray current-context (or an
# ambient KUBECONFIG) can't leak resources onto a remote cluster. Override
# KIND_CLUSTER_NAME to target a differently-named kind cluster.
KIND_CLUSTER_NAME ?= kagent
E2E_PARALLEL ?= 2

.PHONY: e2e
e2e: core/bin/kagent-local ## Run end-to-end tests.
	@kind get clusters 2>/dev/null | grep -qx '$(KIND_CLUSTER_NAME)' || { \
	  echo "Error: kind cluster '$(KIND_CLUSTER_NAME)' not found. Run 'make create-kind-cluster' first." >&2; \
	  exit 1; \
	}
	@set -eu; \
	  kubeconfig=$$(mktemp); \
	  trap 'rm -f "$$kubeconfig"' EXIT; \
	  kind get kubeconfig --name $(KIND_CLUSTER_NAME) > "$$kubeconfig"; \
	  KAGENT_E2E_CLI=$(CURDIR)/core/bin/kagent-local KUBECONFIG="$$kubeconfig" go test -v github.com/kagent-dev/kagent/go/core/test/e2e -parallel $(E2E_PARALLEL) -count=1 -timeout=30m

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Commands
KUBECTL ?= kubectl
CONTROLLER_GEN ?= go tool controller-gen
ENVTEST ?= go tool setup-envtest

## Envtest Assets
# ENVTEST_K8S_VERSION is the Kubernetes version used to set up envtest binaries (for example, 1.31).
ENVTEST_K8S_VERSION ?= $(shell go list -m -f "{{ .Version }}" k8s.io/api | awk -F'[v.]' '{printf "1.%d", $$3}')

.PHONY: controller-gen
controller-gen: ## Verify the controller-gen Go tool dependency.
	@$(CONTROLLER_GEN) --version

.PHONY: setup-envtest
setup-envtest: envtest $(LOCALBIN) ## Download the binaries required for ENVTEST in the local bin directory.
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@$(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path || { \
		echo "Error: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	}

.PHONY: envtest-path
envtest-path: $(LOCALBIN) ## Print the path to the envtest binaries (downloads them if absent).
	@$(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path

.PHONY: envtest
envtest: ## Verify the setup-envtest Go tool dependency.
	@$(ENVTEST) version
