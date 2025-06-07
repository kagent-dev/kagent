export GIT_LFS_SKIP_SMUDGE=1

### Build and Test

.PHONY: update
update:
	uv sync --all-extras --all-groups
	uv lock

.PHONY: format
format:
	uv run ruff format
	uv run ruff check

.PHONY: test
test: update
	uv run pytest tests

.PHONY: build
build: update format

.PHONY: license-check
license-check:
	@echo "Checking for license headers in Python files..."
	@if ! grep -q "license.*Apache" pyproject.toml; then \
		echo "Error: License not found in pyproject.toml"; \
		exit 1; \
	fi
	@files_without_license=$$(find src -name "*.py" -type f -exec grep -L "Licensed under the Apache License" {} \;) && \
	if [ ! -z "$$files_without_license" ]; then \
		echo "Error: The following Python files are missing license headers:"; \
		echo "$$files_without_license"; \
		exit 1; \
	fi
	@echo "All Python files have proper license headers."

.PHONY: add-license
add-license:
	@echo "Adding license headers to Python files..."
	@python scripts/add_license_headers.py
	@echo "Done."
