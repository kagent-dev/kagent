IMAGE_REGISTRY ?= cr.kagent.dev
IMAGE_NAME ?= kagent-dev/kagent/sandbox-mcp
IMAGE_TAG ?= latest

.PHONY: build
build:
	go build -o bin/sandbox-mcp ./cmd/

.PHONY: test
test:
	go test ./...

.PHONY: docker-build
docker-build:
	docker build -t $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG) .

.PHONY: docker-push
docker-push:
	docker push $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)

.PHONY: tidy
tidy:
	go mod tidy
