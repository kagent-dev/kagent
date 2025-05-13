# Image configuration
DOCKER_REGISTRY ?= ghcr.io
BASE_IMAGE_REGISTRY ?= cgr.dev
DOCKER_REPO ?= kagent-dev/kagent

BUILD_DATE := $(shell date -u '+%Y-%m-%d')
GIT_COMMIT := $(shell git rev-parse --short HEAD || echo "unknown")
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/-dirty//' | grep v || echo "v0.0.0-$(GIT_COMMIT)")

CONTROLLER_IMAGE_NAME ?= controller
UI_IMAGE_NAME ?= ui
APP_IMAGE_NAME ?= app

CONTROLLER_IMAGE_TAG ?= $(VERSION)
UI_IMAGE_TAG ?= $(VERSION)
APP_IMAGE_TAG ?= $(VERSION)

CONTROLLER_IMG ?= $(DOCKER_REGISTRY)/$(DOCKER_REPO)/$(CONTROLLER_IMAGE_NAME):$(CONTROLLER_IMAGE_TAG)
UI_IMG ?= $(DOCKER_REGISTRY)/$(DOCKER_REPO)/$(UI_IMAGE_NAME):$(UI_IMAGE_TAG)
APP_IMG ?= $(DOCKER_REGISTRY)/$(DOCKER_REPO)/$(APP_IMAGE_NAME):$(APP_IMAGE_TAG)

# Retagged image variables for kind loading; the Helm chart uses these
RETAGGED_DOCKER_REGISTRY = cr.kagent.dev
RETAGGED_CONTROLLER_IMG = $(RETAGGED_DOCKER_REGISTRY)/$(DOCKER_REPO)/$(CONTROLLER_IMAGE_NAME):$(CONTROLLER_IMAGE_TAG)
RETAGGED_UI_IMG = $(RETAGGED_DOCKER_REGISTRY)/$(DOCKER_REPO)/$(UI_IMAGE_NAME):$(UI_IMAGE_TAG)
RETAGGED_APP_IMG = $(RETAGGED_DOCKER_REGISTRY)/$(DOCKER_REPO)/$(APP_IMAGE_NAME):$(APP_IMAGE_TAG)
DOCKER_BUILDER ?= docker
DOCKER_BUILD_ARGS ?=
KIND_CLUSTER_NAME ?= kagent

#take from go/go.mod
AWK ?= $(shell command -v gawk || command -v awk)
TOOLS_GO_VERSION ?= $(shell $(AWK) '/^go / { print $$2 }' go/go.mod)

#tools versions
TOOLS_UV_VERSION ?= 0.7.2
TOOLS_K9S_VERSION ?= 0.50.4
TOOLS_KIND_VERSION ?= 0.27.0
TOOLS_NODE_VERSION ?= 22.15.0
TOOLS_ISTIO_VERSION ?= 1.26.0
TOOLS_ARGO_CD_VERSION ?= 3.0.0
TOOLS_KUBECTL_VERSION ?= 1.33.4

# build args
TOOLS_IMAGE_BUILD_ARGS = --build-arg BASE_IMAGE_REGISTRY=$(BASE_IMAGE_REGISTRY)
TOOLS_IMAGE_BUILD_ARGS += --build-arg TOOLS_GO_VERSION=$(TOOLS_GO_VERSION)
TOOLS_IMAGE_BUILD_ARGS += --build-arg TOOLS_UV_VERSION=$(TOOLS_UV_VERSION)
TOOLS_IMAGE_BUILD_ARGS += --build-arg TOOLS_K9S_VERSION=$(TOOLS_K9S_VERSION)
TOOLS_IMAGE_BUILD_ARGS += --build-arg TOOLS_KIND_VERSION=$(TOOLS_KIND_VERSION)
TOOLS_IMAGE_BUILD_ARGS += --build-arg TOOLS_NODE_VERSION=$(TOOLS_NODE_VERSION)
TOOLS_IMAGE_BUILD_ARGS += --build-arg TOOLS_ISTIO_VERSION=$(TOOLS_ISTIO_VERSION)
TOOLS_IMAGE_BUILD_ARGS += --build-arg TOOLS_ARGO_CD_VERSION=$(TOOLS_ARGO_CD_VERSION)
TOOLS_IMAGE_BUILD_ARGS += --build-arg TOOLS_KUBECTL_VERSION=$(TOOLS_KUBECTL_VERSION)

