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
test: update
	uv run pytest tests

.PHONY: test-cov
test-cov: update
	uv run pytest tests --cov=packages --cov-report=term-missing --cov-report=html --cov-fail-under=90

.PHONY: test-adk
test-adk:
	cd packages/kagent-adk && uv run pytest tests -v

.PHONY: test-adk-cov
test-adk-cov:
	cd packages/kagent-adk && uv run pytest tests -v --cov=kagent.adk --cov-report=term-missing --cov-report=html --cov-fail-under=73

.PHONY: test-adk-cov-strict
test-adk-cov-strict:
	cd packages/kagent-adk && uv run pytest tests -v --cov=kagent.adk --cov-report=term-missing --cov-report=html --cov-fail-under=90

.PHONY: test-adk-cov-core
test-adk-cov-core:
	cd packages/kagent-adk && uv run pytest tests -v \
		--cov=kagent.adk.types \
		--cov=kagent.adk._token \
		--cov=kagent.adk._session_service \
		--cov=kagent.adk._a2a \
		--cov=kagent.adk.converters \
		--cov-report=term-missing --cov-report=html

.PHONY: test-types
test-types:
	cd packages/kagent-adk && uv run pytest tests/test_types.py -v --cov=src/kagent/adk/types.py --cov-report=term-missing --cov-report=html

.PHONY: test-watch
test-watch:
	uv run pytest-watch tests -- -v

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