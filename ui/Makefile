# Node.js is required: https://nodejs.org/ or use nvm/brew

.PHONY: build
build:
	npm ci
	npm run build

.PHONY: clean
clean:
	rm -rf node_modules
	rm -rf dist
	rm -rf .cache

.PHONY: install-node
install-node:
	brew install node

.PHONY: audit
audit:
	@echo "Running security audit..."
	npm audit
	@echo "Security audit completed."

.PHONY: update
update:
	@echo "Updating UI lock file..."
	npm install
	npm audit fix --package-lock-only
	@echo "Updating UI lock file done."