HELM_ACTION=upgrade --install

# Helm chart variables
KAGENT_DEFAULT_MODEL_PROVIDER ?= openAI

# Print tools versions
print-tools-versions:
	@echo "VERSION      : $(VERSION)"
	@echo "Tools Go     : $(TOOLS_GO_VERSION)"
	@echo "Tools UV     : $(TOOLS_UV_VERSION)"
	@echo "Tools K9S    : $(TOOLS_K9S_VERSION)"
	@echo "Tools Kind   : $(TOOLS_KIND_VERSION)"
	@echo "Tools Node   : $(TOOLS_NODE_VERSION)"
	@echo "Tools Istio  : $(TOOLS_ISTIO_VERSION)"
	@echo "Tools Argo CD: $(TOOLS_ARGO_CD_VERSION)"

# Check if OPENAI_API_KEY is set
check-openai-key:
	@if [ -z "$(OPENAI_API_KEY)" ]; then \
		echo "Error: OPENAI_API_KEY environment variable is not set"; \
		echo "Please set it with: export OPENAI_API_KEY=your-api-key"; \
		exit 1; \
	fi

# Build targets

.PHONY: create-kind-cluster
create-kind-cluster:
	kind create cluster --name $(KIND_CLUSTER_NAME)

.PHONY: use-kind-cluster
use-kind-cluster:
	kind get kubeconfig --name $(KIND_CLUSTER_NAME) > ~/.kube/config
	kubectl create namespace kagent || true
	kubectl config set-context --current --namespace kagent || true

.PHONY: delete-kind-cluster
delete-kind-cluster:
	kind delete cluster --name $(KIND_CLUSTER_NAME)

.PHONY: prune-kind-cluster
prune-kind-cluster:
	echo "Pruning dangling docker images from kind  ..."
	docker exec $(KIND_CLUSTER_NAME)-control-plane crictl images --filter dangling=true --no-trunc --quiet || :
	docker exec $(KIND_CLUSTER_NAME)-control-plane crictl images --filter dangling=true --no-trunc --quiet | \
	awk '{print $3}' | xargs -r docker exec $(KIND_CLUSTER_NAME)-control-plane crictl rmi || :

.PHONY: build
build: build-controller build-ui build-app

.PHONY: build-cli
build-cli:
	make -C go build

.PHONY: build-cli-local
build-cli-local:
	make -C go clean
	make -C go bin/kagent-local

.PHONY: build-img-versions
build-img-versions:
	@echo controller=$(CONTROLLER_IMG)
	@echo ui=$(UI_IMG)
	@echo app=$(APP_IMG)

.PHONY: push
push: push-controller push-ui push-app

