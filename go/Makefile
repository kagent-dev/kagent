BUILD_DATE := $(shell date -u '+%Y-%m-%d')
GIT_COMMIT := $(shell git rev-parse --short HEAD || echo "unknown")
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/-dirty//' | grep v || echo "v0.0.0-$(GIT_COMMIT)")

LDFLAGS := -X github.com/kagent-dev/kagent/go/internal/version.Version=$(VERSION)    \
           -X github.com/kagent-dev/kagent/go/internal/version.GitCommit=$(GIT_COMMIT) \
           -X github.com/kagent-dev/kagent/go/internal/version.BuildDate=$(BUILD_DATE)

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

##@ Development

.PHONY: manifests
manifests: controller-gen generate ## Generate ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="internal/controller/hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	$(GOLANGCI_LINT) run --fix

.PHONY: lint-config
lint-config: golangci-lint ## Verify golangci-lint linter configuration
	$(GOLANGCI_LINT) config verify

.PHONY: govulncheck
govulncheck:
	$(call go-install-tool,bin/govulncheck,golang.org/x/vuln/cmd/govulncheck,latest)
	go mod tidy -v
	./bin/govulncheck-latest ./...

##@ Build

bin/kagent-local:
	CGO_ENABLED=0 go test -race ./cli/...
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/kagent-local ./cli/cmd/kagent

bin/kagent-linux-amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/kagent-linux-amd64 ./cli/cmd/kagent

bin/kagent-linux-amd64.sha256: bin/kagent-linux-amd64
	sha256sum bin/kagent-linux-amd64 > bin/kagent-linux-amd64.sha256

bin/kagent-linux-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/kagent-linux-arm64 ./cli/cmd/kagent

bin/kagent-linux-arm64.sha256: bin/kagent-linux-arm64
	sha256sum bin/kagent-linux-arm64 > bin/kagent-linux-arm64.sha256

bin/kagent-darwin-amd64:
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/kagent-darwin-amd64 ./cli/cmd/kagent

bin/kagent-darwin-amd64.sha256: bin/kagent-darwin-amd64
	sha256sum bin/kagent-darwin-amd64 > bin/kagent-darwin-amd64.sha256

bin/kagent-darwin-arm64:
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/kagent-darwin-arm64 ./cli/cmd/kagent

bin/kagent-darwin-arm64.sha256: bin/kagent-darwin-arm64
	sha256sum bin/kagent-darwin-arm64 > bin/kagent-darwin-arm64.sha256

bin/kagent-windows-amd64.exe:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/kagent-windows-amd64.exe ./cli/cmd/kagent

bin/kagent-windows-amd64.exe.sha256: bin/kagent-windows-amd64.exe
	sha256sum bin/kagent-windows-amd64.exe > bin/kagent-windows-amd64.exe.sha256

.PHONY: clean
clean:
	rm -f bin/kagent* && mkdir -p bin

.PHONY: build
build: bin/kagent-linux-amd64.sha256 bin/kagent-linux-arm64.sha256 bin/kagent-darwin-amd64.sha256 bin/kagent-darwin-arm64.sha256 bin/kagent-windows-amd64.exe.sha256

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/controller/main.go

.PHONY: test
test:
	CGO_ENABLED=0 go mod tidy -v
	CGO_ENABLED=0 go test -race ./cli/...
#	CGO_ENABLED=0 go test ./controller/...

.PHONY: e2e
e2e:
	#UPDATE_GOLDEN=true go test -race -skip 'TestE2E.*' -v ./...
	go test -race -skip 'TestE2E.*' -v ./...

.PHONY: install
install: manifests
	$(KUBECTL) apply -f config/crd/bases/

.PHONY: uninstall
uninstall: manifests
	$(KUBECTL) delete -f config/crd/bases/ --ignore-not-found=$(ignore-not-found)

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

## Tool Versions
CONTROLLER_TOOLS_VERSION ?= v0.19.0
#ENVTEST_VERSION is the version of controller-runtime release branch to fetch the envtest setup script (i.e. release-0.20)
ENVTEST_VERSION ?= $(shell go list -m -f "{{ .Version }}" sigs.k8s.io/controller-runtime | awk -F'[v.]' '{printf "release-%d.%d", $$2, $$3}')
#ENVTEST_K8S_VERSION is the version of Kubernetes to use for setting up ENVTEST binaries (i.e. 1.31)
ENVTEST_K8S_VERSION ?= $(shell go list -m -f "{{ .Version }}" k8s.io/api | awk -F'[v.]' '{printf "1.%d", $$3}')
GOLANGCI_LINT_VERSION ?= v2.5.0

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
