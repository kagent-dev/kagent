
# Install bun
# https://bun.sh/docs/installation

.PHONY: build
build:
	rm -f bun.lockb
	bun install
	bun run build --no-lint

.PHONY: clean
clean:
	rm -rf node_modules
	rm -f bun.lockb
	rm -rf dist
	rm -rf .bun
	rm -rf .cache

.PHONY: install-bun
install-bun:
	brew install bun

.PHONY: build
update:
	@echo "Updating UI lock file..."
	bun install --frozen-lockfile --production
	bun update  --lockfile-only
	@echo "Updating UI lock file done."