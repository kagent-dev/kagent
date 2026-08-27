# Node.js is required: https://nodejs.org/ or use nvm/brew
# The Node version is pinned in .nvmrc; Yarn ships via corepack.

.PHONY: build
build:
	yarn install --immutable
	yarn build

.PHONY: clean
clean:
	rm -rf node_modules
	rm -rf dist
	rm -rf .cache

.PHONY: install-node
install-node:
	brew install node

.PHONY: lint
lint:
	yarn lint
	yarn typecheck

.PHONY: test
test:
	yarn test

.PHONY: audit
audit:
	@echo "Running security audit..."
	yarn npm audit --all --recursive
	@echo "Security audit completed."

.PHONY: update
update:
	@echo "Updating UI lock file..."
	yarn up '*'
	@echo "Updating UI lock file done."
