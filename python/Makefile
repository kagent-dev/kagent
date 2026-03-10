export GIT_LFS_SKIP_SMUDGE=1

### Build and Test

.PHONY: update
update:
	uv python list | grep 3.12
	uv sync --all-extras --all-groups
	uv lock

.PHONY: format
format:
	uv run ruff format
	uv run ruff check

.PHONY: lint
lint: 
	uv run ruff format --diff

.PHONY: test
test: update generate-test-certs
	uv run pytest ./packages/**/tests/

.PHONY: build
build: update format

.PHONY: audit
audit: build
	#check pip-audit is installed
	uv run pip-audit --version || uv pip install pip-audit
	uv run pip-audit

.PHONY: basic-langchain-sample
basic-langchain-sample:
	docker build . -f samples/langgraph/currency/Dockerfile --tag localhost:5001/langgraph-currency:latest --push

.PHONY: research-crew-sample
research-crew-sample:
	docker build . -f samples/crewai/research-crew/Dockerfile --tag localhost:5001/research-crew:latest --push

.PHONY: poem-flow-sample
poem-flow-sample:
	docker build . -f samples/crewai/poem_flow/Dockerfile --tag localhost:5001/poem-flow:latest --push

.PHONY: generate-test-certs
generate-test-certs:
	@CERT_DIR=packages/kagent-adk/tests/fixtures/certs; \
	if [ -f "$$CERT_DIR/ca-cert.pem" ] && [ -f "$$CERT_DIR/server-cert.pem" ] && [ -f "$$CERT_DIR/server-key.pem" ]; then \
		echo "Test certificates already exist, skipping generation"; \
	else \
		echo "Generating test certificates..."; \
		cd $$CERT_DIR && \
		openssl req -x509 -newkey rsa:4096 -nodes \
			-keyout ca-key.pem \
			-out ca-cert.pem \
			-days 365 \
			-subj "/CN=Test CA/O=Kagent Test/OU=Test" \
			-extensions v3_ca \
			-config ca-extensions.conf && \
		openssl req -newkey rsa:4096 -nodes \
			-keyout server-key.pem \
			-out server-req.pem \
			-subj "/CN=localhost/O=Kagent Test/OU=Test Server" && \
		echo "subjectAltName=DNS:localhost,IP:127.0.0.1" > server-extensions.conf && \
		openssl x509 -req \
			-in server-req.pem \
			-CA ca-cert.pem \
			-CAkey ca-key.pem \
			-CAcreateserial \
			-out server-cert.pem \
			-days 365 \
			-extfile server-extensions.conf && \
		rm -f server-extensions.conf server-req.pem && \
		echo "Test certificates generated successfully"; \
	fi
	
.PHONY: basic-openai-sample
basic-openai-sample:
	docker build . -f samples/openai/basic_agent/Dockerfile --tag localhost:5001/basic-openai:latest --push
