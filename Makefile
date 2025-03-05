# Image configuration
DOCKER_REGISTRY ?= ghcr.io
DOCKER_REPO ?= kagent-dev/kagent
CONTROLLER_IMAGE_NAME ?= controller
APP_IMAGE_NAME ?= app
VERSION ?= $(shell git describe --tags --always --dirty)
CONTROLLER_IMAGE_TAG ?= $(VERSION)
APP_IMAGE_TAG ?= $(VERSION)
CONTROLLER_IMG ?= $(DOCKER_REGISTRY)/$(DOCKER_REPO)/$(CONTROLLER_IMAGE_NAME):$(CONTROLLER_IMAGE_TAG)
APP_IMG ?= $(DOCKER_REGISTRY)/$(DOCKER_REPO)/$(APP_IMAGE_NAME):$(APP_IMAGE_TAG)

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
	kind create cluster --name autogen

.PHONY: build
build: build-controller build-app

.PHONY: build-cli
build-cli:
	make -C go build

.PHONY: push
push:
	docker push $(CONTROLLER_IMG)
	docker push $(APP_IMG)

.PHONY: controller-manifests
controller-manifests:
	make -C go manifests
	cp go/config/crd/bases/* helm/crds/

.PHONY: build-controller
build-controller: controller-manifests
	make -C go docker-build

.PHONY: build-app
build-app:
	# Build the combined UI and backend image
	docker build -t $(APP_IMG) .
	# Tag with latest for convenience
	docker tag $(APP_IMG) $(DOCKER_REGISTRY)/$(DOCKER_REPO)/$(APP_IMAGE_NAME):latest

.PHONY: kind-load-docker-images
kind-load-docker-images: build
	kind load docker-image --name autogen $(CONTROLLER_IMG)
	kind load docker-image --name autogen $(APP_IMG)

.PHONY: helm-version
helm-version:
	VERSION=$(VERSION) envsubst < helm/Chart-template.yaml > helm/Chart.yaml
	helm package helm/

.PHONY: helm-install
helm-install: helm-version check-openai-key kind-load-docker-images
	helm upgrade --install kagent helm/ \
		--namespace kagent \
		--create-namespace \
		--set controller.image.tag=$(CONTROLLER_IMAGE_TAG) \
		--set app.image.tag=$(APP_IMAGE_TAG) \
		--set openai.apiKey=$(OPENAI_API_KEY)

.PHONY: helm-publish
helm-publish: helm-version
	helm push kagent-$(VERSION).tgz oci://ghcr.io/kagent-dev/kagent/helm
