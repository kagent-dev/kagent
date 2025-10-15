
# Local architecture detection to build for the current platform
LOCALARCH ?= $(shell uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')

.PHONY: build
build: 
	docker buildx build --push --platform linux/$(LOCALARCH) -t localhost:5001/a2a-inspector:latest .

.PHONY: deploy
deploy: build
	kubectl apply -f a2a-inspector.yaml
	kubectl wait --for=condition=Available deployment/a2a-inspector -n kagent --timeout=60s
	open http://localhost:8080
	kubectl port-forward svc/a2a-inspector 8080:8080 -n kagent
