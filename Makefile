# Image configuration
DOCKER_REGISTRY ?= ghcr.io
BASE_IMAGE_REGISTRY ?= cgr.dev
DOCKER_REPO ?= kagent-dev/kagent
HELM_REPO ?= oci://ghcr.io/kagent-dev
HELM_DIST_FOLDER ?= dist

BUILD_DATE := $(shell date -u '+%Y-%m-%d')
GIT_COMMIT := $(shell git rev-parse --short HEAD || echo "unknown")
VERSION ?= $(shell git describe --tags --always 2>/dev/null | grep v || echo "v0.0.0+$(GIT_COMMIT)")

# Local architecture detection to build for the current platform
LOCALARCH ?= $(shell uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')

# Docker buildx configuration
BUILDKIT_VERSION = v0.23.0
BUILDX_NO_DEFAULT_ATTESTATIONS=1
BUILDX_BUILDER_NAME ?= kagent-builder-$(BUILDKIT_VERSION)

DOCKER_BUILDER ?= docker buildx
DOCKER_BUILD_ARGS ?= --builder $(BUILDX_BUILDER_NAME) --pull --load --platform linux/$(LOCALARCH)
KIND_CLUSTER_NAME ?= kagent

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

#take from go/go.mod
AWK ?= $(shell command -v gawk || command -v awk)
TOOLS_GO_VERSION ?= $(shell $(AWK) '/^go / { print $$2 }' go/go.mod)

# Version information for the build
LDFLAGS := "-X github.com/kagent-dev/kagent/go/internal/version.Version=$(VERSION)      \
            -X github.com/kagent-dev/kagent/go/internal/version.GitCommit=$(GIT_COMMIT) \
            -X github.com/kagent-dev/kagent/go/internal/version.BuildDate=$(BUILD_DATE)"

#tools versions
TOOLS_UV_VERSION ?= 0.7.2
TOOLS_BUN_VERSION ?= 1.2.16
TOOLS_NODE_VERSION ?= 22.16.0
TOOLS_PYTHON_VERSION ?= 3.13
TOOLS_KIND_IMAGE_VERSION ?= 1.33.1

# build args
TOOLS_IMAGE_BUILD_ARGS =  --build-arg VERSION=$(VERSION)
TOOLS_IMAGE_BUILD_ARGS += --build-arg LDFLAGS=$(LDFLAGS)
TOOLS_IMAGE_BUILD_ARGS += --build-arg BASE_IMAGE_REGISTRY=$(BASE_IMAGE_REGISTRY)
TOOLS_IMAGE_BUILD_ARGS += --build-arg TOOLS_GO_VERSION=$(TOOLS_GO_VERSION)
TOOLS_IMAGE_BUILD_ARGS += --build-arg TOOLS_UV_VERSION=$(TOOLS_UV_VERSION)
TOOLS_IMAGE_BUILD_ARGS += --build-arg TOOLS_BUN_VERSION=$(TOOLS_BUN_VERSION)
TOOLS_IMAGE_BUILD_ARGS += --build-arg TOOLS_PYTHON_VERSION=$(TOOLS_PYTHON_VERSION)
TOOLS_IMAGE_BUILD_ARGS += --build-arg TOOLS_NODE_VERSION=$(TOOLS_NODE_VERSION)

HELM_ACTION=upgrade --install

# Helm chart variables
KAGENT_DEFAULT_MODEL_PROVIDER ?= openAI

# Print tools versions
print-tools-versions:
	@echo "VERSION      : $(VERSION)"
	@echo "Tools Go     : $(TOOLS_GO_VERSION)"
	@echo "Tools UV     : $(TOOLS_UV_VERSION)"
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

.PHONY: buildx-create
buildx-create:
	docker buildx inspect $(BUILDX_BUILDER_NAME) 2>&1 > /dev/null || \
	docker buildx create --name $(BUILDX_BUILDER_NAME) --platform linux/amd64,linux/arm64 --driver docker-container --use || true

.PHONY: build-all  # for test purpose build all but output to /dev/null
build-all: BUILD_ARGS ?= --progress=plain --builder $(BUILDX_BUILDER_NAME) --platform linux/amd64,linux/arm64 --output type=tar,dest=/dev/null
build-all: buildx-create
	$(DOCKER_BUILDER) build $(BUILD_ARGS) $(TOOLS_IMAGE_BUILD_ARGS) -f go/Dockerfile     ./go
	$(DOCKER_BUILDER) build $(BUILD_ARGS) $(TOOLS_IMAGE_BUILD_ARGS) -f ui/Dockerfile     ./ui
	$(DOCKER_BUILDER) build $(BUILD_ARGS) $(TOOLS_IMAGE_BUILD_ARGS) -f python/Dockerfile ./python

.PHONY: create-kind-cluster
create-kind-cluster:
	docker pull kindest/node:v$(TOOLS_KIND_IMAGE_VERSION) || true
	kind create cluster --name $(KIND_CLUSTER_NAME) --image kindest/node:v$(TOOLS_KIND_IMAGE_VERSION) --config ./scripts/kind/kind-config.yaml

.PHONY: use-kind-cluster
use-kind-cluster:
	kind get kubeconfig --name $(KIND_CLUSTER_NAME) > /tmp/kind-config
	KUBECONFIG=~/.kube/config:/tmp/kind-config kubectl config view --merge --flatten > ~/.kube/config.tmp && mv ~/.kube/config.tmp ~/.kube/config
	kubectl create namespace kagent || true
	kubectl config set-context --current --namespace kagent || true

.PHONY: delete-kind-cluster
delete-kind-cluster:
	kind delete cluster --name $(KIND_CLUSTER_NAME)

.PHONY: clean
clean: prune-kind-cluster
clean: prune-docker-images
	docker buildx rm $(BUILDX_BUILDER_NAME)  -f || true
	rm -rf ./go/bin

.PHONY: prune-kind-cluster
prune-kind-cluster:
	echo "Pruning dangling docker images from kind  ..."
	docker exec $(KIND_CLUSTER_NAME)-control-plane crictl images --no-trunc --quiet | \
	grep '<none>' | awk '{print $3}' | xargs -r -n1 docker exec $(KIND_CLUSTER_NAME)-control-plane crictl rmi || :

.PHONY: prune-docker-images
prune-docker-images:
	echo "Pruning dangling docker images ..."
	docker images --format '{{.Repository}}:{{.Tag}} {{.ID}}' | \
	grep -v ":$(VERSION) " | grep kagent | grep -v '<none>' | awk '{print $2}' | xargs -r docker rmi || :
	docker images --filter dangling=true -q | xargs -r docker rmi || :

.PHONY: build
build: buildx-create build-controller build-ui build-app
	@echo "Build completed successfully."
	@echo "Controller Image: $(CONTROLLER_IMG)"
	@echo "UI Image: $(UI_IMG)"
	@echo "App Image: $(APP_IMG)"
	@echo "Tools Image: $(TOOLS_IMG)"

.PHONY: build-monitor
build-monitor: buildx-create
	watch docker exec -t  buildx_buildkit_$(BUILDX_BUILDER_NAME)0  ps

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
build-controller: buildx-create controller-manifests
	$(DOCKER_BUILDER) build $(DOCKER_BUILD_ARGS) $(TOOLS_IMAGE_BUILD_ARGS) -t $(CONTROLLER_IMG) -f go/Dockerfile ./go

.PHONY: build-ui
build-ui: buildx-create
	$(DOCKER_BUILDER) build $(DOCKER_BUILD_ARGS) $(TOOLS_IMAGE_BUILD_ARGS) -t $(UI_IMG) -f ui/Dockerfile ./ui

.PHONY: build-app
build-app: buildx-create
	$(DOCKER_BUILDER) build $(DOCKER_BUILD_ARGS) $(TOOLS_IMAGE_BUILD_ARGS) -t $(APP_IMG) -f python/Dockerfile ./python

.PHONY: kind-load-docker-images
kind-load-docker-images: retag-docker-images use-kind-cluster
	docker images | grep $(VERSION) | grep $(DOCKER_REGISTRY) || true
	echo "Loading docker images into kind cluster $(KIND_CLUSTER_NAME)..."
	kind get clusters || true
	kind load docker-image --name $(KIND_CLUSTER_NAME) $(RETAGGED_CONTROLLER_IMG)
	kind load docker-image --name $(KIND_CLUSTER_NAME) $(RETAGGED_UI_IMG)
	kind load docker-image --name $(KIND_CLUSTER_NAME) $(RETAGGED_APP_IMG)

.PHONY: retag-docker-images
retag-docker-images: build
	docker tag $(CONTROLLER_IMG) $(RETAGGED_CONTROLLER_IMG)
	docker tag $(UI_IMG) $(RETAGGED_UI_IMG)
	docker tag $(APP_IMG) $(RETAGGED_APP_IMG)

.PHONY: helm-cleanup
helm-cleanup:
	rm -f ./$(HELM_DIST_FOLDER)/*.tgz

.PHONY: helm-test
helm-test: helm-version
	mkdir -p tmp
	echo $$(helm template kagent ./helm/kagent/ --namespace kagent --set providers.default=ollama																	| tee tmp/ollama.yaml 		| grep ^kind: | wc -l)
	echo $$(helm template kagent ./helm/kagent/ --namespace kagent --set providers.default=openAI       --set providers.openAI.apiKey=your-openai-api-key 			| tee tmp/openAI.yaml 		| grep ^kind: | wc -l)
	echo $$(helm template kagent ./helm/kagent/ --namespace kagent --set providers.default=anthropic    --set providers.anthropic.apiKey=your-anthropic-api-key 	| tee tmp/anthropic.yaml 	| grep ^kind: | wc -l)
	echo $$(helm template kagent ./helm/kagent/ --namespace kagent --set providers.default=azureOpenAI  --set providers.azureOpenAI.apiKey=your-openai-api-key		| tee tmp/azureOpenAI.yaml	| grep ^kind: | wc -l)
	helm plugin ls | grep unittest || helm plugin install https://github.com/helm-unittest/helm-unittest.git
	helm unittest helm/kagent

.PHONY: helm-agents
helm-agents:
	VERSION=$(VERSION) envsubst < helm/agents/k8s/Chart-template.yaml > helm/agents/k8s/Chart.yaml
	helm package -d $(HELM_DIST_FOLDER) helm/agents/k8s
	VERSION=$(VERSION) envsubst < helm/agents/kgateway/Chart-template.yaml > helm/agents/kgateway/Chart.yaml
	helm package -d $(HELM_DIST_FOLDER) helm/agents/kgateway
	VERSION=$(VERSION) envsubst < helm/agents/istio/Chart-template.yaml > helm/agents/istio/Chart.yaml
	helm package -d $(HELM_DIST_FOLDER) helm/agents/istio
	VERSION=$(VERSION) envsubst < helm/agents/promql/Chart-template.yaml > helm/agents/promql/Chart.yaml
	helm package -d $(HELM_DIST_FOLDER) helm/agents/promql
	VERSION=$(VERSION) envsubst < helm/agents/observability/Chart-template.yaml > helm/agents/observability/Chart.yaml
	helm package -d $(HELM_DIST_FOLDER) helm/agents/observability
	VERSION=$(VERSION) envsubst < helm/agents/helm/Chart-template.yaml > helm/agents/helm/Chart.yaml
	helm package -d $(HELM_DIST_FOLDER) helm/agents/helm
	VERSION=$(VERSION) envsubst < helm/agents/argo-rollouts/Chart-template.yaml > helm/agents/argo-rollouts/Chart.yaml
	helm package -d $(HELM_DIST_FOLDER) helm/agents/argo-rollouts
	VERSION=$(VERSION) envsubst < helm/agents/cilium-policy/Chart-template.yaml > helm/agents/cilium-policy/Chart.yaml
	helm package -d $(HELM_DIST_FOLDER) helm/agents/cilium-policy
	VERSION=$(VERSION) envsubst < helm/agents/cilium-debug/Chart-template.yaml > helm/agents/cilium-debug/Chart.yaml
	helm package -d $(HELM_DIST_FOLDER) helm/agents/cilium-debug
	VERSION=$(VERSION) envsubst < helm/agents/cilium-manager/Chart-template.yaml > helm/agents/cilium-manager/Chart.yaml
	helm package -d $(HELM_DIST_FOLDER) helm/agents/cilium-manager

.PHONY: helm-tools
helm-tools:
	VERSION=$(VERSION) envsubst < helm/tools/querydoc/Chart-template.yaml > helm/tools/querydoc/Chart.yaml
	helm package -d $(HELM_DIST_FOLDER) helm/tools/querydoc

.PHONY: helm-version
helm-version: helm-cleanup helm-agents helm-tools
	VERSION=$(VERSION) envsubst < helm/kagent-crds/Chart-template.yaml > helm/kagent-crds/Chart.yaml
	VERSION=$(VERSION) envsubst < helm/kagent/Chart-template.yaml > helm/kagent/Chart.yaml
	helm dependency update helm/kagent
	helm package -d $(HELM_DIST_FOLDER) helm/kagent-crds
	helm package -d $(HELM_DIST_FOLDER) helm/kagent

.PHONY: helm-install-provider
helm-install-provider: helm-version check-openai-key
	helm $(HELM_ACTION) kagent-crds helm/kagent-crds \
		--namespace kagent \
		--create-namespace \
		--history-max 2    \
		--timeout 5m 			\
		--kube-context kind-$(KIND_CLUSTER_NAME) \
		--wait
	helm $(HELM_ACTION) kagent helm/kagent \
		--namespace kagent \
		--create-namespace \
		--history-max 2    \
		--timeout 5m       \
		--kube-context kind-$(KIND_CLUSTER_NAME) \
		--wait \
		--set ui.service.type=LoadBalancer \
		--set ui.image.registry=$(RETAGGED_DOCKER_REGISTRY) \
		--set ui.image.tag=$(UI_IMAGE_TAG) \
		--set controller.image.registry=$(RETAGGED_DOCKER_REGISTRY) \
		--set controller.image.tag=$(CONTROLLER_IMAGE_TAG) \
		--set controller.service.type=LoadBalancer \
		--set engine.image.registry=$(RETAGGED_DOCKER_REGISTRY) \
		--set engine.image.tag=$(APP_IMAGE_TAG) \
		--set providers.openAI.apiKey=$(OPENAI_API_KEY) \
		--set providers.azureOpenAI.apiKey=$(AZUREOPENAI_API_KEY) \
		--set providers.anthropic.apiKey=$(ANTHROPIC_API_KEY) \
		--set providers.default=$(KAGENT_DEFAULT_MODEL_PROVIDER) \
		--set querydoc.openai.apiKey=$(OPENAI_API_KEY) \
		$(KAGENT_HELM_EXTRA_ARGS)

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
	helm uninstall kagent --namespace kagent --kube-context kind-$(KIND_CLUSTER_NAME) --wait
	helm uninstall kagent-crds --namespace kagent --kube-context kind-$(KIND_CLUSTER_NAME) --wait

.PHONY: helm-publish
helm-publish: helm-version
	helm push ./$(HELM_DIST_FOLDER)/kagent-crds-$(VERSION).tgz $(HELM_REPO)/kagent/helm
	helm push ./$(HELM_DIST_FOLDER)/kagent-$(VERSION).tgz $(HELM_REPO)/kagent/helm
	helm push ./$(HELM_DIST_FOLDER)/helm-agent-$(VERSION).tgz $(HELM_REPO)/kagent/agents
	helm push ./$(HELM_DIST_FOLDER)/istio-agent-$(VERSION).tgz $(HELM_REPO)/kagent/agents
	helm push ./$(HELM_DIST_FOLDER)/promql-agent-$(VERSION).tgz $(HELM_REPO)/kagent/agents
	helm push ./$(HELM_DIST_FOLDER)/observability-agent-$(VERSION).tgz $(HELM_REPO)/kagent/agents
	helm push ./$(HELM_DIST_FOLDER)/argo-rollouts-agent-$(VERSION).tgz $(HELM_REPO)/kagent/agents
	helm push ./$(HELM_DIST_FOLDER)/cilium-policy-agent-$(VERSION).tgz $(HELM_REPO)/kagent/agents
	helm push ./$(HELM_DIST_FOLDER)/cilium-manager-agent-$(VERSION).tgz $(HELM_REPO)/kagent/agents
	helm push ./$(HELM_DIST_FOLDER)/cilium-debug-agent-$(VERSION).tgz $(HELM_REPO)/kagent/agents
	helm push ./$(HELM_DIST_FOLDER)/kgateway-agent-$(VERSION).tgz $(HELM_REPO)/kagent/agents

.PHONY: kagent-cli-install
kagent-cli-install: use-kind-cluster build-cli-local kind-load-docker-images helm-version
	KAGENT_HELM_REPO=./helm/ ./go/bin/kagent-local install
	KAGENT_HELM_REPO=./helm/ ./go/bin/kagent-local dashboard

.PHONY: docker/push
docker/push: retag-docker-images
	docker push $(RETAGGED_UI_IMG)
	docker push $(RETAGGED_APP_IMG)
	docker push $(RETAGGED_CONTROLLER_IMG)

.PHONY: kagent-cli-port-forward
kagent-cli-port-forward: use-kind-cluster
	@echo "Port forwarding to kagent CLI..."
	kubectl port-forward -n kagent service/kagent 8081:8081 8082:80 8084:8084

.PHONY: kagent-addon-install
kagent-addon-install: use-kind-cluster
	#to test the kagent addons - installing istio, grafana, prometheus, metrics-server
	istioctl install --set profile=demo -y
	kubectl apply -f contrib/addons/grafana.yaml
	kubectl apply -f contrib/addons/prometheus.yaml
	kubectl apply -f contrib/addons/metrics-server.yaml
	#wait for pods to be ready
	kubectl wait --for=condition=Ready pod -l app.kubernetes.io/name=grafana 	-n kagent --timeout=60s
	kubectl wait --for=condition=Ready pod -l app.kubernetes.io/name=prometheus -n kagent --timeout=60s
	#port forward grafana service
	kubectl port-forward svc/grafana 3000:3000 -n kagent

.PHONY: open-dev-container
open-dev-container:
	@echo "Opening dev container..."
	devcontainer build .
	@devcontainer open .

.PHONY: otel-local
otel-local:
	docker rm -f jaeger-desktop || true
	docker run -d --name jaeger-desktop --restart=always -p 16686:16686 -p 4317:4317 -p 4318:4318 jaegertracing/jaeger:2.7.0
	open http://localhost:16686/

.PHONY: report/image-cve
report/image-cve: build
	make -C go govulncheck
	echo "Running CVE scan :: CVE -> CSV ... reports/$(SEMVER)/"
	grype docker:$(CONTROLLER_IMG) -o template -t reports/cve-report.tmpl --file reports/$(SEMVER)/controller-cve.csv
	grype docker:$(APP_IMG)        -o template -t reports/cve-report.tmpl --file reports/$(SEMVER)/app-cve.csv
	grype docker:$(UI_IMG)         -o template -t reports/cve-report.tmpl --file reports/$(SEMVER)/ui-cve.csv
