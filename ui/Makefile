
# Install bun
# https://bun.sh/docs/installation

.PHONY: build
build:
	rm -f bun.lockb
	bun install
	bun run build --no-lint

.PHONY: install-bun
install-bun:
	brew install bun

.PHONY: build
update:
	@echo "Updating UI lock file..."
	bun install --frozen-lockfile --production
	bun update  --lockfile-only
	@echo "Updating UI lock file done."