# Image configuration
DOCKER_REGISTRY ?= ghcr.io
DOCKER_REPO ?= kagent-dev/kagent
CONTROLLER_IMAGE_NAME ?= controller
UI_IMAGE_NAME ?= ui
APP_IMAGE_NAME ?= app
VERSION ?= $(shell git describe --tags --always --dirty)
CONTROLLER_IMAGE_TAG ?= $(VERSION)
UI_IMAGE_TAG ?= $(VERSION)
APP_IMAGE_TAG ?= $(VERSION)
CONTROLLER_IMG ?= $(DOCKER_REGISTRY)/$(DOCKER_REPO)/$(CONTROLLER_IMAGE_NAME):$(CONTROLLER_IMAGE_TAG)
UI_IMG ?= $(DOCKER_REGISTRY)/$(DOCKER_REPO)/$(UI_IMAGE_NAME):$(UI_IMAGE_TAG)
APP_IMG ?= $(DOCKER_REGISTRY)/$(DOCKER_REPO)/$(APP_IMAGE_NAME):$(APP_IMAGE_TAG)
DOCKER_BUILDER ?= docker
DOCKER_BUILD_ARGS ?=
KIND_CLUSTER_NAME ?= kagent

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

.PHONY: build
build: build-controller build-ui build-app

.PHONY: build-cli
build-cli:
	make -C go build

.PHONY: push
push: push-controller push-ui push-app

.PHONY: controller-manifests
controller-manifests:
	make -C go manifests
	cp go/config/crd/bases/* helm/crds/

.PHONY: build-controller
build-controller: controller-manifests
	$(DOCKER_BUILDER) build  $(DOCKER_BUILD_ARGS) -t $(CONTROLLER_IMG) -f go/Dockerfile ./go

.PHONY: release-controller
release-controller: DOCKER_BUILD_ARGS += --push --platform linux/amd64,linux/arm64
release-controller: DOCKER_BUILDER = docker buildx
release-controller: build-controller

.PHONY: build-ui
build-ui:
	# Build the combined UI and backend image
	$(DOCKER_BUILDER) build $(DOCKER_BUILD_ARGS)  -t $(UI_IMG) -f ui/Dockerfile ./ui

.PHONY: release-ui
release-ui: DOCKER_BUILD_ARGS += --push --platform linux/amd64,linux/arm64
release-ui: DOCKER_BUILDER = docker buildx
release-ui: build-ui

.PHONY: build-app
build-app:
	$(DOCKER_BUILDER)  build $(DOCKER_BUILD_ARGS) -t $(APP_IMG) -f python/Dockerfile ./python

.PHONY: release-app
release-app: DOCKER_BUILD_ARGS += --push --platform linux/amd64,linux/arm64
release-app: DOCKER_BUILDER = docker buildx
release-app: build-app

.PHONY: kind-load-docker-images
kind-load-docker-images: build
	kind load docker-image --name $(KIND_CLUSTER_NAME) $(CONTROLLER_IMG)
	kind load docker-image --name $(KIND_CLUSTER_NAME) $(UI_IMG)
	kind load docker-image --name $(KIND_CLUSTER_NAME) $(APP_IMG)

.PHONY: helm-version
helm-version:
	VERSION=$(VERSION) envsubst < helm/Chart-template.yaml > helm/Chart.yaml
	helm package helm/

.PHONY: helm-install
helm-install: helm-version check-openai-key kind-load-docker-images
	helm upgrade --install kagent helm/ \
		--namespace kagent \
		--create-namespace \
		--wait \
		--set controller.image.tag=$(CONTROLLER_IMAGE_TAG) \
		--set ui.image.tag=$(UI_IMAGE_TAG) \
		--set app.image.tag=$(APP_IMAGE_TAG) \
		--set openai.apiKey=$(OPENAI_API_KEY)

.PHONY: helm-publish
helm-publish: helm-version
	helm push kagent-$(VERSION).tgz oci://ghcr.io/kagent-dev/kagent/helm