.PHONY: controller-manifests
controller-manifests:
	make -C go manifests
	cp go/config/crd/bases/* helm/kagent-crds/templates/

.PHONY: build-controller
build-controller: controller-manifests
	$(DOCKER_BUILDER) build $(DOCKER_BUILD_ARGS) $(TOOLS_IMAGE_BUILD_ARGS) -t $(CONTROLLER_IMG) -f go/Dockerfile ./go

.PHONY: release-controller
release-controller: DOCKER_BUILD_ARGS += --push --platform linux/amd64,linux/arm64
release-controller: DOCKER_BUILDER = docker buildx
release-controller: build-controller

.PHONY: build-ui
build-ui:
	# Build the combined UI and backend image
	$(DOCKER_BUILDER) build $(DOCKER_BUILD_ARGS) $(TOOLS_IMAGE_BUILD_ARGS) -t $(UI_IMG) -f ui/Dockerfile ./ui

.PHONY: release-ui
release-ui: DOCKER_BUILD_ARGS += --push --platform linux/amd64,linux/arm64
release-ui: DOCKER_BUILDER = docker buildx
release-ui: build-ui

.PHONY: build-app
build-app:
	$(DOCKER_BUILDER) build $(DOCKER_BUILD_ARGS) $(TOOLS_IMAGE_BUILD_ARGS) -t $(APP_IMG) -f python/Dockerfile ./python

.PHONY: release-app
release-app: DOCKER_BUILD_ARGS += --push --platform linux/amd64,linux/arm64
release-app: DOCKER_BUILDER = docker buildx
release-app: build-app

.PHONY: kind-load-docker-images
kind-load-docker-images: retag-docker-images
	docker images | grep $(VERSION) || true
	kind load docker-image --name $(KIND_CLUSTER_NAME) $(RETAGGED_CONTROLLER_IMG)
	kind load docker-image --name $(KIND_CLUSTER_NAME) $(RETAGGED_UI_IMG)
	kind load docker-image --name $(KIND_CLUSTER_NAME) $(RETAGGED_APP_IMG)

.PHONY: retag-docker-images
retag-docker-images: build
	docker tag $(CONTROLLER_IMG) $(RETAGGED_CONTROLLER_IMG)
	docker tag $(UI_IMG) $(RETAGGED_UI_IMG)
	docker tag $(APP_IMG) $(RETAGGED_APP_IMG)

.PHONY: helm-version
helm-version:
	VERSION=$(VERSION) envsubst < helm/kagent-crds/Chart-template.yaml > helm/kagent-crds/Chart.yaml
	VERSION=$(VERSION) envsubst < helm/kagent/Chart-template.yaml > helm/kagent/Chart.yaml
	helm package helm/kagent-crds
	helm package helm/kagent

.PHONY: helm-install-provider
helm-install-provider: helm-version check-openai-key
	helm $(HELM_ACTION) kagent-crds helm/kagent-crds \
		--namespace kagent \
		--create-namespace \
		--history-max 2    \
		--wait
	helm $(HELM_ACTION) kagent helm/kagent \
		--namespace kagent \
		--create-namespace \
		--history-max 2    \
		--timeout 5m       \
		--wait \
		--set controller.image.registry=$(RETAGGED_DOCKER_REGISTRY) \
		--set ui.image.registry=$(RETAGGED_DOCKER_REGISTRY) \
		--set app.image.registry=$(RETAGGED_DOCKER_REGISTRY) \
		--set controller.image.tag=$(CONTROLLER_IMAGE_TAG) \
		--set ui.image.tag=$(UI_IMAGE_TAG) \
		--set app.image.tag=$(APP_IMAGE_TAG) \
		--set providers.openAI.apiKey=$(OPENAI_API_KEY) \
		--set providers.azureOpenAI.apiKey=$(AZUREOPENAI_API_KEY) \
		--set providers.anthropic.apiKey=$(ANTHROPIC_API_KEY) \
		--set providers.default=$(KAGENT_DEFAULT_MODEL_PROVIDER) \
		$(HELM_EXTRA_ARGS)

.PHONY: helm-install
helm-install: kind-load-docker-images
helm-install: helm-install-provider

.PHONY: helm-test-install
helm-test-install: HELM_ACTION+="--dry-run"
helm-test-install: helm-install-provider
# Test install with dry-run
# Example: `make helm-test-install | tee helm-test-install.log`

.PHONY: helm-uninstall
helm-uninstall:
	helm uninstall kagent --namespace kagent
	helm uninstall kagent-crds --namespace kagent

.PHONY: helm-publish
helm-publish: helm-version
	helm push kagent-crds-$(VERSION).tgz oci://ghcr.io/kagent-dev/kagent/helm
	helm push kagent-$(VERSION).tgz oci://ghcr.io/kagent-dev/kagent/helm

.PHONY: kagent-cli-install
kagent-cli-install: build-cli-local helm-version kind-load-docker-images
kagent-cli-install:
	KAGENT_HELM_REPO=./helm/ ./go/bin/kagent-local

.PHONY: kagent-cli-port-forward
kagent-cli-port-forward: use-kind-cluster
	@echo "Port forwarding to KAgent CLI..."
	kubectl port-forward -n kagent service/kagent 8081:8081 8082:80

.PHONY: open-dev-container
open-dev-container:
	@echo "Opening dev container..."
	devcontainer build .
	@devcontainer open .